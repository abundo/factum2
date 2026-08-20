package drivers

// Arista eAPI (the "command-api" JSON-RPC endpoint) transport, used by
// driver_arista_eos.go. It's the CLI-shaped counterpart to the NETCONF
// plumbing in openconfig.go: NETCONF has no RPC for arbitrary CLI commands
// or "show running-config" as text, and EOS's OpenConfig/NETCONF agent isn't
// enabled by default, so eAPI covers both the CLI-only operations and the
// fallback for devices where NETCONF isn't reachable.
//
// eAPI itself has to be enabled on the device ("management api
// http-commands" plus "no shutdown" under it); if it isn't, the POST below
// fails at dial time the same way NETCONF does.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// eapiPath is the URI EOS serves the JSON-RPC endpoint on.
	eapiPath = "/command-api"
	// eapiTimeout covers the whole request - "show running-config" on a
	// large chassis is the slow case, not the JSON-RPC round trip itself.
	eapiTimeout = 60 * time.Second
	// eapiFormatJSON returns each command's structured output; eapiFormatText
	// returns the same text the CLI would print, wrapped in {"output": ...}.
	// Not every command supports JSON (EOS answers 1003 "not supported" for
	// those), which is why arbitrary Exec commands go out as text.
	eapiFormatJSON = "json"
	eapiFormatText = "text"
)

// eapiClient is shared by every call - one client (and so one connection
// pool) rather than a fresh transport per request. Switches serve eAPI with
// a self-signed certificate out of the box, so verification is off; the same
// tradeoff the NETCONF/SSH side makes with ssh.InsecureIgnoreHostKey.
var eapiClient = newEAPIClient()

func newEAPIClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
		// Older EOS releases (pre-4.20ish) only speak TLS 1.0/1.1 with
		// legacy cipher suites - Go's default MinVersion (TLS 1.2) and
		// default cipher suite list (secure suites only) leave no overlap
		// with those switches, which fails the handshake outright (alert
		// 40) rather than falling back. Cert verification is already off
		// for every device above (self-signed out of the box), so widening
		// the offered suites here doesn't lower security below what's
		// already accepted.
		MinVersion:   tls.VersionTLS10,
		CipherSuites: allCipherSuiteIDs(),
	}
	return &http.Client{Transport: transport, Timeout: eapiTimeout}
}

// allCipherSuiteIDs returns every cipher suite Go knows about, secure and
// insecure alike - see newEAPIClient's comment on why.
func allCipherSuiteIDs() []uint16 {
	var ids []uint16
	for _, cs := range tls.CipherSuites() {
		ids = append(ids, cs.ID)
	}
	for _, cs := range tls.InsecureCipherSuites() {
		ids = append(ids, cs.ID)
	}
	return ids
}

type eapiParams struct {
	Version int      `json:"version"`
	Cmds    []string `json:"cmds"`
	Format  string   `json:"format"`
}

type eapiRequest struct {
	Jsonrpc string     `json:"jsonrpc"`
	Method  string     `json:"method"`
	Params  eapiParams `json:"params"`
	ID      string     `json:"id"`
}

// eapiError is the JSON-RPC error object EOS returns when a command is
// rejected. Data carries one entry per command, the failing one holding the
// CLI's own error lines ("Invalid input (at token 1: ...)"), which say a lot
// more than Message's "CLI command 1 of 1 ... failed" - see eapiError.Error.
type eapiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		Errors []string `json:"errors"`
	} `json:"data"`
}

func (e *eapiError) Error() string {
	var details []string
	for _, d := range e.Data {
		details = append(details, d.Errors...)
	}
	if len(details) > 0 {
		return fmt.Sprintf("eAPI error %d: %s: %s", e.Code, e.Message, strings.Join(details, "; "))
	}
	return fmt.Sprintf("eAPI error %d: %s", e.Code, e.Message)
}

type eapiResponse struct {
	Jsonrpc string            `json:"jsonrpc"`
	ID      string            `json:"id"`
	Result  []json.RawMessage `json:"result"`
	Error   *eapiError        `json:"error"`
}

// eapiTextResult is the shape of one result entry when format is "text".
type eapiTextResult struct {
	Output string `json:"output"`
}

// eapiRunCmds runs cmds on host over eAPI and returns one raw JSON result
// per command, in order. A command the device rejects comes back as a
// JSON-RPC error (HTTP 200 with an "error" object), not an HTTP status, so
// it's unwrapped into an *eapiError here rather than surfacing as a
// successful call with empty results.
func eapiRunCmds(username, password, host string, cmds []string, format string) ([]json.RawMessage, error) {
	if format == "" {
		format = eapiFormatJSON
	}
	body, err := json.Marshal(eapiRequest{
		Jsonrpc: "2.0",
		Method:  "runCmds",
		Params:  eapiParams{Version: 1, Cmds: cmds, Format: format},
		ID:      "1",
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, "https://"+host+eapiPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, password)

	resp, err := eapiClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eAPI %s: %s", host, resp.Status)
	}

	var parsed eapiResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, parsed.Error
	}
	if len(parsed.Result) != len(cmds) {
		return nil, fmt.Errorf("eAPI %s: got %d results for %d commands", host, len(parsed.Result), len(cmds))
	}
	return parsed.Result, nil
}

// eapiRunText runs cmds in text format and returns the last command's
// output - callers send at most one output-producing command (the earlier
// ones being mode changes like "configure"), so that's the interesting one.
func eapiRunText(username, password, host string, cmds []string) (string, error) {
	results, err := eapiRunCmds(username, password, host, cmds, eapiFormatText)
	if err != nil {
		return "", err
	}
	var out eapiTextResult
	if err := json.Unmarshal(results[len(results)-1], &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

// jsonObjectKeys returns a JSON object's keys in the order they appear in
// the encoded bytes. EOS returns per-interface data as an object keyed by
// interface name, and that order is the device's own interface order, which
// unmarshaling into a Go map would throw away.
func jsonObjectKeys(raw json.RawMessage) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("expected a JSON object")
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("expected a JSON object key")
		}
		keys = append(keys, key)
		var value json.RawMessage // read past the value to reach the next key
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
	}
	return keys, nil
}
