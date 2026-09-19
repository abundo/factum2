package drivers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
)

func TestSessionSocketPath(t *testing.T) {
	t.Setenv("FACTUM_DRIVER_SESSION_SOCKET", "")
	if got := SessionSocketPath(""); got != DefaultSessionSocket {
		t.Fatalf("default = %q", got)
	}
	if got := SessionSocketPath("/tmp/custom.sock"); got != "/tmp/custom.sock" {
		t.Fatalf("yaml override = %q", got)
	}
	if got := SessionSocketPath("none"); got != "" {
		t.Fatalf("none = %q", got)
	}
	if got := SessionSocketPath("0"); got != "" {
		t.Fatalf("0 = %q", got)
	}
	t.Setenv("FACTUM_DRIVER_SESSION_SOCKET", "/tmp/env.sock")
	if got := SessionSocketPath(""); got != "/tmp/env.sock" {
		t.Fatalf("env = %q", got)
	}
	if got := SessionSocketPath("/tmp/yaml.sock"); got != "/tmp/yaml.sock" {
		t.Fatalf("yaml wins = %q", got)
	}
	t.Setenv("FACTUM_DRIVER_SESSION_SOCKET", "none")
	if got := SessionSocketPath(""); got != "" {
		t.Fatalf("env none = %q", got)
	}
}

func TestParseSessionListen(t *testing.T) {
	t.Setenv("FACTUM_DRIVER_SESSION_SOCKET", "")
	if _, err := parseSessionListen(util.ConfigDriver{}); err != nil {
		t.Fatalf("unix-only default: %v", err)
	}
	if _, err := parseSessionListen(util.ConfigDriver{Socket: "none"}); err == nil {
		t.Fatal("socket none and no listen should fail")
	}
	if _, err := parseSessionListen(util.ConfigDriver{Listen: "127.0.0.1:8092"}); err == nil {
		t.Fatal("TCP without token should fail")
	}
	if _, err := parseSessionListen(util.ConfigDriver{Listen: "127.0.0.1:8092", Token: "s"}); err != nil {
		t.Fatalf("loopback TCP with token: %v", err)
	}
	_, err := parseSessionListen(util.ConfigDriver{Listen: "0.0.0.0:8092", Token: "s"})
	if err == nil || !strings.Contains(err.Error(), "tls_cert") {
		t.Fatalf("non-loopback without TLS: %v", err)
	}
	_, err = parseSessionListen(util.ConfigDriver{
		Listen:  "192.0.2.1:8092",
		Token:   "s",
		TLSCert: "/nope.crt",
		TLSKey:  "/nope.key",
	})
	if err == nil || !strings.Contains(err.Error(), "allow_cidrs") {
		t.Fatalf("non-loopback without CIDR: %v", err)
	}
}

func TestEndMarkerTokenPointerEquality(t *testing.T) {
	if endMarkerToken(vrpConfigEndMarker) != "vrp_config" {
		t.Fatal("vrp package var")
	}
	if endMarkerToken(iosxrConfigEndMarker) != "iosxr_config" {
		t.Fatal("iosxr package var")
	}
	if endMarkerToken(srosConfigEndMarker) != "sros_config" {
		t.Fatal("sros package var")
	}
	copyRE := vrpConfigEndMarker.Copy()
	if endMarkerToken(copyRE) != "" {
		t.Fatal("copy of regexp must not map to a token")
	}
}

func TestHTTPRoundTrip(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host, port := f.addr()
	body := cliRunRequestJSON{
		Host:     host,
		Port:     port,
		Username: f.user,
		Password: f.pass,
		Platform: "vrp",
		Mode:     "batch",
		Cmds:     []cliRunCmdJSON{{Cmd: "display version"}},
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/cli/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body %s", resp.StatusCode, b)
	}
	var out cliRunResponseJSON
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Outputs) == 0 || !strings.Contains(out.Outputs[len(out.Outputs)-1], "VRP version") {
		t.Fatalf("outputs=%v", out.Outputs)
	}
}

func TestUnixHTTPProxyIgnored(t *testing.T) {
	ResetSSHPoolForTest()
	dir := t.TempDir()
	sock := filepath.Join(dir, "session.sock")
	ln, err := listenSessionUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: newSessionHandler(&sessionMux{}), ReadHeaderTimeout: sessionReadHeader}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")

	c := newSessionUnixClient(sock)
	tr, ok := c.client.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatal("unix transport Proxy must be nil")
	}
	req, err := http.NewRequest(http.MethodGet, sessionUnixBaseURL+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestUnixNoAuthorization(t *testing.T) {
	ResetSSHPoolForTest()
	dir := t.TempDir()
	sock := filepath.Join(dir, "session.sock")
	ln, err := listenSessionUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: newSessionHandler(&sessionMux{}), ReadHeaderTimeout: sessionReadHeader}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	c := newSessionUnixClient(sock)
	req, _ := http.NewRequest(http.MethodGet, sessionUnixBaseURL+"/v1/sessions", nil)
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("unix without Authorization: %d", resp.StatusCode)
	}
}

func TestTCPUnauthorized(t *testing.T) {
	ResetSSHPoolForTest()
	mux := newSessionHandler(&sessionMux{token: "s3cret"})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
	resp2, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("loopback /health should be unauthenticated, got %d", resp2.StatusCode)
	}
}

func TestNonLoopbackHealthRequiresAuth(t *testing.T) {
	ResetSSHPoolForTest()
	mux := newSessionHandler(&sessionMux{token: "s3cret", authHealth: true})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestUnknownEndMarker400(t *testing.T) {
	ResetSSHPoolForTest()
	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	body := cliRunRequestJSON{
		Host: "h", Username: "u", Platform: "vrp",
		Cmds: []cliRunCmdJSON{{Cmd: "display version", EndMarker: "not_a_token"}},
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/cli/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestOversizeBody413(t *testing.T) {
	ResetSSHPoolForTest()
	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	resp, err := http.Post(srv.URL+"/v1/cli/run", "application/json", bytes.NewReader(bytes.Repeat([]byte("a"), sessionMaxBody+1)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestQueueFull429(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	f.hangOn = "hang"
	if err := initSSHPoolForTest(SSHPoolConfig{QueueDepth: 1, MaxSessions: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	p := f.param("vrp")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "hang"}})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countCmd(f.commands(), "hang") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	queued := make(chan error, 1)
	go func() {
		_, err := sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "display version"}})
		queued <- err
	}()
	time.Sleep(50 * time.Millisecond)
	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host, port := f.addr()
	body := cliRunRequestJSON{
		Host: host, Port: port, Username: f.user, Password: f.pass, Platform: "vrp",
		Cmds: []cliRunCmdJSON{{Cmd: "display clock"}},
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/cli/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body %s", resp.StatusCode, b)
	}
	close(f.hangRelease)
	<-done
	<-queued
}

func TestDeleteSessionKeyWithSlash(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	p := f.param("vrp")
	if _, err := sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	key := sessionKey(p)
	if !strings.Contains(key, "/") {
		t.Fatalf("key %q should contain /", key)
	}
	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/sessions?key="+key, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body %s", resp.StatusCode, b)
	}
	st := getPool().Stats()
	if len(st) != 0 {
		t.Fatalf("stats after delete: %+v", st)
	}
}

func TestCLIRunDoesNotHTTPToOwnSocket(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	sshGlobals.mu.Lock()
	remoteOn := sshGlobals.remote != nil
	sshGlobals.mu.Unlock()
	if remoteOn {
		t.Fatal("start-style pool must not configure a session HTTP client")
	}

	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host, port := f.addr()
	body := cliRunRequestJSON{
		Host: host, Port: port, Username: f.user, Password: f.pass, Platform: "vrp",
		Cmds: []cliRunCmdJSON{{Cmd: "display version"}},
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/cli/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body %s", resp.StatusCode, b)
	}
}

func TestMissingSocketFallsBackInProcess(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	sock := filepath.Join(t.TempDir(), "missing.sock")
	if err := initSSHPoolForTest(SSHPoolConfig{Socket: sock}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	out, err := sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "VRP version") {
		t.Fatalf("output = %q", out)
	}
}

func TestConnectionRefusedFallsBackInProcess(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "stale.sock")
	if err := os.WriteFile(sock, []byte{}, 0o660); err != nil {
		t.Fatal(err)
	}
	if err := initSSHPoolForTest(SSHPoolConfig{Socket: sock}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	out, err := sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "VRP version") {
		t.Fatalf("output = %q", out)
	}
}

func TestHTTP502NotReplayedLocally(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		writeSessionJSONError(w, http.StatusBadGateway, "device exploded")
	}))
	t.Cleanup(ts.Close)
	if err := initSSHPoolForTest(SSHPoolConfig{SessionURL: ts.URL}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	_, err := sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}})
	if err == nil {
		t.Fatal("expected 502 error")
	}
	var rs *remoteStatusError
	if !errors.As(err, &rs) || rs.status != 502 {
		t.Fatalf("err = %v", err)
	}
	if f.connections() != 0 {
		t.Fatalf("local apply connections = %d, want 0", f.connections())
	}
	if hits != 1 {
		t.Fatalf("hits %d", hits)
	}
}

func TestAllowCIDREveryAddress(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/8")
	m := &sessionMux{enforceCIDR: true, cidrs: []*net.IPNet{n}}
	m.lookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.1.2.3"), net.ParseIP("8.8.8.8")}, nil
	}
	if err := m.checkHost(context.Background(), "dual.example"); err == nil {
		t.Fatal("want forbidden when any A is outside")
	}
	m.lookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.1.2.3"), net.ParseIP("10.9.9.9")}, nil
	}
	if err := m.checkHost(context.Background(), "ok.example"); err != nil {
		t.Fatal(err)
	}
	m.lookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return nil, fmt.Errorf("nxdomain")
	}
	if err := m.checkHost(context.Background(), "missing.example"); err == nil {
		t.Fatal("DNS fail should forbid")
	}
}

func TestServeSSHSessionUnixAndSIGTERM(t *testing.T) {
	ResetSSHPoolForTest()
	dir := t.TempDir()
	sock := filepath.Join(dir, "session.sock")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- ServeSSHSession(ctx, util.ConfigDriver{Socket: sock})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("socket not created")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c := newSessionUnixClient(sock)
	req, _ := http.NewRequest(http.MethodGet, sessionUnixBaseURL+"/health", nil)
	resp, err := c.client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		cancel()
		t.Fatalf("health %d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ServeSSHSession did not return after cancel")
	}
}

func TestTLSRequiredFilesLoad(t *testing.T) {
	cert, key := writeTestCert(t, "127.0.0.1")
	_, err := parseSessionListen(util.ConfigDriver{
		Listen:     "192.0.2.10:8092",
		Token:      "tok",
		TLSCert:    cert,
		TLSKey:     key,
		AllowCIDRs: []string{"10.0.0.0/8"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStalledTLSFallsBackAndStickyDown(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(time.Minute)
			}(c)
		}
	}()
	oldHS := sessionTLSHandshakeTimeout
	oldRetry := remoteRetry
	sessionTLSHandshakeTimeout = 80 * time.Millisecond
	remoteRetry = time.Hour
	t.Cleanup(func() {
		sessionTLSHandshakeTimeout = oldHS
		remoteRetry = oldRetry
	})
	if err := initSSHPoolForTest(SSHPoolConfig{SessionURL: "https://" + ln.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	out, err := sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "VRP version") {
		t.Fatalf("output = %q", out)
	}
	sshGlobals.mu.Lock()
	down := sshGlobals.remote != nil && sshGlobals.remote.down
	sshGlobals.mu.Unlock()
	if !down {
		t.Fatal("want sticky down after stalled TLS handshake")
	}
	if _, err := sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if f.connections() != 1 {
		t.Fatalf("connections = %d, want in-process reuse while down", f.connections())
	}
}

func TestHealthReprobeRequiresHTTPResponse(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(time.Minute)
			}(c)
		}
	}()
	oldHS := sessionTLSHandshakeTimeout
	oldRetry := remoteRetry
	oldProbe := sessionProbeTimeout
	sessionTLSHandshakeTimeout = 80 * time.Millisecond
	sessionProbeTimeout = 200 * time.Millisecond
	remoteRetry = time.Hour
	t.Cleanup(func() {
		sessionTLSHandshakeTimeout = oldHS
		sessionProbeTimeout = oldProbe
		remoteRetry = oldRetry
	})
	if err := initSSHPoolForTest(SSHPoolConfig{SessionURL: "https://" + ln.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	if _, err := sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if len(getPool().Stats()) != 1 {
		t.Fatal("want one in-process session after fallback")
	}
	remoteRetry = 0
	if _, err := sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}}); err != nil {
		t.Fatal(err)
	}
	if len(getPool().Stats()) != 1 {
		t.Fatal("TCP-only re-probe must not replaceMemoryPool")
	}
	if f.connections() != 1 {
		t.Fatalf("connections = %d, want reuse", f.connections())
	}
	sshGlobals.mu.Lock()
	down := sshGlobals.remote != nil && sshGlobals.remote.down
	sshGlobals.mu.Unlock()
	if !down {
		t.Fatal("want still down after failed health probe")
	}
}

func TestTLSErrorAfterRequestNotReplayed(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	certPath, keyPath := writeTestCert(t, "127.0.0.1")
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		sc := tls.Server(c, tlsCfg)
		if err := sc.Handshake(); err != nil {
			return
		}
		buf := make([]byte, 256)
		_, _ = sc.Read(buf)
		_ = sc.Close()
	}()
	if err := initSSHPoolForTest(SSHPoolConfig{
		SessionURL: "https://" + ln.Addr().String(),
		TLSCA:      certPath,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	_, err = sshRunCLI(context.Background(), f.param("vrp"), []sshCmd{{Cmd: "display version"}})
	if err == nil {
		t.Fatal("expected error after peer close")
	}
	if errors.Is(err, errRemoteConnect) {
		t.Fatalf("must not fallback after request written: %v", err)
	}
	if f.connections() != 0 {
		t.Fatalf("local apply connections = %d, want 0", f.connections())
	}
}

func TestDaemonRespectsPlatformKillSwitch(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	off := []string{}
	if err := initSSHPoolForTest(SSHPoolConfig{Platforms: &off}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ResetSSHPoolForTest)
	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host, port := f.addr()
	body := cliRunRequestJSON{
		Host: host, Port: port, Username: f.user, Password: f.pass, Platform: "vrp",
		Cmds: []cliRunCmdJSON{{Cmd: "display version"}},
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/cli/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(getPool().Stats()) != 0 {
		t.Fatalf("kill switch must not pool: %+v", getPool().Stats())
	}
	if f.connections() != 0 {
		t.Fatalf("legacy SSH CLI dials :22, not JSON port; fake connections = %d", f.connections())
	}
}

func TestDaemonDoesNotPoolUnprofiledPlatform(t *testing.T) {
	useFastSSHIdle(t)
	f := startFakeSSH(t)
	ResetSSHPoolForTest()
	mux := newSessionHandler(&sessionMux{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host, port := f.addr()
	body := cliRunRequestJSON{
		Host: host, Port: port, Username: f.user, Password: f.pass, Platform: "ios-xr",
		Cmds: []cliRunCmdJSON{{Cmd: "display version"}},
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/v1/cli/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(getPool().Stats()) != 0 {
		t.Fatalf("ios-xr must not be pooled: %+v", getPool().Stats())
	}
	if f.connections() != 0 {
		t.Fatalf("legacy SSH CLI dials :22, not JSON port; fake connections = %d", f.connections())
	}
}

func writeTestCert(t *testing.T, ip string) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP(ip)},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "c.pem")
	keyPath = filepath.Join(dir, "k.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
