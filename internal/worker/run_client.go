package worker

// run_client.go is the HTTP client half of "factum2-worker run" - the
// rabbitmq-era CLI dialed the message bus directly; post-cutover, only the
// primary holds hub connections, so the CLI instead asks the primary to run
// the command and stream results back, the same way any other remote CLI
// tool (internal/factum.FactumClient) talks to it.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/abundo/factum2/internal/util"
)

type runRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// RunRemote asks the primary (cfg.URL, authenticated with cfg.Token - same
// fields internal/factum.FactumClient uses) to run command via
// RemoteManager.RunAndWait, calling onLine for each streamed LogMsg. Returns
// the exit code from the command's final StreamExit line, or an error if
// the request failed outright or the stream ended without ever seeing one
// (e.g. the connection dropped mid-run). ctx cancellation (e.g. Ctrl-C)
// aborts the wait - it does not cancel the already-dispatched remote
// command, matching the old rabbitmq-based CLI's behavior.
func RunRemote(ctx context.Context, cfg *util.ConfigFactum, command string, args []string, onLine func(LogMsg)) (exitCode int, err error) {
	if cfg.URL == "" {
		return -1, fmt.Errorf("factum.url is not set - required for \"factum2-worker run\"")
	}

	body, err := json.Marshal(runRequest{Command: command, Args: args})
	if err != nil {
		return -1, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL+"/api/worker/run", bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return -1, fmt.Errorf("%s", errResp.Error)
		}
		return -1, fmt.Errorf("run command: %s", resp.Status)
	}

	sawExit := false
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var msg LogMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		onLine(msg)
		if msg.Stream == StreamExit {
			sawExit = true
			exitCode = msg.ExitCode
			if msg.Err != "" {
				err = fmt.Errorf("%s", msg.Err)
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return -1, scanErr
	}
	if !sawExit {
		return -1, fmt.Errorf("run command: connection ended before command finished")
	}
	return exitCode, err
}
