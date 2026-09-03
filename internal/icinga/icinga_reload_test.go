package icinga

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestReloadFallsBackToAPI(t *testing.T) {
	orig := systemctlReload
	t.Cleanup(func() { systemctlReload = orig })
	systemctlReload = func() ([]byte, error) {
		return []byte("Unit icinga2.service not found"), errors.New("exit 5")
	}

	var gotPath string
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(srv.Close)

	c := NewIcingaClient(util.ConfigIcinga{
		URL:      srv.URL,
		Username: "factum",
		Password: "factum",
	})
	body, err := c.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if gotPath != "/v1/actions/restart-process" {
		t.Fatalf("path %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("auth %q", gotAuth)
	}
	if !strings.Contains(string(body), "results") {
		t.Fatalf("body %s", body)
	}
}

func TestReloadSystemctlWins(t *testing.T) {
	orig := systemctlReload
	t.Cleanup(func() { systemctlReload = orig })
	systemctlReload = func() ([]byte, error) {
		return []byte("reloaded"), nil
	}
	c := NewIcingaClient(util.ConfigIcinga{URL: "https://unused.example"})
	body, err := c.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if string(body) != "reloaded" {
		t.Fatalf("body %s", body)
	}
}
