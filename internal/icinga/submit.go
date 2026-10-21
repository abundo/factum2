package icinga

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

type checkResult struct {
	Host    string
	Service string
	Status  int
	Output  string
	Perf    string
}

func sanitizePluginOutput(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return s
}

func formatExternalCommand(unix int64, r checkResult) string {
	body := sanitizePluginOutput(r.Output)
	if perf := strings.TrimSpace(r.Perf); perf != "" {
		body += "|" + perf
	}
	const max = 3400
	if len(body) > max {
		body = body[:max]
	}
	return fmt.Sprintf("[%d] PROCESS_SERVICE_CHECK_RESULT;%s;%s;%d;%s\n", unix, r.Host, r.Service, r.Status, body)
}

func writeCheckResults(w io.Writer, results []checkResult, now time.Time) error {
	unix := now.Unix()
	buf := make([]byte, 0, 32*1024)
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		_, err := w.Write(buf)
		buf = buf[:0]
		return err
	}
	for _, r := range results {
		line := formatExternalCommand(unix, r)
		if len(buf)+len(line) > 32*1024 {
			if err := flush(); err != nil {
				return err
			}
		}
		buf = append(buf, line...)
	}
	return flush()
}

type pipeSubmitter struct {
	path string
	mu   sync.Mutex
}

func (p *pipeSubmitter) Submit(ctx context.Context, results []checkResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(p.path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open icinga command pipe %s: %w", p.path, err)
	}
	defer f.Close()
	if err := syscall.SetNonblock(int(f.Fd()), false); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return writeCheckResults(f, results, time.Now())
}

func icingaFilterString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func (icinga *icingaClient) SubmitServiceResult(ctx context.Context, r checkResult) error {
	if strings.TrimSpace(icinga.c.URL) == "" {
		return fmt.Errorf("icinga API URL is empty")
	}
	payload := map[string]any{
		"type":          "Service",
		"filter":        fmt.Sprintf(`host.name=="%s" && service.name=="%s"`, icingaFilterString(r.Host), icingaFilterString(r.Service)),
		"exit_status":   r.Status,
		"plugin_output": sanitizePluginOutput(r.Output),
	}
	if fields := strings.Fields(r.Perf); len(fields) > 0 {
		payload["performance_data"] = fields
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(icinga.c.URL, "/") + "/v1/actions/process-check-result"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(icinga.c.Username, icinga.c.Password)
	resp, err := icinga.http.Do(req)
	if err != nil {
		return err
	}
	// Read the whole body. A partial read stops the transport from returning
	// this connection to the idle pool, so the next result would dial again.
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("icinga process-check-result %s: %s", resp.Status, bytes.TrimSpace(respBody))
	}
	return nil
}

// apiSubmitter posts one process-check-result per service on the Icinga
// client's HTTP/1.1 connection. Poll runs one Submit per device at a time
// inside a worker, so that device's results stay on one socket. Workers do
// not share a socket with each other.
type apiSubmitter struct {
	icinga *icingaClient
}

func (a *apiSubmitter) Submit(ctx context.Context, results []checkResult) error {
	var failed int
	var first error
	for _, r := range results {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.icinga.SubmitServiceResult(ctx, r); err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	if first != nil {
		return fmt.Errorf("%d interface result(s) failed, first: %w", failed, first)
	}
	return nil
}
