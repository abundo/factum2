package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/buildinfo"
)

func TestApiVersion(t *testing.T) {
	origV, origC, origD := buildinfo.Version, buildinfo.Commit, buildinfo.Date
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit, buildinfo.Date = origV, origC, origD
	})
	buildinfo.Version = "v9.9.9"
	buildinfo.Commit = "deadbeefcafebabe"
	buildinfo.Date = "2026-08-30T00:00:00Z"

	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodGet, "/api/version", nil, nil, nil)
	if err := ctrl.ApiVersion(c); err != nil {
		t.Fatalf("ApiVersion: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got buildinfo.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != "v9.9.9" {
		t.Errorf("version = %q, want v9.9.9", got.Version)
	}
	if got.Commit != "deadbeefcafebabe" {
		t.Errorf("commit = %q, want deadbeefcafebabe", got.Commit)
	}
	if got.Date != "2026-08-30T00:00:00Z" {
		t.Errorf("date = %q, want 2026-08-30T00:00:00Z", got.Date)
	}
	if got.GoVersion == "" || !strings.HasPrefix(got.GoVersion, "go") {
		t.Errorf("go_version = %q, want a goX.Y runtime version", got.GoVersion)
	}
}
