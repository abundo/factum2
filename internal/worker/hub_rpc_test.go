package worker

import (
	"bytes"
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
	"github.com/gorilla/websocket"
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
	denied := []struct{ method, path string }{
		{http.MethodGet, "/api/admin/settings"},
		{http.MethodGet, "/api/device/1"},
		{http.MethodGet, "/api/device/1/impact"},
		{http.MethodGet, "/api/jobs/1/tasks/2/events"},
		{http.MethodPost, "/api/device/1/interfaces/refresh"},
		{http.MethodPost, "/api/worker/run"},
		{http.MethodPost, "/api/librenms/pending-deletes/7/delete-next-sync"},
	}
	for i, tc := range denied {
		outbox := make(chan Envelope, 1)
		m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
			ID: fmt.Sprintf("%d", i), Method: tc.method, Path: tc.path,
		})
		resp := decodeResponse(t, <-outbox)
		if resp.Status != http.StatusForbidden {
			t.Errorf("%s %s status %d, want 403", tc.method, tc.path, resp.Status)
		}
	}
	if hits != 0 {
		t.Fatalf("handler hits = %d, allowlist must reject before ServeHTTP", hits)
	}
}

func TestHandleHubRequestPreservesIncludeQuery(t *testing.T) {
	m := NewRemoteManager(nil)
	var gotPath, gotRawQuery, gotInclude string
	m.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		gotInclude = r.URL.Query().Get("include")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	outbox := make(chan Envelope, 1)
	m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
		ID: "1", Method: http.MethodGet, Path: "/api/device?include=interfaces",
	})
	resp := decodeResponse(t, <-outbox)
	if resp.Status != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.Status)
	}
	if gotPath != "/api/device" {
		t.Fatalf("path %q, want /api/device", gotPath)
	}
	if gotRawQuery != "include=interfaces" || gotInclude != "interfaces" {
		t.Fatalf("query raw=%q include=%q, want include=interfaces", gotRawQuery, gotInclude)
	}
}

func TestHandleHubRequestOversizeYields413WithoutTearingConn(t *testing.T) {
	orig := hubMaxMessageSize
	hubMaxMessageSize = 256
	t.Cleanup(func() { hubMaxMessageSize = orig })

	m := NewRemoteManager(nil)
	big, err := json.Marshal(strings.Repeat("x", 2000))
	if err != nil {
		t.Fatal(err)
	}
	m.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(big)
	}))
	outbox := make(chan Envelope, 2)
	m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
		ID: "big", Method: http.MethodGet, Path: "/api/device",
	})
	env := <-outbox
	if len(env.frame) == 0 {
		t.Fatal("missing pre-marshaled frame")
	}
	if len(env.frame) > hubMaxMessageSize {
		t.Fatalf("413 frame %d exceeds cap %d", len(env.frame), hubMaxMessageSize)
	}
	resp := decodeResponse(t, env)
	if resp.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", resp.Status)
	}
	if string(resp.Body) != `{"error":"hub response too large"}` {
		t.Fatalf("body %s", resp.Body)
	}
	if bytes.Contains(resp.Body, bytes.Repeat([]byte("x"), 100)) {
		t.Fatal("must not send the oversized handler body")
	}
	if err := readWSWithLimit(t, int64(hubMaxMessageSize), env.frame); err != nil {
		t.Fatalf("413 frame tripped ReadLimit (would tear the conn): %v", err)
	}

	// Same connection/outbox still accepts a later RPC.
	m.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	m.HandleHubRequest(context.Background(), "n1", outbox, RequestMsg{
		ID: "small", Method: http.MethodGet, Path: "/api/device",
	})
	small := decodeResponse(t, <-outbox)
	if small.Status != http.StatusOK {
		t.Fatalf("follow-up status %d, want 200 (conn still usable)", small.Status)
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

	certFile, keyFile := writeTestHubTLS(t)
	w := New(&util.ConfigWorker{
		Listen:  "127.0.0.1:0",
		TLSCert: certFile,
		TLSKey:  keyFile,
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
	certFile, keyFile := writeTestHubTLS(t)
	w := New(&util.ConfigWorker{
		Listen:    "127.0.0.1:0",
		TLSCert:   certFile,
		TLSKey:    keyFile,
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

func TestMarshalHubFrameIncludesTrailingNewline(t *testing.T) {
	env := Envelope{Type: EnvelopeHello, Payload: json.RawMessage(`{"hostname":"x"}`)}
	frame, err := marshalHubFrame(env)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != len(raw)+1 || frame[len(frame)-1] != '\n' || !bytes.Equal(frame[:len(raw)], raw) {
		t.Fatalf("frame %q vs marshal %q", frame, raw)
	}
}

func TestSendHubResponseTooLargeYields413(t *testing.T) {
	orig := hubMaxMessageSize
	hubMaxMessageSize = 128
	t.Cleanup(func() { hubMaxMessageSize = orig })

	outbox := make(chan Envelope, 1)
	body, err := json.Marshal(strings.Repeat("x", 200))
	if err != nil {
		t.Fatal(err)
	}
	sendHubResponse(&nodeConn{outbox: outbox}, ResponseMsg{ID: "1", Status: http.StatusOK, Body: body})
	env := <-outbox
	if len(env.frame) == 0 {
		t.Fatal("missing pre-marshaled frame")
	}
	if len(env.frame) > hubMaxMessageSize {
		t.Fatalf("413 frame %d exceeds cap %d", len(env.frame), hubMaxMessageSize)
	}
	resp := decodeResponse(t, env)
	if resp.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", resp.Status)
	}
	if string(resp.Body) != `{"error":"hub response too large"}` {
		t.Fatalf("body %s", resp.Body)
	}
}

func TestHubFrameAtCapDoesNotTripReadLimit(t *testing.T) {
	orig := hubMaxMessageSize
	hubMaxMessageSize = 256
	t.Cleanup(func() { hubMaxMessageSize = orig })

	var last Envelope
	for n := 1; n < 300; n++ {
		body, err := json.Marshal(strings.Repeat("x", n))
		if err != nil {
			t.Fatal(err)
		}
		env, err := marshalResponseEnvelope(ResponseMsg{ID: "1", Status: http.StatusOK, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		var resp ResponseMsg
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Status == http.StatusRequestEntityTooLarge {
			break
		}
		last = env
	}
	if len(last.frame) == 0 {
		t.Fatal("did not produce an under-cap frame")
	}
	if len(last.frame) > hubMaxMessageSize {
		t.Fatalf("under-cap frame %d exceeds %d", len(last.frame), hubMaxMessageSize)
	}

	if err := readWSWithLimit(t, int64(hubMaxMessageSize), last.frame); err != nil {
		t.Fatalf("max-size frame tripped ReadLimit: %v", err)
	}
	if err := readWSWithLimit(t, int64(len(last.frame)), last.frame); err != nil {
		t.Fatalf("exact frame size tripped ReadLimit: %v", err)
	}
	err := readWSWithLimit(t, int64(len(last.frame)-1), last.frame)
	if !errors.Is(err, websocket.ErrReadLimit) {
		t.Fatalf("limit-1: got %v, want ErrReadLimit", err)
	}

	// The 413 substitute itself must fit and be readable at the cap.
	big, err := json.Marshal(strings.Repeat("x", 400))
	if err != nil {
		t.Fatal(err)
	}
	over, err := marshalResponseEnvelope(ResponseMsg{ID: "1", Status: http.StatusOK, Body: big})
	if err != nil {
		t.Fatal(err)
	}
	var overResp ResponseMsg
	if err := json.Unmarshal(over.Payload, &overResp); err != nil {
		t.Fatal(err)
	}
	if overResp.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", overResp.Status)
	}
	if err := readWSWithLimit(t, int64(hubMaxMessageSize), over.frame); err != nil {
		t.Fatalf("413 frame tripped ReadLimit: %v", err)
	}
}

func readWSWithLimit(t *testing.T, limit int64, payload []byte) error {
	t.Helper()
	errc := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := hubUpgrader.Upgrade(w, r, nil)
		if err != nil {
			errc <- err
			return
		}
		defer c.Close()
		c.SetReadLimit(limit)
		_, _, err = c.ReadMessage()
		errc <- err
	}))
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case err := <-errc:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for read")
		return nil
	}
}

func TestDoHubRequestFailsFastIfSessionReplaced(t *testing.T) {
	w := New(&util.ConfigWorker{})
	sess1 := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(time.Millisecond)
		sess2 := &hubSession{outbox: make(chan Envelope, 8), waiters: make(map[string]chan ResponseMsg)}
		w.setHubSession(sess2)
		w.clearHubSession(sess2)
	}()
	_, _, err := w.DoHubRequest(ctx, http.MethodGet, "/api/librenms-config", nil)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timed out: waiter registered on a displaced session")
	}
	if err == nil {
		t.Fatal("expected disconnect or replaced error")
	}
}

func TestHandleLocalAPIRejectsOversizeBody(t *testing.T) {
	orig := hubMaxMessageSize
	hubMaxMessageSize = 64
	t.Cleanup(func() { hubMaxMessageSize = orig })

	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 1), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/librenms-config", bytes.NewReader(bytes.Repeat([]byte("x"), hubMaxMessageSize+1)))
	w.handleLocalAPI(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rec.Code)
	}
	select {
	case <-sess.outbox:
		t.Fatal("must not trySend a truncated body")
	default:
	}
}

func TestHandleLocalAPIEnvelopeOversizeIs413(t *testing.T) {
	orig := hubMaxMessageSize
	hubMaxMessageSize = 64
	t.Cleanup(func() { hubMaxMessageSize = orig })

	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 1), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/librenms-config", nil)
	w.handleLocalAPI(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rec.Code)
	}
	select {
	case <-sess.outbox:
		t.Fatal("must not trySend an oversized request envelope")
	default:
	}
}

func TestDoHubRequestRejectsOversizeEnvelope(t *testing.T) {
	orig := hubMaxMessageSize
	hubMaxMessageSize = 64
	t.Cleanup(func() { hubMaxMessageSize = orig })

	w := New(&util.ConfigWorker{})
	sess := &hubSession{outbox: make(chan Envelope, 1), waiters: make(map[string]chan ResponseMsg)}
	w.setHubSession(sess)

	reqBody, err := json.Marshal(strings.Repeat("x", 80))
	if err != nil {
		t.Fatal(err)
	}
	status, body, err := w.DoHubRequest(context.Background(), http.MethodGet, "/api/librenms-config", reqBody)
	if err != nil {
		t.Fatalf("oversize is HTTP 413, not a transport error: %v", err)
	}
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", status)
	}
	if !bytes.Contains(body, []byte("hub request too large")) {
		t.Fatalf("body %s", body)
	}
	select {
	case <-sess.outbox:
		t.Fatal("must not trySend an oversized envelope")
	default:
	}
}
