package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
)

func decodeResponse(t *testing.T, env Envelope) ResponseMsg {
	t.Helper()
	if env.Type != EnvelopeResponse {
		t.Fatalf("envelope type %q, want %q", env.Type, EnvelopeResponse)
	}
	var resp ResponseMsg
	if err := json.Unmarshal(env.Payload, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

func (m *RemoteManager) inFlightCount(node string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inFlight[node]
}

func TestHandleHubRequestSuccessInFlightReturnsToZero(t *testing.T) {
	m := NewRemoteManager(nil)
	m.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsHubAuth(r.Context()) {
			t.Error("ServeHTTP request missing hub auth")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	outbox := make(chan Envelope, 1)
	m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
		ID: "1", Method: http.MethodGet, Path: "/api/librenms-config",
	})
	resp := decodeResponse(t, <-outbox)
	if resp.Status != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.Status)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Fatalf("body %s", resp.Body)
	}
	if got := m.inFlightCount("n1"); got != 0 {
		t.Fatalf("inFlight after success = %d, want 0", got)
	}
}

func TestHandleHubRequestInFlightCapAnd429DoesNotIncrement(t *testing.T) {
	m := NewRemoteManager(nil)
	entered := make(chan struct{}, hubRPCMaxInFlight)
	release := make(chan struct{})
	m.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	outboxes := make([]chan Envelope, hubRPCMaxInFlight)
	for i := 0; i < hubRPCMaxInFlight; i++ {
		ob := make(chan Envelope, 1)
		outboxes[i] = ob
		go m.HandleHubRequest(context.Background(), "n1", ob, RequestMsg{
			ID: fmt.Sprintf("%d", i), Method: http.MethodGet, Path: "/api/librenms-config",
		})
	}
	for i := 0; i < hubRPCMaxInFlight; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not start")
		}
	}
	if got := m.inFlightCount("n1"); got != hubRPCMaxInFlight {
		t.Fatalf("inFlight = %d, want %d", got, hubRPCMaxInFlight)
	}

	ob := make(chan Envelope, 1)
	m.HandleHubRequest(context.Background(), "n1", ob, RequestMsg{
		ID: "overflow", Method: http.MethodGet, Path: "/api/librenms-config",
	})
	resp := decodeResponse(t, <-ob)
	if resp.Status != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", resp.Status)
	}
	if resp.Error != "" {
		t.Fatalf("429 must use Status+Body, not Error: %+v", resp)
	}
	if got := m.inFlightCount("n1"); got != hubRPCMaxInFlight {
		t.Fatalf("inFlight after 429 = %d, want %d (must not increment)", got, hubRPCMaxInFlight)
	}

	close(release)
	for i := 0; i < hubRPCMaxInFlight; i++ {
		select {
		case <-outboxes[i]:
		case <-time.After(2 * time.Second):
			t.Fatal("in-flight RPC did not finish")
		}
	}
	if got := m.inFlightCount("n1"); got != 0 {
		t.Fatalf("inFlight after drain = %d, want 0", got)
	}
}

func TestHandleHubRequestAllowlistBeforeHandler(t *testing.T) {
	m := NewRemoteManager(nil)
	var hits int
	m.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	outbox := make(chan Envelope, 1)
	m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
		ID: "1", Method: http.MethodGet, Path: "/api/admin/settings",
	})
	resp := decodeResponse(t, <-outbox)
	if resp.Status != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.Status)
	}
	if hits != 0 {
		t.Fatalf("handler hits = %d, allowlist must reject before ServeHTTP", hits)
	}
}

func TestHandleHubRequestRejectsTraversal(t *testing.T) {
	m := NewRemoteManager(nil)
	var hits int
	m.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{
		"/api/librenms-config/../../admin/settings",
		"http://evil/api/librenms-config",
		"//evil/api/librenms-config",
		"/api/librenms-config#x",
	} {
		outbox := make(chan Envelope, 1)
		m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
			ID: "1", Method: http.MethodGet, Path: path,
		})
		resp := decodeResponse(t, <-outbox)
		if resp.Status != http.StatusBadRequest {
			t.Errorf("path %q status %d, want 400", path, resp.Status)
		}
	}
	if hits != 0 {
		t.Fatalf("handler hits = %d", hits)
	}
}

func TestHandleHubRequestNilHandler(t *testing.T) {
	m := NewRemoteManager(nil)
	outbox := make(chan Envelope, 1)
	m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
		ID: "1", Method: http.MethodGet, Path: "/api/librenms-config",
	})
	resp := decodeResponse(t, <-outbox)
	if resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", resp.Status)
	}
	if got := m.inFlightCount("n1"); got != 0 {
		t.Fatalf("inFlight after 503 = %d, want 0", got)
	}
}

func waitWaiters(t *testing.T, sess *hubSession, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess.mu.Lock()
		got := len(sess.waiters)
		sess.mu.Unlock()
		if got == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d waiters", n)
}

func TestDoHubRequestFailsOnDisconnect(t *testing.T) {
	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := w.DoHubRequest(context.Background(), http.MethodGet, "/api/librenms-config", nil)
		errCh <- err
	}()
	waitWaiters(t, sess, 1)
	w.clearHubSession(sess)
	err := <-errCh
	if err == nil || err.Error() != "hub disconnected" {
		t.Fatalf("err = %v, want hub disconnected", err)
	}
}

func TestDoHubRequestLatestWinsReplacedConn(t *testing.T) {
	w := New(&util.ConfigWorker{})
	sess1 := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess1)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := w.DoHubRequest(context.Background(), http.MethodGet, "/api/librenms-config", nil)
		errCh <- err
	}()
	waitWaiters(t, sess1, 1)

	sess2 := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess2)
	err := <-errCh
	if err == nil || err.Error() != "hub connection replaced" {
		t.Fatalf("err = %v, want hub connection replaced", err)
	}

	go func() {
		env := <-sess2.outbox
		var req RequestMsg
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		sess2.mu.Lock()
		ch := sess2.waiters[req.ID]
		sess2.mu.Unlock()
		ch <- ResponseMsg{ID: req.ID, Status: http.StatusOK, Body: json.RawMessage(`{"ok":true}`)}
	}()
	status, body, err := w.DoHubRequest(context.Background(), http.MethodGet, "/api/librenms-config", nil)
	if err != nil {
		t.Fatalf("latest session: %v", err)
	}
	if status != http.StatusOK || string(body) != `{"ok":true}` {
		t.Fatalf("status=%d body=%s", status, body)
	}
}

func TestDoHubRequestTimeoutUnregisters(t *testing.T) {
	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := w.DoHubRequest(ctx, http.MethodGet, "/api/librenms-config", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	sess.mu.Lock()
	n := len(sess.waiters)
	sess.mu.Unlock()
	if n != 0 {
		t.Fatalf("waiters leaked: %d", n)
	}
}

func TestHandleLocalAPITransportErrorIs502(t *testing.T) {
	w := New(&util.ConfigWorker{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/librenms-config", nil)
	w.handleLocalAPI(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"error":`) && rec.Code == 200 {
		t.Fatal("must not return HTTP 200 with an error string")
	}
}

func TestHandleLocalAPIAllowlist(t *testing.T) {
	w := New(&util.ConfigWorker{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	w.handleLocalAPI(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

func TestHandleLocalAPIErrorFieldIs502Not200(t *testing.T) {
	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)
	go func() {
		env := <-sess.outbox
		var req RequestMsg
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			return
		}
		sess.mu.Lock()
		ch := sess.waiters[req.ID]
		sess.mu.Unlock()
		ch <- ResponseMsg{ID: req.ID, Error: "hub disconnected"}
	}()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/librenms-config", nil)
	w.handleLocalAPI(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502 (Error must not become HTTP 200)", rec.Code)
	}
}

func TestHandleLocalAPIProxiesStatusAndBody(t *testing.T) {
	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)
	go func() {
		env := <-sess.outbox
		var req RequestMsg
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			return
		}
		sess.mu.Lock()
		ch := sess.waiters[req.ID]
		sess.mu.Unlock()
		ch <- ResponseMsg{ID: req.ID, Status: http.StatusOK, Body: json.RawMessage(`{"url":"http://librenms"}`)}
	}()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/librenms-config", nil)
	w.handleLocalAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if rec.Body.String() != `{"url":"http://librenms"}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestListenLocalAPITightensDirAndSocketMode(t *testing.T) {
	dir := t.TempDir()
	sockDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(sockDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sockDir, 0o777); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(sockDir, "api.sock")
	ln, err := listenLocalAPI(sock)
	if err != nil {
		t.Fatalf("listenLocalAPI: %v", err)
	}
	defer ln.Close()

	st, err := os.Stat(sockDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o750 {
		t.Fatalf("dir mode %o, want 0750", st.Mode().Perm())
	}
	st, err = os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode %o, want 0660", st.Mode().Perm())
	}
}

func TestStartRefusesWorldWritableDirThatCannotBeTightened(t *testing.T) {
	orig := osChmod
	osChmod = func(name string, mode os.FileMode) error {
		return nil
	}
	t.Cleanup(func() { osChmod = orig })

	dir := t.TempDir()
	sockDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(sockDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sockDir, 0o777); err != nil {
		t.Fatal(err)
	}

	w := New(&util.ConfigWorker{
		Listen: "127.0.0.1:0",
		Commands: map[string]util.ConfigWorkerCommand{
			"x": {Cmd: "/bin/true"},
		},
		APISocket: filepath.Join(sockDir, "api.sock"),
	})
	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start succeeded, want world-accessible error")
		}
		if !strings.Contains(err.Error(), "world-accessible") {
			t.Fatalf("err = %v, want world-accessible", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestStartRefusesDisabledUnixSocket(t *testing.T) {
	w := New(&util.ConfigWorker{
		Listen:    "127.0.0.1:0",
		Commands:  map[string]util.ConfigWorkerCommand{"x": {Cmd: "/bin/true"}},
		APISocket: "none",
	})
	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start succeeded with socket disabled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestDoHubRequestConcurrentWaiters(t *testing.T) {
	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			env := <-sess.outbox
			var req RequestMsg
			if err := json.Unmarshal(env.Payload, &req); err != nil {
				t.Errorf("unmarshal: %v", err)
				return
			}
			sess.mu.Lock()
			ch := sess.waiters[req.ID]
			sess.mu.Unlock()
			ch <- ResponseMsg{ID: req.ID, Status: http.StatusOK, Body: json.RawMessage(`{}`)}
		}
	}()

	var got sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		got.Add(1)
		go func() {
			defer got.Done()
			_, _, err := w.DoHubRequest(context.Background(), http.MethodGet, "/api/librenms-config", nil)
			errs <- err
		}()
	}
	got.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
