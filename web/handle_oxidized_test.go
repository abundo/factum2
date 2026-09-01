package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestApiOxidizedNodesNotConfigured(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/oxidized/nodes", nil, nil, nil)
	if err := ctrl.ApiOxidizedNodes(c); err != nil {
		t.Fatalf("nodes: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiOxidizedBrowser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"name":      "rtr1",
				"full_name": "rtr1",
				"ip":        "10.1.1.1",
				"group":     "default",
				"model":     "eos",
				"status":    "success",
				"time":      "2026-01-02 03:04:05 +0000",
				"mtime":     "2026-01-01 00:00:00 +0000",
			},
		})
	})
	mux.HandleFunc("/node/fetch/rtr1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hostname rtr1\n")
	})
	mux.HandleFunc("/node/version.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"oid": "aaa", "date": "2026-01-02", "message": "latest", "author": map[string]any{"name": "ox"}},
			{"oid": "bbb", "date": "2026-01-01", "message": "first"},
		})
	})
	mux.HandleFunc("/node/version/view", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hostname rtr1\n")
	})
	mux.HandleFunc("/node/version/diffs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]string{"--- a\n", "+++ b\n", "+foo\n", "-bar\n"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	db := newTestDB(t)
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.OxidizedApiURL = srv.URL
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/oxidized/nodes", nil, nil, nil)
	if err := ctrl.ApiOxidizedNodes(c); err != nil {
		t.Fatalf("nodes: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	var nodes []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0]["name"] != "rtr1" {
		t.Fatalf("nodes = %#v", nodes)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/oxidized/node/config?node_full=rtr1", nil, nil, nil)
	if err := ctrl.ApiOxidizedNodeConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["config"] != "hostname rtr1\n" {
		t.Fatalf("config = %#v", cfg)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/oxidized/node/versions?node_full=rtr1", nil, nil, nil)
	if err := ctrl.ApiOxidizedNodeVersions(c); err != nil {
		t.Fatalf("versions: %v", err)
	}
	var versions []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0]["oid"] != "aaa" {
		t.Fatalf("versions = %#v", versions)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/oxidized/node/version?node_full=rtr1&oid=aaa", nil, nil, nil)
	if err := ctrl.ApiOxidizedNodeVersion(c); err != nil {
		t.Fatalf("version: %v", err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/oxidized/node/diff?node_full=rtr1&oid=aaa&oid2=bbb", nil, nil, nil)
	if err := ctrl.ApiOxidizedNodeDiff(c); err != nil {
		t.Fatalf("diff: %v", err)
	}
	var diff map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
		t.Fatal(err)
	}
	patch, _ := diff["patch"].(string)
	if !strings.Contains(patch, "+foo") {
		t.Fatalf("diff = %#v", diff)
	}
}

func TestApiOxidizedConfig(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.DefaultDomain = "example.com"
	settings.OxidizedApiURL = "http://oxidized.example.com"
	settings.OxidizedDestFile = "/etc/oxidized/router.db"
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/oxidized-config", nil, nil, nil)
	if err := ctrl.ApiOxidizedConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	var body OxidizedConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.URL != settings.OxidizedApiURL {
		t.Fatalf("url = %q", body.URL)
	}
	if body.DestFile != settings.OxidizedDestFile {
		t.Fatalf("dest_file = %q", body.DestFile)
	}
	if body.DefaultDomain != "example.com" {
		t.Fatalf("default_domain = %q", body.DefaultDomain)
	}
}

func TestApiOxidizedNodeConfigMissingParam(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/oxidized/node/config", nil, nil, nil)
	if err := ctrl.ApiOxidizedNodeConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}
