package util

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestHubSocketPath(t *testing.T) {
	t.Setenv("FACTUM_WORKER_API_SOCKET", "")
	if got := HubSocketPath(""); got != DefaultHubSocket {
		t.Fatalf("empty = %q, want default", got)
	}
	if got := HubSocketPath("/tmp/custom.sock"); got != "/tmp/custom.sock" {
		t.Fatalf("yaml override = %q", got)
	}
	if got := HubSocketPath("none"); got != "" {
		t.Fatalf("yaml none = %q, want empty", got)
	}
	if got := HubSocketPath("0"); got != "" {
		t.Fatalf("yaml 0 = %q, want empty", got)
	}

	t.Setenv("FACTUM_WORKER_API_SOCKET", "/tmp/env.sock")
	if got := HubSocketPath(""); got != "/tmp/env.sock" {
		t.Fatalf("env = %q", got)
	}
	if got := HubSocketPath("/tmp/yaml.sock"); got != "/tmp/yaml.sock" {
		t.Fatalf("yaml beats env: %q", got)
	}

	t.Setenv("FACTUM_WORKER_API_SOCKET", "none")
	if got := HubSocketPath(""); got != "" {
		t.Fatalf("env none = %q, want empty", got)
	}
}

type probePayload struct {
	DefaultDomain string `json:"default_domain"`
}

func serveUnixHTTP(t *testing.T, h http.Handler) string {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "api.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	deadline := time.Now().Add(time.Second)
	for {
		c, err := net.DialTimeout("unix", socket, 50*time.Millisecond)
		if err == nil {
			c.Close()
			return socket
		}
		if time.Now().After(deadline) {
			t.Fatalf("unix server not ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func serveHTTPSConfig(t *testing.T, status int, body string, hits *atomic.Int32, auth *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if auth != nil {
			auth.Store(r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFactumHTTPUnix(t *testing.T) {
	var unixAuth atomic.Value
	unixAuth.Store("")
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unixAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"default_domain":"unix"}`))
	}))
	var httpsHits atomic.Int32
	https := serveHTTPSConfig(t, http.StatusOK, `{"default_domain":"https"}`, &httpsHits, nil)

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")

	cfg := &ConfigFactum{URL: https.URL, Token: "secret", Socket: sock}
	client, baseURL, viaSocket, err := FactumHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !viaSocket {
		t.Fatal("want viaSocket")
	}
	if baseURL != factumUnixBaseURL {
		t.Fatalf("baseURL = %q, want %q", baseURL, factumUnixBaseURL)
	}
	if client.Timeout != HubRPCTimeout {
		t.Fatalf("timeout %s, want %s", client.Timeout, HubRPCTimeout)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T, want *http.Transport", client.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("unix transport Proxy must be nil")
	}

	got, err := FetchRemoteConfig[probePayload](cfg, "/api/librenms-config")
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultDomain != "unix" {
		t.Fatalf("got %+v, want unix payload", got)
	}
	if httpsHits.Load() != 0 {
		t.Fatalf("https hits %d, want 0", httpsHits.Load())
	}
	if auth, _ := unixAuth.Load().(string); auth != "" {
		t.Fatalf("unix Authorization = %q, want empty", auth)
	}
}

func TestFactumHTTPHTTPSWhenSocketMissing(t *testing.T) {
	var httpsAuth atomic.Value
	httpsAuth.Store("")
	https := serveHTTPSConfig(t, http.StatusOK, `{"default_domain":"https"}`, nil, &httpsAuth)
	cfg := &ConfigFactum{
		URL:    https.URL,
		Token:  "secret",
		Socket: filepath.Join(t.TempDir(), "missing.sock"),
	}
	client, baseURL, viaSocket, err := FactumHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if viaSocket {
		t.Fatal("want HTTPS, got unix")
	}
	if baseURL != https.URL {
		t.Fatalf("baseURL = %q, want %q", baseURL, https.URL)
	}
	if client.Timeout != HubRPCTimeout {
		t.Fatalf("timeout %s, want %s", client.Timeout, HubRPCTimeout)
	}

	got, err := FetchRemoteConfig[probePayload](cfg, "/api/librenms-config")
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultDomain != "https" {
		t.Fatalf("got %+v", got)
	}
	if auth, _ := httpsAuth.Load().(string); auth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", auth)
	}
}

func TestFactumHTTPNeitherError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.sock")
	_, _, _, err := FactumHTTP(&ConfigFactum{Socket: missing})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "factum.url") {
		t.Fatalf("err = %v, want socket path and factum.url", err)
	}

	_, _, _, err = FactumHTTP(&ConfigFactum{Socket: "none"})
	if err == nil {
		t.Fatal("want error when socket disabled and URL unset")
	}
	if !strings.Contains(err.Error(), "hub socket disabled") || !strings.Contains(err.Error(), "factum.url") {
		t.Fatalf("err = %v, want disabled + factum.url", err)
	}
}

func TestFactumHTTPStatPermissionFallsBackToHTTPS(t *testing.T) {
	https := serveHTTPSConfig(t, http.StatusOK, `{"default_domain":"https"}`, nil, nil)
	sock := filepath.Join(t.TempDir(), "api.sock")

	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM} {
		t.Run(errno.Error(), func(t *testing.T) {
			orig := osStat
			osStat = func(name string) (os.FileInfo, error) {
				return nil, &os.PathError{Op: "stat", Path: name, Err: errno}
			}
			t.Cleanup(func() { osStat = orig })

			cfg := &ConfigFactum{URL: https.URL, Token: "secret", Socket: sock}
			_, baseURL, viaSocket, err := FactumHTTP(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if viaSocket {
				t.Fatal("want HTTPS after permission error")
			}
			if baseURL != https.URL {
				t.Fatalf("baseURL = %q", baseURL)
			}

			got, err := FetchRemoteConfig[probePayload](cfg, "/api/librenms-config")
			if err != nil {
				t.Fatal(err)
			}
			if got.DefaultDomain != "https" {
				t.Fatalf("got %+v", got)
			}

			_, _, _, err = FactumHTTP(&ConfigFactum{Socket: sock})
			if err == nil {
				t.Fatal("want error when socket is unreadable and URL unset")
			}
			if !strings.Contains(err.Error(), "not readable") || !strings.Contains(err.Error(), "factum.url") {
				t.Fatalf("err = %v, want not readable + factum.url", err)
			}
		})
	}
}

func TestFactumHTTPStaleSocketDialErrorFallsBackToHTTPS(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "api.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	ln.Close()

	https := serveHTTPSConfig(t, http.StatusOK, `{"default_domain":"https"}`, nil, nil)
	cfg := &ConfigFactum{URL: https.URL, Token: "secret", Socket: sock}
	_, baseURL, viaSocket, err := FactumHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if viaSocket {
		t.Fatal("want HTTPS after stale socket Dial error")
	}
	if baseURL != https.URL {
		t.Fatalf("baseURL = %q", baseURL)
	}

	got, err := FetchRemoteConfig[probePayload](cfg, "/api/librenms-config")
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultDomain != "https" {
		t.Fatalf("got %+v", got)
	}

	_, _, _, err = FactumHTTP(&ConfigFactum{Socket: sock})
	if err == nil {
		t.Fatal("want Dial error when URL unset")
	}
	if !strings.Contains(err.Error(), sock) {
		t.Fatalf("err = %v, want socket path", err)
	}
}

func TestFetchRemoteConfigUnix502DoesNotRetryHTTPS(t *testing.T) {
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"hub disconnected"}`))
	}))
	var httpsHits atomic.Int32
	https := serveHTTPSConfig(t, http.StatusOK, `{"default_domain":"https"}`, &httpsHits, nil)

	_, err := FetchRemoteConfig[probePayload](&ConfigFactum{
		URL:    https.URL,
		Token:  "secret",
		Socket: sock,
	}, "/api/librenms-config")
	if err == nil {
		t.Fatal("want 502 error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want 502", err)
	}
	if httpsHits.Load() != 0 {
		t.Fatalf("https hits %d, unix 502 must not retry HTTPS", httpsHits.Load())
	}
}

func TestFactumHTTPEnvNoneForcesHTTPSEvenIfSocketExists(t *testing.T) {
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"default_domain":"unix"}`))
	}))
	https := serveHTTPSConfig(t, http.StatusOK, `{"default_domain":"https"}`, nil, nil)

	t.Setenv("FACTUM_WORKER_API_SOCKET", sock)
	cfgUnix := &ConfigFactum{URL: https.URL, Token: "secret"}
	_, _, viaSocket, err := FactumHTTP(cfgUnix)
	if err != nil {
		t.Fatal(err)
	}
	if !viaSocket {
		t.Fatal("env socket path should select unix")
	}

	t.Setenv("FACTUM_WORKER_API_SOCKET", "none")
	cfg := &ConfigFactum{URL: https.URL, Token: "secret"}
	_, baseURL, viaSocket, err := FactumHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if viaSocket {
		t.Fatal("FACTUM_WORKER_API_SOCKET=none must force HTTPS")
	}
	if baseURL != https.URL {
		t.Fatalf("baseURL = %q", baseURL)
	}

	got, err := FetchRemoteConfig[probePayload](cfg, "/api/librenms-config")
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultDomain != "https" {
		t.Fatalf("got %+v, want https payload", got)
	}

	cfg.Socket = "none"
	t.Setenv("FACTUM_WORKER_API_SOCKET", sock)
	_, _, viaSocket, err = FactumHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if viaSocket {
		t.Fatal("yaml socket none must force HTTPS even if env names a live socket")
	}
}

func TestFetchRemoteConfigUnixNoBearer(t *testing.T) {
	var unixAuth atomic.Value
	unixAuth.Store("unset")
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unixAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"default_domain":"unix"}`))
	}))

	_, err := FetchRemoteConfig[probePayload](&ConfigFactum{
		URL:    "http://127.0.0.1:1",
		Token:  "secret",
		Socket: sock,
	}, "/api/common-config")
	if err != nil {
		t.Fatal(err)
	}
	if auth, _ := unixAuth.Load().(string); auth != "" {
		t.Fatalf("unix Authorization = %q, want empty", auth)
	}
}
