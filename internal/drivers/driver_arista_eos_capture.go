package drivers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// eosMirrorLinux is the Linux interface name in "show monitor session":
// "Cpu : active (mirror0)".
var eosMirrorLinux = regexp.MustCompile(`(?i)\(\s*(mirror[0-9]+)\s*\)`)

var eosMirrorIfaceExact = regexp.MustCompile(`^mirror[0-9]+$`)

// eosMirrorApplyCommands creates one monitor session over eAPI. The
// session name is the interface, so a capture of Ethernet1 is
// "monitor session Ethernet1 source Ethernet1",
// "monitor session Ethernet1 destination cpu", and
// "monitor session Ethernet1 rate-limit per-ingress-chip 1 mbps".
// The Linux mirror name is read by a later show: in the same runCmds
// batch the CPU destination is still "unknown" and has no mirror
// interface. A leftover session of the same name is deleted first, in
// its own request. "no monitor session" on a missing session fails the
// whole batch and would roll the new session back with it.
func eosMirrorApplyCommands(iface string) []string {
	return []string{
		"configure",
		"monitor session " + iface + " source " + iface,
		"monitor session " + iface + " destination cpu",
		"monitor session " + iface + " rate-limit per-ingress-chip 1 mbps",
		"end",
	}
}

func eosMirrorDeleteCommands(session string) []string {
	return []string{
		"configure",
		"no monitor session " + session,
		"end",
	}
}

func eosSessionMissing(line string) bool {
	l := strings.ToLower(line)
	return strings.Contains(l, "does not exist") || strings.Contains(l, "not found") || strings.Contains(l, "no such")
}

// eosParseMirrorJSON reads mirrorDeviceName from "show monitor session"
// in JSON. The text form hides that name until the CPU destination is
// active, and a show in the same runCmds as the configuration still
// says "unknown".
func eosParseMirrorJSON(session string, raw json.RawMessage) (string, error) {
	var show struct {
		Sessions map[string]struct {
			MirrorDeviceName string `json:"mirrorDeviceName"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &show); err != nil {
		return "", err
	}
	name := ""
	for key, sess := range show.Sessions {
		if strings.EqualFold(key, session) {
			name = sess.MirrorDeviceName
			break
		}
	}
	if !eosMirrorIfaceExact.MatchString(name) {
		return "", fmt.Errorf("monitor session %s has no cpu mirror interface", session)
	}
	return name, nil
}

// eosParseMirrorInterface reads the Linux capture interface from
// "show monitor session" text: "Cpu : active (mirror0)".
func eosParseMirrorInterface(session, show string) (string, error) {
	m := eosMirrorLinux.FindStringSubmatch(show)
	if m == nil {
		text := strings.Join(strings.Fields(show), " ")
		if len(text) > 300 {
			text = text[:300]
		}
		if text == "" {
			text = "(empty)"
		}
		return "", fmt.Errorf("monitor session %s has no cpu mirror interface: %s", session, text)
	}
	return m[1], nil
}

// eosTCPDumpCommand is the EOS CLI command that streams pcap bytes from
// the Linux mirror interface. The filter and interface are already
// validated; snaplen and the packet count are integers we format.
func eosTCPDumpCommand(linuxIface string, req PortMirrorRequest) (string, error) {
	if !eosMirrorIfaceExact.MatchString(linuxIface) {
		return "", fmt.Errorf("refusing capture interface %q", linuxIface)
	}
	if req.Snaplen < 0 || req.Snaplen > captureMaxSnaplen {
		return "", fmt.Errorf("snaplen %d is out of range", req.Snaplen)
	}
	if req.MaxPackets < 0 || req.MaxPackets > captureMaxPackets {
		return "", fmt.Errorf("max packets %d is out of range", req.MaxPackets)
	}
	if err := ValidateCaptureFilter(strings.TrimSpace(req.Filter)); err != nil {
		return "", err
	}
	args := []string{
		"bash", "sudo", "-n", "tcpdump",
		"-n", "-U", "-w", "-",
		"-i", linuxIface,
		"-s", strconv.Itoa(req.Snaplen),
	}
	if req.MaxPackets > 0 {
		args = append(args, "-c", strconv.Itoa(req.MaxPackets))
	}
	if f := strings.TrimSpace(req.Filter); f != "" {
		args = append(args, "--", f)
	}
	return strings.Join(args, " "), nil
}

// SetupPortMirror mirrors req.Interface to the switch CPU over eAPI and
// starts tcpdump on the mirror interface. The pcap bytes stay on their
// own SSH exec (no PTY). ctx ends that tcpdump (client disconnect or the
// capture's own time limit). The monitor session is removed on failure
// here, and by TeardownPortMirror when the capture ends.
func (driver *AristaDriver) SetupPortMirror(ctx context.Context, req PortMirrorRequest) (*PortMirrorSession, error) {
	req, err := NormalizeCaptureRequest(req)
	if err != nil {
		return nil, err
	}
	// EOS "monitor session … source ?" accepts Ethernet, Port-Channel,
	// and Recirc-Channel only, and not a subinterface. The GUI hides
	// the other rows; this rejects a direct call.
	if err := CaptureSourceAllowed("eos", req.Interface); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Drop a leftover of this same name so sources do not accumulate.
	// A session that is already gone is not a failure.
	if err := driver.removeMirror(ctx, req.Interface); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, eosMirrorApplyCommands(req.Interface), eapiFormatText); err != nil {
		_ = driver.removeMirror(context.Background(), req.Interface)
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = driver.removeMirror(context.Background(), req.Interface)
		return nil, err
	}
	// The CPU destination stays "unknown", with no Linux mirror name,
	// until EOS programs the session. On the lab switch that is about
	// two seconds after the configuration request returns.
	linuxIface, err := driver.eosMirrorLinuxIface(ctx, req.Interface)
	if err != nil {
		_ = driver.removeMirror(context.Background(), req.Interface)
		return nil, err
	}
	cmd, err := eosTCPDumpCommand(linuxIface, req)
	if err != nil {
		_ = driver.removeMirror(context.Background(), req.Interface)
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = driver.removeMirror(context.Background(), req.Interface)
		return nil, err
	}
	stream, err := startSSHCommand(ctx, driver.p, cmd)
	if err != nil {
		_ = driver.removeMirror(context.Background(), req.Interface)
		return nil, err
	}
	return &PortMirrorSession{
		Name:      req.Interface,
		Interface: linuxIface,
		Packets:   stream,
		diag:      stream.stderrText,
	}, nil
}

// TeardownPortMirror stops tcpdump and deletes the monitor session over
// eAPI. The caller passes a context that outlives the capture request,
// because the request context is already cancelled when the browser
// disconnects. Closing the SSH stream is what stops tcpdump.
func (driver *AristaDriver) TeardownPortMirror(ctx context.Context, session *PortMirrorSession) error {
	var err error
	if session != nil && session.Packets != nil {
		err = session.Packets.Close()
	}
	name := ""
	if session != nil {
		name = session.Name
	}
	if name != "" {
		if rerr := driver.removeMirror(ctx, name); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}
	return err
}

// eosMirrorReadyWait is how long setup polls "show monitor session"
// for the Linux mirror interface. Programming the session is not
// finished when the configuration request returns.
const eosMirrorReadyWait = 4 * time.Second

func (driver *AristaDriver) eosMirrorLinuxIface(ctx context.Context, iface string) (string, error) {
	deadline := time.Now().Add(eosMirrorReadyWait)
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		shown, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, []string{"show monitor session " + iface}, eapiFormatJSON)
		if err != nil {
			return "", err
		}
		name, err := eosParseMirrorJSON(iface, shown[0])
		if err == nil {
			return name, nil
		}
		if !strings.Contains(err.Error(), "no cpu mirror interface") {
			return "", err
		}
		last = err
		if !time.Now().Before(deadline) {
			return "", last
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

// removeMirror deletes the named monitor session. A session that is
// already gone is success. eAPI runs the whole batch or rolls it back,
// so this stays its own request.
func (driver *AristaDriver) removeMirror(ctx context.Context, session string) error {
	if session == "" {
		return nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	_, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, eosMirrorDeleteCommands(session), eapiFormatText)
	if err == nil || eosSessionMissing(err.Error()) {
		return nil
	}
	return err
}

// sshExecStream is one SSH exec channel (no PTY, so tcpdump's pcap bytes
// are not rewritten as terminal text). Close stops the remote command and
// leaves the cached SSH connection open. Close is safe to call more than once.
type sshExecStream struct {
	session *ssh.Session
	stdout  io.Reader
	release func()

	mu     sync.Mutex
	stderr bytes.Buffer
	once   sync.Once
}

func (s *sshExecStream) Read(p []byte) (int, error) { return s.stdout.Read(p) }

func (s *sshExecStream) Close() error {
	var err error
	s.once.Do(func() {
		if s.session != nil {
			err = s.session.Close()
		}
		if s.release != nil {
			s.release()
		}
	})
	return err
}

func (s *sshExecStream) stderrText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.stderr.String())
}

func (s *sshExecStream) writeStderr(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	const max = 4 << 10
	if s.stderr.Len() >= max {
		return
	}
	if len(p) > max-s.stderr.Len() {
		p = p[:max-s.stderr.Len()]
	}
	_, _ = s.stderr.Write(p)
}

// startSSHCommand runs cmd on the device over a cached SSH connection on
// port 22 (not the NETCONF port on DriverParam.Port). Cancelling ctx
// closes the exec channel, which stops the remote command. The TCP
// connection and SSH handshake stay cached for the next capture.
func startSSHCommand(ctx context.Context, p DriverParam, cmd string) (*sshExecStream, error) {
	return openCachedSSHExec(ctx, p.Username, p.Password, net.JoinHostPort(p.Name, sshDefaultPort), cmd)
}
