package drivers

// Transport-level tests for eapi.go. The driver tests in
// driver_arista_eos_test.go cover the happy paths through this code; these
// cover the failure modes a device can produce that a driver method can't
// easily reach - a non-200 status, a truncated result list, a malformed body.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEapiRunCmdsDefaultsToJSON(t *testing.T) {
	fake := newFakeEOS(t, respondJSON(raw(`{}`)))
	host := strings.TrimPrefix(fake.URL, "https://")

	if _, err := eapiRunCmds("admin", "secret", host, []string{"show version"}, ""); err != nil {
		t.Fatalf("eapiRunCmds: %v", err)
	}
	if format := fake.only(t).Params.Format; format != eapiFormatJSON {
		t.Errorf("format = %q, want %q", format, eapiFormatJSON)
	}
	if version := fake.only(t).Params.Version; version != 1 {
		t.Errorf("version = %d, want 1", version)
	}
}

// EOS reports rejected commands in the JSON-RPC body, so an HTTP error status
// is something else entirely - a wrong URL, an eAPI endpoint that's configured
// but shut, a proxy in between - and must not be parsed as a reply.
func TestEapiRunCmdsHTTPError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := eapiRunCmds("admin", "wrong", strings.TrimPrefix(server.URL, "https://"), []string{"show version"}, eapiFormatJSON)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want it to mention the HTTP status", err)
	}
}

// A caller indexes into the results by command position, so a short list has
// to be an error rather than a panic further up.
func TestEapiRunCmdsResultCountMismatch(t *testing.T) {
	fake := newFakeEOS(t, respondJSON(raw(`{}`)))
	host := strings.TrimPrefix(fake.URL, "https://")

	_, err := eapiRunCmds("admin", "secret", host, []string{"show version", "show hostname"}, eapiFormatJSON)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "got 1 results for 2 commands") {
		t.Errorf("error = %q", err)
	}
}

// eapiRunText returns the last command's output: the earlier ones are mode
// changes ("configure", "interface X") whose output is empty.
func TestEapiRunTextReturnsLastOutput(t *testing.T) {
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		results := make([]json.RawMessage, len(req.Params.Cmds))
		for i, cmd := range req.Params.Cmds {
			body, _ := json.Marshal(eapiTextResult{Output: "output of " + cmd})
			results[i] = body
		}
		return results, nil
	})

	got, err := eapiRunText("admin", "secret", strings.TrimPrefix(fake.URL, "https://"), []string{"configure", "show hostname"})
	if err != nil {
		t.Fatalf("eapiRunText: %v", err)
	}
	if got != "output of show hostname" {
		t.Errorf("output = %q", got)
	}
}

func TestJSONObjectKeys(t *testing.T) {
	// Keys come back in encoded order, not sorted order - Ethernet2 before
	// Ethernet10 - and nested objects don't leak their own keys into the list.
	raw := json.RawMessage(`{
		"Ethernet2":  {"description": "a", "nested": {"deep": 1}},
		"Ethernet10": {"description": "b"},
		"Ethernet1":  {}
	}`)
	got, err := jsonObjectKeys(raw)
	if err != nil {
		t.Fatalf("jsonObjectKeys: %v", err)
	}
	want := []string{"Ethernet2", "Ethernet10", "Ethernet1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// Only well-formed-but-wrong-shaped input is covered: a truncated object is
// not something jsonObjectKeys can be handed, since its json.RawMessage comes
// from a json.Unmarshal of the whole eAPI reply, which validates it first.
// (It wouldn't be reported anyway - json.Decoder.More returns false on a parse
// error, so a truncated object decodes as zero keys and no error.)
func TestJSONObjectKeysRejectsNonObject(t *testing.T) {
	for _, input := range []string{`[1, 2]`, `"a string"`, `123`, `null`, ``} {
		if _, err := jsonObjectKeys(json.RawMessage(input)); err == nil {
			t.Errorf("jsonObjectKeys(%q): want error, got nil", input)
		}
	}
}
