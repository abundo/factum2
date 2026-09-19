package drivers

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"

	"golang.org/x/crypto/ssh"
)

func TestMain(m *testing.M) {
	ResetSSHPoolForTest()
	m.Run()
}

func useFastSSHIdle(t *testing.T) {
	t.Helper()
	sshTestIdleWindow = 25 * time.Millisecond
	t.Cleanup(func() {
		sshTestIdleWindow = 0
	})
}

type fakeSSH struct {
	t                 *testing.T
	ln                net.Listener
	user, pass, pass2 string
	banner            string
	bannerDribble     time.Duration
	unrecognizedWidth bool
	dropOn            string
	dropAfter         string
	blockKeepalive    bool
	pagerOn           string
	moreInDump        bool
	hangOn            string
	hangRelease       chan struct{}
	linger            map[string]time.Duration
	replies           map[string]string
	crlf              bool
	mu                sync.Mutex
	nConn             int
	cmds              []string
	conns             []net.Conn
}

func startFakeSSH(t *testing.T) *fakeSSH {
	t.Helper()
	f := &fakeSSH{
		t:           t,
		user:        "admin",
		pass:        "secret",
		linger:      map[string]time.Duration{},
		replies:     map[string]string{},
		hangRelease: make(chan struct{}),
	}
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
			if c.User() != f.user {
				return nil, fmt.Errorf("bad user")
			}
			if string(pass) == f.pass || (f.pass2 != "" && string(pass) == f.pass2) {
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
			f.mu.Lock()
			f.conns = append(f.conns, conn)
			f.mu.Unlock()
			go f.serve(conn, cfg)
		}
	}()
	return f
}

func (f *fakeSSH) addr() (host, port string) {
	host, port, err := net.SplitHostPort(f.ln.Addr().String())
	if err != nil {
		f.t.Fatal(err)
	}
	return host, port
}

func (f *fakeSSH) param(platform string) DriverParam {
	host, port := f.addr()
	return DriverParam{
		Name:     host,
		Port:     port,
		Username: f.user,
		Password: f.pass,
		Platform: platform,
	}
}

func (f *fakeSSH) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	f.mu.Lock()
	f.nConn++
	f.mu.Unlock()
	go f.handleGlobalReqs(reqs)
	for newCh := range chans {
		go f.handleChannel(newCh)
	}
	_ = sconn.Close()
}

func (f *fakeSSH) handleGlobalReqs(reqs <-chan *ssh.Request) {
	for req := range reqs {
		f.mu.Lock()
		block := f.blockKeepalive && req.Type == "keepalive@openssh.com"
		f.mu.Unlock()
		if block {
			continue
		}
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}

func (f *fakeSSH) handleChannel(newCh ssh.NewChannel) {
	if newCh.ChannelType() != "session" {
		_ = newCh.Reject(ssh.UnknownChannelType, "only session")
		return
	}
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	go func() {
		for req := range reqs {
			ok := req.Type == "pty-req" || req.Type == "shell" || req.Type == "env" || req.Type == "window-change"
			if req.WantReply {
				_ = req.Reply(ok, nil)
			}
		}
	}()
	if f.bannerDribble > 0 {
		deadline := time.Now().Add(f.bannerDribble)
		for time.Now().Before(deadline) {
			_, _ = ch.Write([]byte("MOTD\n"))
			time.Sleep(200 * time.Millisecond)
		}
	} else if f.banner != "" {
		_, _ = ch.Write([]byte(f.banner))
	}
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	for {
		n, err := ch.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				i := strings.IndexByte(string(buf), '\n')
				if i < 0 {
					break
				}
				line := strings.TrimRight(string(buf[:i]), "\r")
				buf = buf[i+1:]
				f.handleCmd(ch, line)
			}
		}
		if err != nil {
			return
		}
	}
}

func (f *fakeSSH) handleCmd(ch ssh.Channel, cmd string) {
	f.mu.Lock()
	f.cmds = append(f.cmds, cmd)
	f.mu.Unlock()

	nl := "\n"
	if f.crlf {
		nl = "\r\n"
	}

	if f.hangOn != "" && cmd == f.hangOn {
		for {
			select {
			case <-f.hangRelease:
				_, _ = ch.Write([]byte("hung-done" + nl))
				return
			case <-time.After(15 * time.Millisecond):
				_, _ = ch.Write([]byte("x"))
			}
		}
	}

	if d := f.linger[cmd]; d > 0 {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			_, _ = ch.Write([]byte("."))
			time.Sleep(5 * time.Millisecond)
		}
	}

	if f.pagerOn != "" && cmd == f.pagerOn {
		_, _ = ch.Write([]byte("line1" + nl + "  --More--  " + nl))
		return
	}
	if f.moreInDump && strings.Contains(cmd, "display") {
		_, _ = ch.Write([]byte("interface GigabitEthernet0/0/1" + nl + " description Need More: fiber and more traffic" + nl))
		return
	}
	if f.dropOn != "" && cmd == f.dropOn {
		f.dropOn = ""
		_ = ch.Close()
		return
	}

	out := f.replyFor(cmd)
	if out != "" {
		if f.crlf {
			out = strings.ReplaceAll(out, "\n", "\r\n")
		}
		_, _ = ch.Write([]byte(out))
	}
	if f.dropAfter != "" && cmd == f.dropAfter {
		_ = ch.Close()
	}
}

func (f *fakeSSH) replyFor(cmd string) string {
	if s, ok := f.replies[cmd]; ok {
		return s
	}
	if cmd == "terminal width 0" && f.unrecognizedWidth {
		return "% Unrecognized command\n"
	}
	switch cmd {
	case "screen-length 0 temporary", "terminal datadump", "terminal width 0":
		return "OK\n"
	case "display version":
		return "VRP version 1.0\n"
	case "system-view":
		return "[device]\n"
	case "return", "quit", "end", "configure":
		return ""
	default:
		return "out:" + cmd + "\n"
	}
}

func (f *fakeSSH) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.cmds))
	copy(out, f.cmds)
	return out
}

func (f *fakeSSH) connections() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nConn
}

func (f *fakeSSH) killConns() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		_ = c.Close()
	}
}

func countCmd(cmds []string, want string) int {
	n := 0
	for _, c := range cmds {
		if c == want {
			n++
		}
	}
	return n
}

func TestSessionKey(t *testing.T) {
	got := sessionKey(DriverParam{Name: "[2001:db8::1]", Username: "admin", Platform: "vrp"})
	want := "vrp/admin@[2001:db8::1]:22"
	if got != want {
		t.Fatalf("sessionKey IPv6: got %q want %q", got, want)
	}
	got = sessionKey(DriverParam{Name: "sw1.example.com", Port: "22", Username: "u", Platform: "sros-md"})
	want = "sros/u@sw1.example.com:22"
	if got != want {
		t.Fatalf("sessionKey sros-md: got %q want %q", got, want)
	}
}

func TestEnabledPlatformSet(t *testing.T) {
	s := enabledPlatformSet(nil)
	if _, ok := s["vrp"]; !ok {
		t.Fatalf("nil platforms: %v", s)
	}
	if _, ok := s["ciscosmb"]; !ok {
		t.Fatalf("nil platforms: %v", s)
	}
	empty := []string{}
	if s := enabledPlatformSet(&empty); len(s) != 0 {
		t.Fatalf("empty: %v", s)
	}
	none := []string{"none"}
	if s := enabledPlatformSet(&none); len(s) != 0 {
		t.Fatalf("none: %v", s)
	}
	noneVrp := []string{"none", "vrp"}
	if s := enabledPlatformSet(&noneVrp); len(s) != 0 {
		t.Fatalf("none,vrp: %v", s)
	}
	vrp := []string{"vrp"}
	if s := enabledPlatformSet(&vrp); len(s) != 1 {
		t.Fatalf("vrp: %v", s)
	}
}

func TestPagerRegex(t *testing.T) {
	if !hasPagerLeftover("line1\n  --More--  \n") {
		t.Fatal("expected pager match for --More--")
	}
	if !hasPagerLeftover("---- More ----\n") {
		t.Fatal("expected pager match for ---- More ----")
	}
	if hasPagerLeftover("description Need More: fiber and more traffic\n") {
		t.Fatal("More: substring must not match")
	}
}

func TestInitSSHPoolKnobs(t *testing.T) {
	clearSSHPoolForTest()
	t.Cleanup(ResetSSHPoolForTest)
	if err := InitSSHPool(SSHPoolConfig{MaxSessions: -1}); err == nil {
		t.Fatal("expected error for negative max_sessions")
	}
	if err := InitSSHPool(SSHPoolConfig{QueueDepth: -2}); err == nil {
		t.Fatal("expected error for negative queue_depth")
	}
	if err := InitSSHPool(SSHPoolConfig{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected double-init panic")
		}
	}()
	_ = InitSSHPool(SSHPoolConfig{})
}

func TestGetPoolPanicsIfMissing(t *testing.T) {
	clearSSHPoolForTest()
	t.Cleanup(ResetSSHPoolForTest)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if !strings.Contains(fmt.Sprint(r), "InitSSHPool was not called") {
			t.Fatalf("panic = %v", r)
		}
	}()
	_ = getPool()
}

func TestSSHReuse(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.banner = "Welcome\n"
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	out1, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display version"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out1, "VRP version") {
		t.Fatalf("output = %q", out1)
	}
	out2, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display version"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "VRP version") {
		t.Fatalf("output2 = %q", out2)
	}
	if f.connections() != 1 {
		t.Fatalf("connections = %d, want 1", f.connections())
	}
	if countCmd(f.commands(), "screen-length 0 temporary") != 1 {
		t.Fatalf("setup should run once, cmds=%v", f.commands())
	}
	if countCmd(f.commands(), "display version") != 2 {
		t.Fatalf("display version count, cmds=%v", f.commands())
	}
	st := getPool().Stats()
	if len(st) != 1 || st[0].Reused < 1 {
		t.Fatalf("stats reused = %+v", st)
	}
}

func TestSSHSerialize(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	errc := make(chan error, 2)
	go func() {
		defer wg.Done()
		_, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display a"}})
		errc <- err
	}()
	go func() {
		defer wg.Done()
		_, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display b"}})
		errc <- err
	}()
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.connections() != 1 {
		t.Fatalf("connections = %d, want 1", f.connections())
	}
	cmds := f.commands()
	if countCmd(cmds, "display a") != 1 || countCmd(cmds, "display b") != 1 {
		t.Fatalf("cmds = %v", cmds)
	}
}

func TestSSHPasswordChange(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.pass2 = "other"
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	p.Password = "other"
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() != 2 {
		t.Fatalf("connections = %d, want 2 after password change", f.connections())
	}
}

func TestSkipResetOnReads(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if countCmd(f.commands(), "return") != 0 {
		t.Fatalf("Reset should be skipped on reads, cmds=%v", f.commands())
	}
}

func TestResetOnLeftoverConfigMode(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "system-view"}}); err != nil {
		t.Fatal(err)
	}
	if countCmd(f.commands(), "return") != 1 {
		t.Fatalf("expected Reset return, cmds=%v", f.commands())
	}
}

func TestPagerLeftoverKillsSession(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.pagerOn = "display version"
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	_, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}})
	if !errors.Is(err, errPagerLeftover) {
		t.Fatalf("err = %v, want pager leftover", err)
	}
	f.pagerOn = ""
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display clock"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() < 2 {
		t.Fatalf("expected redial after pager kill, conns=%d", f.connections())
	}
}

func TestDumpContainingMoreDoesNotKill(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.moreInDump = true
	ResetSSHPoolForTest()
	p := f.param("vrp")
	out, err := sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "display current-config"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Need More:") {
		t.Fatalf("output = %q", out)
	}
}

func TestTerminalWidthUnrecognizedStillReady(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.unrecognizedWidth = true
	ResetSSHPoolForTest()
	p := f.param("ciscosmb")
	ctx := context.Background()
	cmds := []sshCmd{{Cmd: "terminal datadump"}, {Cmd: "terminal width 0"}, {Cmd: "show version"}}
	if _, err := sshRunCLI(ctx, p, cmds); err != nil {
		t.Fatal(err)
	}
	if _, err := sshRunCLI(ctx, p, cmds); err != nil {
		t.Fatal(err)
	}
	if f.connections() != 1 {
		t.Fatalf("connections = %d, want reuse", f.connections())
	}
	st := getPool().Stats()
	if len(st) != 1 || st[0].Reused < 1 {
		t.Fatalf("expected reuse after width 0 unrecognized: %+v", st)
	}
}

func TestQueueFull(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.hangOn = "hang"
	if err := initSSHPoolForTest(SSHPoolConfig{QueueDepth: 1, MaxSessions: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	p := f.param("vrp")
	ctx := context.Background()
	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = sshRunCLI(ctx, p, []sshCmd{{Cmd: "hang"}})
	}()
	<-started
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countCmd(f.commands(), "hang") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if countCmd(f.commands(), "hang") == 0 {
		t.Fatal("hang command never started")
	}
	queued := make(chan error, 1)
	go func() {
		_, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}})
		queued <- err
	}()
	time.Sleep(50 * time.Millisecond)
	_, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display clock"}})
	if !errors.Is(err, errQueueFull) {
		t.Fatalf("err = %v, want queue full", err)
	}
	close(f.hangRelease)
	if err := <-queued; err != nil {
		t.Fatalf("queued run: %v", err)
	}
}

func TestIdleEviction(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	if err := initSSHPoolForTest(SSHPoolConfig{IdleTimeout: 120 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() < 2 {
		t.Fatalf("expected redial after idle eviction, conns=%d", f.connections())
	}
}

func TestStaleIdleReplayOnWriteFail(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	sshTestFailWrite.Store(true)
	out, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "VRP version") {
		t.Fatalf("output = %q", out)
	}
	if f.connections() < 2 {
		t.Fatalf("expected redial after write fail, conns=%d", f.connections())
	}
}

func TestStaleIdleReplayOnConnDead(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	f.killConns()
	time.Sleep(50 * time.Millisecond)
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display clock"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() < 2 {
		t.Fatalf("expected redial after dead conn, conns=%d", f.connections())
	}
}

func TestStaleIdleReplayOnEmptyEOF(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	f.dropOn = "display clock"
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display clock"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() < 2 {
		t.Fatalf("expected extra dial for empty EOF replay, conns=%d", f.connections())
	}
}

func TestKeepaliveUnimplementedStillReuses(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	if err := initSSHPoolForTest(SSHPoolConfig{Keepalive: 40 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() != 1 {
		t.Fatalf("keepalive ok=false should still reuse, conns=%d", f.connections())
	}
	st := getPool().Stats()
	if len(st) != 1 || st[0].Reused < 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestCRLFNormalized(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.crlf = true
	f.replies["display version"] = "hello\nworld\n"
	ResetSSHPoolForTest()
	p := f.param("vrp")
	out, err := sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "display version"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\r\n") {
		t.Fatalf("CRLF leaked to caller: %q", out)
	}
	if !strings.Contains(out, "hello\nworld") {
		t.Fatalf("output = %q", out)
	}
}

func TestLegacyPlatformNotPooled(t *testing.T) {
	plats := []string{"sros", "ios-xr"}
	if err := initSSHPoolForTest(SSHPoolConfig{Platforms: &plats}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	if sshUseMemoryPool("sros") {
		t.Fatal("sros listed in YAML still has no v1 profile")
	}
	if sshUseMemoryPool("ios-xr") {
		t.Fatal("ios-xr listed in YAML still has no v1 profile")
	}
	if sshUseMemoryPool("vrp") {
		t.Fatal("vrp is not in the enabled set")
	}
	ResetSSHPoolForTest()
	if !sshUseMemoryPool("vrp") || !sshUseMemoryPool("ciscosmb") {
		t.Fatal("compiled defaults should pool vrp and ciscosmb")
	}
}

func TestColdRunLoginNotInJobTimeout(t *testing.T) {
	f := startFakeSSH(t)
	f.bannerDribble = 7 * time.Second
	f.linger["screen-length 0 temporary"] = 7 * time.Second
	f.linger["display version"] = 7 * time.Second
	ResetSSHPoolForTest()
	p := f.param("vrp")
	start := time.Now()
	out, err := sshRunCLI(context.Background(), p, []sshCmd{
		{Cmd: "screen-length 0 temporary"},
		{Cmd: "display version"},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("cold run: %v after %s", err, elapsed)
	}
	if !strings.Contains(out, "VRP version") {
		t.Fatalf("output = %q", out)
	}
	if elapsed < 30*time.Second {
		t.Fatalf("expected wall time >30s of idle waits, got %s", elapsed)
	}
}

func TestPooledJobTimeoutHardError(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.linger["display version"] = 35 * time.Second
	ResetSSHPoolForTest()
	p := f.param("vrp")
	start := time.Now()
	_, err := sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "display version"}})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected job timeout error")
	}
	if !errors.Is(err, errWaitTimeout) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if elapsed > 36*time.Second {
		t.Fatalf("job timeout should fire around 30s, took %s", elapsed)
	}
}

func TestKeepaliveAfterReplayDoesNotKillNewSession(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.blockKeepalive = true
	if err := initSSHPoolForTest(SSHPoolConfig{Keepalive: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	p := f.param("vrp")
	ctx := context.Background()
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	sshTestFailWrite.Store(true)
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := sshRunCLI(ctx, p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() != 2 {
		t.Fatalf("stale keepalive must not kill the redialed session, conns=%d", f.connections())
	}
	st := getPool().Stats()
	if len(st) != 1 || st[0].Reused < 1 {
		t.Fatalf("third run should reuse the new session: %+v", st)
	}
}

func TestEvictSkipsAcquiredSession(t *testing.T) {
	useFastSSHIdle(t)
	f1 := startFakeSSH(t)
	f1.hangOn = "hang"
	f2 := startFakeSSH(t)
	if err := initSSHPoolForTest(SSHPoolConfig{MaxSessions: 1, QueueDepth: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	ctx := context.Background()
	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = sshRunCLI(ctx, f1.param("vrp"), []sshCmd{{Cmd: "hang"}})
	}()
	<-started
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countCmd(f1.commands(), "hang") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if countCmd(f1.commands(), "hang") == 0 {
		t.Fatal("hang never started")
	}
	_, err := sshRunCLI(ctx, f2.param("vrp"), []sshCmd{{Cmd: "display version"}})
	if !errors.Is(err, errPoolFull) {
		t.Fatalf("err = %v, want pool full (must not evict the acquired session)", err)
	}
	close(f1.hangRelease)
}

func TestPooledEOFWithPayloadIsError(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.dropAfter = "display version"
	ResetSSHPoolForTest()
	p := f.param("vrp")
	_, err := sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "display version"}})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF for truncated dump", err)
	}
}

func TestPoolConfigFromDriverDefaults(t *testing.T) {
	cfg, err := PoolConfigFromDriver(util.ConfigDriver{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Platforms != nil || cfg.MaxSessions != 0 || cfg.IdleTimeout != 0 {
		t.Fatalf("zero knobs should stay zero for InitSSHPool defaults: %+v", cfg)
	}
}

func TestNeedsReset(t *testing.T) {
	p := hygieneProfiles["vrp"]
	if needsReset([]sshCmd{{Cmd: "display version"}}, p) {
		t.Fatal("read should not reset")
	}
	if needsReset([]sshCmd{{Cmd: "system-view"}, {Cmd: "quit"}}, p) {
		t.Fatal("exited via quit should not reset")
	}
	if !needsReset([]sshCmd{{Cmd: "system-view"}}, p) {
		t.Fatal("leftover system-view should reset")
	}
	if needsReset([]sshCmd{{Cmd: "display | include system-view"}}, p) {
		t.Fatal("substring must not count as entry")
	}
}
