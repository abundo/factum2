package storage

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func hostKeyPath(root string) string {
	return filepath.Join(root, ".ssh", "host_key")
}

func loadOrCreateHostKey(root string) (ssh.Signer, error) {
	path := hostKeyPath(root)
	if pemBytes, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(pemBytes)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	return signer, nil
}

// ServeSFTP is a read-only SSH server (SFTP subsystem plus scp -f) jailed to repo.
func ServeSFTP(ln net.Listener, repo *Repo, user, pass string, hostKey ssh.Signer) error {
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, secret []byte) (*ssh.Permissions, error) {
			if subtle.ConstantTimeCompare([]byte(c.User()), []byte(user)) == 1 &&
				subtle.ConstantTimeCompare(secret, []byte(pass)) == 1 {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(hostKey)
	for {
		tcp, err := ln.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "use of closed") {
				return nil
			}
			return err
		}
		go handleSFTPConn(tcp, repo, cfg)
	}
}

func handleSFTPConn(n net.Conn, repo *Repo, cfg *ssh.ServerConfig) {
	defer n.Close()
	conn, chans, reqs, err := ssh.NewServerConn(n, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			return
		}
		go serveSSHSession(ch, requests, repo)
	}
}

func serveSSHSession(ch ssh.Channel, requests <-chan *ssh.Request, repo *Repo) {
	defer ch.Close()
	for req := range requests {
		switch req.Type {
		case "subsystem":
			if len(req.Payload) < 4 {
				_ = req.Reply(false, nil)
				continue
			}
			n := binary.BigEndian.Uint32(req.Payload)
			if 4+int(n) > len(req.Payload) {
				_ = req.Reply(false, nil)
				continue
			}
			name := string(req.Payload[4 : 4+n])
			if name != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			handlers := sftp.Handlers{
				FileGet:  sftpJail{repo: repo},
				FileList: sftpJail{repo: repo},
				FilePut:  denyPut{},
				FileCmd:  denyPut{},
			}
			srv := sftp.NewRequestServer(ch, handlers)
			_ = srv.Serve()
			return
		case "exec":
			if len(req.Payload) < 4 {
				_ = req.Reply(false, nil)
				continue
			}
			n := binary.BigEndian.Uint32(req.Payload)
			if 4+int(n) > len(req.Payload) {
				_ = req.Reply(false, nil)
				continue
			}
			cmd := string(req.Payload[4 : 4+n])
			path, ok := parseSCPFrom(cmd)
			if !ok {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			_ = scpFrom(ch, repo, path)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func parseSCPFrom(cmd string) (string, bool) {
	cmd = strings.TrimSpace(cmd)
	for _, prefix := range []string{"scp -f ", "scp -f -- "} {
		if strings.HasPrefix(cmd, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(cmd, prefix)), true
		}
	}
	return "", false
}

func scpFrom(ch ssh.Channel, repo *Repo, rel string) error {
	f, ent, err := repo.Open(rel)
	if err != nil {
		_, _ = ch.Stderr().Write([]byte(err.Error() + "\n"))
		return err
	}
	defer f.Close()
	if _, err := ch.Read(make([]byte, 1)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(ch, "C0644 %d %s\n", ent.Size, ent.Name); err != nil {
		return err
	}
	if _, err := ch.Read(make([]byte, 1)); err != nil {
		return err
	}
	if _, err := io.Copy(ch, f); err != nil {
		return err
	}
	if _, err := ch.Write([]byte{0}); err != nil {
		return err
	}
	_, _ = ch.Read(make([]byte, 1))
	return nil
}

type denyPut struct{}

func (denyPut) Filewrite(*sftp.Request) (io.WriterAt, error) {
	return nil, sftp.ErrSSHFxPermissionDenied
}

func (denyPut) Filecmd(*sftp.Request) error {
	return sftp.ErrSSHFxPermissionDenied
}

type sftpJail struct {
	repo *Repo
}

func (j sftpJail) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f, _, err := j.repo.Open(r.Filepath)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (j sftpJail) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		ents, err := j.repo.List(r.Filepath)
		if err != nil {
			return nil, err
		}
		infos := make([]os.FileInfo, 0, len(ents))
		for _, e := range ents {
			st, err := j.lstat(e.Path)
			if err != nil {
				continue
			}
			infos = append(infos, st)
		}
		return listerAt(infos), nil
	case "Stat", "Lstat":
		st, err := j.lstat(r.Filepath)
		if err != nil {
			return nil, err
		}
		return listerAt{st}, nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

func (j sftpJail) lstat(rel string) (os.FileInfo, error) {
	abs, _, err := j.repo.resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.Lstat(abs)
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}
