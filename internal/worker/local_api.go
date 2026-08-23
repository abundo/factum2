package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/abundo/factum2/internal/util"
)

// osChmod is os.Chmod, overridable in tests to simulate a directory that
// cannot be tightened.
var osChmod = os.Chmod

func prepareLocalAPIDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir unix API dir: %w", err)
	}
	// MkdirAll does not chmod an existing too-wide directory.
	if err := osChmod(dir, 0o750); err != nil {
		return fmt.Errorf("chmod unix API dir: %w", err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat unix API dir: %w", err)
	}
	if st.Mode().Perm()&0o007 != 0 {
		return fmt.Errorf("unix API dir %s is world-accessible (mode %o)", dir, st.Mode().Perm())
	}
	return nil
}

func listenLocalAPI(socketPath string) (net.Listener, error) {
	dir := filepath.Dir(socketPath)
	if err := prepareLocalAPIDir(dir); err != nil {
		return nil, err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove leftover unix socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix API: %w", err)
	}
	if err := osChmod(socketPath, 0o660); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod unix API socket: %w", err)
	}
	return ln, nil
}

func (w *Worker) runLocalAPI(ctx context.Context) error {
	socketPath := util.HubSocketPath(w.cfg.APISocket)
	if socketPath == "" {
		return fmt.Errorf("worker unix API socket disabled")
	}
	ln, err := listenLocalAPI(socketPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", w.handleLocalAPI)
	srv := &http.Server{Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("worker unix API started", "socket", socketPath)
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), hubWriteWait)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("unix API: %w", err)
		}
		return nil
	}
}

func writeJSONError(rw http.ResponseWriter, status int, msg string) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(map[string]string{"error": msg})
}

func (w *Worker) handleLocalAPI(rw http.ResponseWriter, r *http.Request) {
	cleaned, _, err := normalizeHubPath(r.URL.RequestURI())
	if err != nil {
		writeJSONError(rw, http.StatusBadRequest, "invalid path")
		return
	}
	if !AllowHubAPI(r.Method, cleaned) {
		writeJSONError(rw, http.StatusForbidden, "path not allowed over hub")
		return
	}

	// Read one extra byte so a body that hits the cap with more remaining
	// is rejected instead of silently truncated and forwarded.
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(hubMaxMessageSize)+1))
	if err != nil {
		writeJSONError(rw, http.StatusBadRequest, "read body")
		return
	}
	if len(body) > hubMaxMessageSize {
		writeJSONError(rw, http.StatusRequestEntityTooLarge, "hub request too large")
		return
	}

	start := time.Now()
	status, respBody, err := w.DoHubRequest(r.Context(), r.Method, r.URL.RequestURI(), body)
	if err != nil {
		slog.Debug("worker unix API", "method", r.Method, "path", cleaned, "err", err, "duration", time.Since(start))
		writeJSONError(rw, http.StatusBadGateway, err.Error())
		return
	}
	slog.Debug("worker unix API", "method", r.Method, "path", cleaned, "status", status, "duration", time.Since(start))
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	if len(respBody) > 0 {
		_, _ = rw.Write(respBody)
	}
}
