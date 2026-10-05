package drivers

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeSSHExec is an SSH server that accepts exec channels and leaves the
// TCP connection up when the channel closes. It counts completed handshakes.
type fakeSSHExec struct {
	t    *testing.T
	ln   net.Listener
	user string
	pass string

	mu    sync.Mutex
	nConn int
	cmds  []string
}

func startFakeSSHExec(t *testing.T) *fakeSSHExec {
	t.Helper()
	f := &fakeSSHExec{t: t, user: "admin", pass: "secret"}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == f.user && string(pass) == f.pass {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.ln = ln
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn, cfg)
		}
	}()
	return f
}

func (f *fakeSSHExec) addr() string { return f.ln.Addr().String() }

func (f *fakeSSHExec) connections() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nConn
}

func (f *fakeSSHExec) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.cmds))
	copy(out, f.cmds)
	return out
}

func (f *fakeSSHExec) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	f.mu.Lock()
	f.nConn++
	f.mu.Unlock()
	go func() {
		for req := range reqs {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}()
	for newCh := range chans {
		go f.handleChannel(newCh)
	}
	_ = sconn.Close()
}

func (f *fakeSSHExec) handleChannel(newCh ssh.NewChannel) {
	if newCh.ChannelType() != "session" {
		_ = newCh.Reject(ssh.UnknownChannelType, "only session")
		return
	}
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	for req := range reqs {
		if req.Type == "exec" {
			var msg struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &msg)
			f.mu.Lock()
			f.cmds = append(f.cmds, msg.Command)
			f.mu.Unlock()
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			_, _ = ch.Write([]byte("OK"))
			_ = ch.CloseWrite()
			continue
		}
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}

func readExecOK(t *testing.T, r io.Reader) {
	t.Helper()
	buf := make([]byte, 2)
	if _, err := io.ReadFull(r, buf); err != nil || string(buf) != "OK" {
		t.Fatalf("read %q err=%v", buf, err)
	}
}

func TestSSHExecReusesConnection(t *testing.T) {
	resetSSHExecCache()
	t.Cleanup(resetSSHExecCache)
	f := startFakeSSHExec(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	first, err := openCachedSSHExec(ctx, "admin", "secret", f.addr(), "tcpdump -i mirror0")
	if err != nil {
		t.Fatal(err)
	}
	readExecOK(t, first)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.connections(); got != 1 {
		t.Fatalf("connections after first close = %d, want 1", got)
	}

	second, err := openCachedSSHExec(ctx, "admin", "secret", f.addr(), "tcpdump -i mirror1")
	if err != nil {
		t.Fatal(err)
	}
	readExecOK(t, second)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.connections(); got != 1 {
		t.Fatalf("connections after second exec = %d, want 1 (cached SSH connection)", got)
	}
	cmds := f.commands()
	if len(cmds) != 2 || cmds[0] != "tcpdump -i mirror0" || cmds[1] != "tcpdump -i mirror1" {
		t.Fatalf("commands = %v", cmds)
	}
}

func TestSSHExecOverlapOneConnection(t *testing.T) {
	resetSSHExecCache()
	t.Cleanup(resetSSHExecCache)
	f := startFakeSSHExec(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream, err := openCachedSSHExec(ctx, "admin", "secret", f.addr(), "tcpdump")
			if err != nil {
				errCh <- err
				return
			}
			buf := make([]byte, 2)
			if _, err := io.ReadFull(stream, buf); err != nil || string(buf) != "OK" {
				_ = stream.Close()
				errCh <- fmt.Errorf("read %q: %w", buf, err)
				return
			}
			errCh <- stream.Close()
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := f.connections(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
}

func TestSSHExecExpireRedials(t *testing.T) {
	resetSSHExecCache()
	t.Cleanup(resetSSHExecCache)
	f := startFakeSSHExec(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := openCachedSSHExec(ctx, "admin", "secret", f.addr(), "tcpdump")
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	sshExecSlotFor("admin", f.addr()).expire(0)
	again, err := openCachedSSHExec(ctx, "admin", "secret", f.addr(), "tcpdump")
	if err != nil {
		t.Fatal(err)
	}
	readExecOK(t, again)
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.connections(); got != 2 {
		t.Fatalf("connections after idle expiry = %d, want 2", got)
	}
}

func TestSSHExecPasswordChangeWhileBusy(t *testing.T) {
	resetSSHExecCache()
	t.Cleanup(resetSSHExecCache)
	f := startFakeSSHExec(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := openCachedSSHExec(ctx, "admin", "secret", f.addr(), "tcpdump")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_, err = openCachedSSHExec(ctx, "admin", "other", f.addr(), "tcpdump")
	if !errors.Is(err, errSSHExecCreds) {
		t.Fatalf("err = %v, want credential change", err)
	}
	if got := f.connections(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
}
