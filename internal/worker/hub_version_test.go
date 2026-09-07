package worker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestCheckHubVersionMatch(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.2.3", "abc123"

	if err := checkHubVersion("v1.2.3", "abc123"); err != nil {
		t.Fatalf("matching identity: %v", err)
	}
}

func TestCheckHubVersionMismatch(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.2.3", "abc123"

	err := checkHubVersion("v9.9.9", "deadbeef")
	if err == nil {
		t.Fatal("want mismatch error")
	}
	if !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("err = %v, want version mismatch", err)
	}
	if !strings.Contains(err.Error(), "v9.9.9") || !strings.Contains(err.Error(), "v1.2.3") {
		t.Fatalf("err = %v, want both versions", err)
	}
}

func TestCheckHubVersionEmptyIsMismatch(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.2.3", "abc123"

	err := checkHubVersion("", "")
	if err == nil {
		t.Fatal("want mismatch for missing identity")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err = %v, want unknown for empty peer identity", err)
	}
}

func TestCheckHubVersionCommitMismatch(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.2.3", "abc123"

	if err := checkHubVersion("v1.2.3", "other"); err == nil {
		t.Fatal("want mismatch when only commit differs")
	}
}

func TestCheckHubVersionSkipsWhenLocalDev(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "dev", "none"

	if err := checkHubVersion("v1.2.3", "abc123"); err != nil {
		t.Fatalf("unstamped local should skip: %v", err)
	}
}

func TestCheckHubVersionSkipsWhenPeerDev(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.2.3", "abc123"

	if err := checkHubVersion("dev", "none"); err != nil {
		t.Fatalf("unstamped peer should skip: %v", err)
	}
}

func TestCheckHubVersionGitDescribeStillChecked(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.0.0-3-gdeadbee", "deadbee"

	err := checkHubVersion("v1.0.0-4-gabcdef", "abcdef")
	if err == nil {
		t.Fatal("want mismatch for different git-describe stamps")
	}
}

func TestHandleHubConnRejectsVersionMismatch(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.0.0", "aaa"

	w := New(&util.ConfigWorker{
		Token: "secret",
		Commands: map[string]util.ConfigWorkerCommand{
			"dns": {Cmd: "/bin/true"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, HubPath, nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(hubVersionHeader, "v9.9.9")
	req.Header.Set(hubCommitHeader, "bbb")
	rec := httptest.NewRecorder()
	w.handleHubConn(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "version mismatch") {
		t.Fatalf("body %q, want version mismatch", rec.Body.String())
	}
}

func TestHandleHubConnRejectsMissingVersion(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.0.0", "aaa"

	w := New(&util.ConfigWorker{
		Token: "secret",
		Commands: map[string]util.ConfigWorkerCommand{
			"dns": {Cmd: "/bin/true"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, HubPath, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	w.handleHubConn(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHubConnAllowsDevPrimary(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.0.0", "aaa"

	w := New(&util.ConfigWorker{
		Token: "secret",
		Commands: map[string]util.ConfigWorkerCommand{
			"dns": {Cmd: "/bin/true"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, HubPath, nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(hubVersionHeader, "dev")
	req.Header.Set(hubCommitHeader, "none")
	rec := httptest.NewRecorder()
	w.handleHubConn(rec, req)
	if rec.Code == http.StatusConflict {
		t.Fatalf("unstamped primary rejected: %s", rec.Body.String())
	}
}

func TestHandleHubConnRejectsTokenBeforeVersion(t *testing.T) {
	w := New(&util.ConfigWorker{
		Token: "secret",
		Commands: map[string]util.ConfigWorkerCommand{
			"dns": {Cmd: "/bin/true"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, HubPath, nil)
	req.Header.Set("Authorization", "Bearer wrong")
	req.Header.Set(hubVersionHeader, "v9.9.9")
	req.Header.Set(hubCommitHeader, "bbb")
	rec := httptest.NewRecorder()
	w.handleHubConn(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestConnectOnceRejectsHTTPVersionMismatch(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "version mismatch: peer v1.0.0 (aaa) != local v9.9.9 (bbb)", http.StatusConflict)
	}))
	srv.TLS = &tls.Config{NextProtos: []string{"http/1.1"}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	m := NewRemoteManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connected, err := m.connectOnce(ctx, models.WorkerNode{
		Name:          "n1",
		Address:       hubTestAddr(t, srv),
		Token:         "secret",
		TLSSkipVerify: true,
	})
	if connected {
		t.Fatal("connected on version mismatch")
	}
	if err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("err = %v, want version mismatch", err)
	}
	st := m.StatusAll()["n1"]
	if st.Connected {
		t.Fatal("status connected on version mismatch")
	}
	if !strings.Contains(st.LastError, "version mismatch") {
		t.Fatalf("LastError = %q, want version mismatch", st.LastError)
	}
}

func TestConnectOnceRejectsHelloVersionMismatch(t *testing.T) {
	origV, origC := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origV, origC
	})
	buildinfo.Version, buildinfo.Commit = "v1.0.0", "aaa"

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := hubUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		payload, err := json.Marshal(HelloMsg{
			Hostname: "worker-1",
			Roles:    []string{"dns"},
			Version:  "v9.9.9",
			Commit:   "bbb",
		})
		if err != nil {
			return
		}
		_ = conn.WriteJSON(Envelope{Type: EnvelopeHello, Payload: payload})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	srv.TLS = &tls.Config{NextProtos: []string{"http/1.1"}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	m := NewRemoteManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connected, err := m.connectOnce(ctx, models.WorkerNode{
		Name:          "n1",
		Address:       hubTestAddr(t, srv),
		Token:         "secret",
		TLSSkipVerify: true,
	})
	if connected {
		t.Fatal("connected on hello version mismatch")
	}
	if err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("err = %v, want version mismatch", err)
	}
	m.mu.Lock()
	_, registered := m.conns["n1"]
	m.mu.Unlock()
	if registered {
		t.Fatal("node registered in conns after version mismatch")
	}
	st := m.StatusAll()["n1"]
	if st.Connected {
		t.Fatal("status connected on hello version mismatch")
	}
	if st.Hostname != "worker-1" {
		t.Fatalf("hostname %q, want worker-1", st.Hostname)
	}
	if st.Version != "v9.9.9" {
		t.Fatalf("version %q, want v9.9.9", st.Version)
	}
}

func TestHubDialErrorConflictUsesBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusConflict,
		Body:       io.NopCloser(strings.NewReader("version mismatch: peer a != local b\n")),
	}
	err := hubDialError(errors.New("bad handshake"), resp)
	if err == nil || err.Error() != "version mismatch: peer a != local b" {
		t.Fatalf("err = %v", err)
	}
}

func TestHubDialErrorNonConflictKeepsDialErr(t *testing.T) {
	dialErr := errors.New("bad handshake")
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader("unauthorized\n")),
	}
	err := hubDialError(dialErr, resp)
	if !errors.Is(err, dialErr) {
		t.Fatalf("err = %v, want dialErr", err)
	}
}
