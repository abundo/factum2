package storage

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// CopyRequest is a factum→device transfer of one repository file.
type CopyRequest struct {
	Path        string `json:"path"`
	Device      string `json:"device"`
	Protocol    string `json:"protocol"` // http, tftp, scp, sftp
	Destination string `json:"destination"`
	Username    string `json:"-"`
	Password    string `json:"-"`
	Platform    string `json:"-"`
	Host        string `json:"-"` // device FQDN/IP to dial for push
}

func NormalizeProtocol(p string) (string, error) {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case "http", "tftp", "scp", "sftp":
		return p, nil
	case "":
		return "", fmt.Errorf("protocol is required")
	default:
		return "", fmt.Errorf("unsupported protocol %q (http, tftp, scp, sftp)", p)
	}
}

// SourceURL is the locator a device uses to pull path over protocol.
func SourceURL(cfg *Config, protocol, relPath string) (string, error) {
	protocol, err := NormalizeProtocol(protocol)
	if err != nil {
		return "", err
	}
	apiPath, err := cleanAPIPath(relPath)
	if err != nil {
		return "", err
	}
	if apiPath == "/" {
		return "", ErrInvalidPath
	}
	rel := strings.TrimPrefix(apiPath, "/")
	switch protocol {
	case "http":
		base := strings.TrimRight(strings.TrimSpace(cfg.HTTPURL), "/")
		if base == "" {
			return "", fmt.Errorf("storage HTTP URL is not configured (Admin → Settings → Factum → Software)")
		}
		return base + "/files/" + rel, nil
	case "tftp":
		host := strings.TrimSpace(cfg.TFTPHost)
		if host == "" {
			return "", fmt.Errorf("storage TFTP host is not configured")
		}
		return "tftp://" + host + "/" + rel, nil
	case "scp":
		host := strings.TrimSpace(cfg.SFTPHost)
		user := strings.TrimSpace(cfg.SFTPUser)
		if host == "" || user == "" {
			return "", fmt.Errorf("storage SFTP host/user is not configured")
		}
		return "scp://" + user + "@" + host + "/" + rel, nil
	case "sftp":
		host := strings.TrimSpace(cfg.SFTPHost)
		user := strings.TrimSpace(cfg.SFTPUser)
		if host == "" || user == "" {
			return "", fmt.Errorf("storage SFTP host/user is not configured")
		}
		return "sftp://" + user + "@" + host + "/" + rel, nil
	default:
		return "", fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func defaultDest(relPath, dest string) string {
	dest = strings.TrimSpace(dest)
	if dest != "" {
		return dest
	}
	return path.Base(strings.ReplaceAll(relPath, "\\", "/"))
}

// PullCommands returns the CLI the device should run to copy src onto dest.
// src is a full locator (http://…, tftp://…). dest is the on-device path.
func PullCommands(platform, protocol, src, dest string) ([]string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	protocol, err := NormalizeProtocol(protocol)
	if err != nil {
		return nil, err
	}
	if src == "" || dest == "" {
		return nil, fmt.Errorf("source and destination are required")
	}
	switch platform {
	case "eos":
		return []string{"copy " + src + " " + dest}, nil
	case "ios-xr":
		return []string{"copy " + src + " " + dest}, nil
	case "sros", "sros-md":
		return []string{"//file copy " + src + " " + dest}, nil
	case "vrp":
		if protocol == "tftp" {
			host, file, err := splitTFTPURL(src)
			if err != nil {
				return nil, err
			}
			return []string{"tftp " + host + " get " + file + " " + dest}, nil
		}
		return []string{"copy " + src + " " + dest}, nil
	case "ciscosmb":
		return []string{"copy " + src + " " + dest}, nil
	default:
		return nil, fmt.Errorf("no copy command for platform %q", platform)
	}
}

func splitTFTPURL(src string) (host, file string, err error) {
	u, err := url.Parse(src)
	if err != nil {
		return "", "", err
	}
	if u.Host == "" || strings.TrimPrefix(u.Path, "/") == "" {
		return "", "", fmt.Errorf("invalid tftp url %q", src)
	}
	return u.Host, strings.TrimPrefix(u.Path, "/"), nil
}

// PullToDevice SSHes (via the platform driver) and runs the copy-from-URL command.
func PullToDevice(req CopyRequest, src string) (string, error) {
	cmds, err := PullCommands(req.Platform, req.Protocol, src, defaultDest(req.Path, req.Destination))
	if err != nil {
		return "", err
	}
	d, err := drivers.NewDriver(drivers.WithActor(drivers.DriverParam{
		Name:     req.Host,
		Platform: strings.ToLower(req.Platform),
		Username: req.Username,
		Password: req.Password,
	}, "storage"))
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, cmd := range cmds {
		res, err := d.Exec(cmd)
		if res != nil {
			out.WriteString(res.Result)
		}
		if err != nil {
			return out.String(), fmt.Errorf("%s: %w", cmd, err)
		}
	}
	return out.String(), nil
}

// PushToDevice copies the local file to the device over SCP or SFTP.
func PushToDevice(repo *Repo, req CopyRequest) error {
	f, ent, err := repo.Open(req.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	dest := defaultDest(req.Path, req.Destination)
	addr := req.Host
	if !strings.Contains(addr, ":") {
		addr = net.JoinHostPort(addr, "22")
	}
	cfg := &ssh.ClientConfig{
		User:            req.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(req.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // same as device CLI sessions
		Timeout:         30 * time.Second,
	}
	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("ssh %s: %w", addr, err)
	}
	defer conn.Close()
	switch req.Protocol {
	case "sftp":
		return sftpPut(conn, f, dest)
	case "scp":
		return scpPut(conn, f, ent.Size, dest)
	default:
		return fmt.Errorf("push not supported for %s", req.Protocol)
	}
}

func sftpPut(conn *ssh.Client, r io.Reader, dest string) error {
	c, err := sftp.NewClient(conn)
	if err != nil {
		return err
	}
	defer c.Close()
	if dir := path.Dir(strings.ReplaceAll(dest, "\\", "/")); dir != "" && dir != "." && dir != "/" {
		_ = c.MkdirAll(dir)
	}
	w, err := c.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
}

func scpPut(conn *ssh.Client, r io.Reader, size int64, dest string) error {
	sess, err := conn.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	name := path.Base(strings.ReplaceAll(dest, "\\", "/"))
	if err := sess.Start("scp -t " + shellQuote(dest)); err != nil {
		return err
	}
	if err := readSCPAck(stdout); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdin, "C0644 %d %s\n", size, name); err != nil {
		return err
	}
	if err := readSCPAck(stdout); err != nil {
		return err
	}
	if _, err := io.Copy(stdin, r); err != nil {
		return err
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}
	if err := readSCPAck(stdout); err != nil {
		return err
	}
	_ = stdin.Close()
	return sess.Wait()
}

func readSCPAck(r io.Reader) error {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return err
	}
	if b[0] == 0 {
		return nil
	}
	msg, _ := io.ReadAll(r)
	return fmt.Errorf("scp: %s%s", string(b[:]), string(msg))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
