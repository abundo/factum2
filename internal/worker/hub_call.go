package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/abundo/factum2/internal/storage"
	"github.com/abundo/factum2/internal/util"
)

// StorageRole is the worker.commands key a node activates to host
// factum2-storage. GUI file ops CallRole this name; copy uses CommandMsg
// extra args on the same command.
const StorageRole = "storage"

// CallMsg is a primary→agent HTTP-subset proxy onto the storage unix socket.
type CallMsg struct {
	ID     string            `json:"id"`
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Header map[string]string `json:"header,omitempty"`
	Body   []byte            `json:"body,omitempty"`
}

// CallResultMsg is the matching reply.
type CallResultMsg struct {
	ID     string            `json:"id"`
	Status int               `json:"status"`
	Header map[string]string `json:"header,omitempty"`
	Body   []byte            `json:"body,omitempty"`
	Error  string            `json:"error,omitempty"`
}

func (m *RemoteManager) deliverCallResult(res CallResultMsg) {
	m.mu.Lock()
	ch, ok := m.callWaiters[res.ID]
	m.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- res:
	default:
	}
}

// CallRole sends one HTTP-subset call to a single connected node whose
// hello roles include role (typically StorageRole), and waits for the
// result. The agent forwards it to its local factum2-storage unix socket.
func (m *RemoteManager) CallRole(ctx context.Context, role, method, path string, header map[string]string, body []byte) (CallResultMsg, error) {
	if m == nil {
		return CallResultMsg{}, fmt.Errorf("worker hub is not running")
	}
	id, err := newID()
	if err != nil {
		return CallResultMsg{}, err
	}
	payload, err := json.Marshal(CallMsg{ID: id, Method: method, Path: path, Header: header, Body: body})
	if err != nil {
		return CallResultMsg{}, err
	}
	env := Envelope{Type: EnvelopeCall, Payload: payload}
	frame, err := marshalHubFrame(env)
	if err != nil {
		return CallResultMsg{}, err
	}
	if len(frame) > hubMaxMessageSize {
		return CallResultMsg{}, fmt.Errorf("storage hub call too large (%d bytes)", len(frame))
	}
	env.frame = frame

	ch := make(chan CallResultMsg, 1)
	m.mu.Lock()
	var target *nodeConn
	for _, nc := range m.conns {
		if slices.Contains(nc.roles, role) {
			target = nc
			break
		}
	}
	if target == nil {
		m.mu.Unlock()
		return CallResultMsg{}, fmt.Errorf("no connected worker with role %q (add worker.commands.%s and run factum2-storage start)", role, role)
	}
	m.callWaiters[id] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.callWaiters, id)
		m.mu.Unlock()
	}()

	if !trySend(target.outbox, env) {
		return CallResultMsg{}, fmt.Errorf("worker hub outbox stuck")
	}

	timeout := util.HubRPCTimeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return CallResultMsg{}, ctx.Err()
	case <-timer.C:
		return CallResultMsg{}, fmt.Errorf("storage hub call timed out")
	case res := <-ch:
		if res.Error != "" && res.Status == 0 {
			return res, fmt.Errorf("%s", res.Error)
		}
		return res, nil
	}
}

func (w *Worker) handleCall(msg CallMsg, outbox chan<- Envelope) {
	res := CallResultMsg{ID: msg.ID}
	socket := storage.StorageSocketPath(w.storageSocket)
	if socket == "" {
		res.Error = "storage unix socket disabled"
		sendCallResult(outbox, res)
		return
	}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, "unix", socket)
		},
	}
	client := &http.Client{Transport: tr, Timeout: util.HubRPCTimeout}
	url := "http://factum2-storage" + msg.Path
	req, err := http.NewRequest(msg.Method, url, bytes.NewReader(msg.Body))
	if err != nil {
		res.Error = err.Error()
		sendCallResult(outbox, res)
		return
	}
	for k, v := range msg.Header {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		res.Status = http.StatusBadGateway
		res.Error = err.Error()
		sendCallResult(outbox, res)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(hubMaxMessageSize)+1))
	if err != nil {
		res.Status = http.StatusBadGateway
		res.Error = err.Error()
		sendCallResult(outbox, res)
		return
	}
	if len(body) > hubMaxMessageSize {
		res.Status = http.StatusRequestEntityTooLarge
		res.Error = "storage response too large"
		sendCallResult(outbox, res)
		return
	}
	res.Status = resp.StatusCode
	res.Body = body
	res.Header = map[string]string{}
	for _, k := range []string{"Content-Type", "X-Storage-Size", "X-Storage-Name"} {
		if v := resp.Header.Get(k); v != "" {
			res.Header[k] = v
		}
	}
	sendCallResult(outbox, res)
}

func sendCallResult(outbox chan<- Envelope, res CallResultMsg) {
	payload, err := json.Marshal(res)
	if err != nil {
		slog.Error("worker hub: marshal call result", "err", err)
		return
	}
	if !trySend(outbox, Envelope{Type: EnvelopeCallResult, Payload: payload}) {
		slog.Warn("worker hub: outbox stuck, dropping call result", "id", res.ID)
	}
}
