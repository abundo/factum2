package oxidized

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestSplitNodeFull(t *testing.T) {
	g, n := SplitNodeFull("rtr1")
	if g != "" || n != "rtr1" {
		t.Fatalf("ungrouped = %q %q", g, n)
	}
	g, n = SplitNodeFull("core/rtr1")
	if g != "core" || n != "rtr1" {
		t.Fatalf("grouped = %q %q", g, n)
	}
	g, n = SplitNodeFull("a/b/rtr1")
	if g != "a/b" || n != "rtr1" {
		t.Fatalf("nested group = %q %q", g, n)
	}
}

func TestListNodesAndBrowserAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes.json", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "ox" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"name":      "rtr1",
				"full_name": "core/rtr1",
				"ip":        "10.0.0.1",
				"group":     "core",
				"model":     "eos",
				"status":    "success",
				"time":      "2026-01-02 03:04:05 +0000",
				"mtime":     "2026-01-01 00:00:00 +0000",
			},
			{
				"name":      "sw1",
				"full_name": "sw1",
				"ip":        "10.0.0.2",
				"group":     "default",
				"model":     "ios",
				"status":    "never",
				"time":      "never",
				"mtime":     nil,
			},
		})
	})
	mux.HandleFunc("/node/fetch/core/rtr1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hostname rtr1\n")
	})
	mux.HandleFunc("/node/version.json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("node_full") != "core/rtr1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"oid":     "aaa111",
				"date":    "2026-01-02 03:04:05 +0000",
				"time":    "2026-01-02 03:04:05 +0000",
				"message": "update rtr1",
				"author":  map[string]any{"name": "oxidized", "email": "ox@example"},
			},
			{
				"oid":     "bbb222",
				"date":    "2026-01-01 00:00:00 +0000",
				"message": "first",
				"author":  map[string]any{"name": "oxidized"},
			},
		})
	})
	mux.HandleFunc("/node/version/view", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("node") != "rtr1" || q.Get("group") != "core" || q.Get("oid") != "aaa111" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "version not found")
			return
		}
		_, _ = io.WriteString(w, "hostname rtr1\ninterface Eth1\n")
	})
	mux.HandleFunc("/node/version/diffs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("node") != "rtr1" || q.Get("oid") != "aaa111" || q.Get("oid2") != "bbb222" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode([]string{
			"--- a/rtr1\n",
			"+++ b/rtr1\n",
			"@@ -1,1 +1,2 @@\n",
			" hostname rtr1\n",
			"+interface Eth1\n",
			"-interface Eth2\n",
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := NewOxidizedClient(util.ConfigOxidized{
		URL:  srv.URL,
		User: "ox",
		Pass: "secret",
	})

	nodes, err := client.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes)=%d", len(nodes))
	}
	if nodes[0].FullName != "core/rtr1" || nodes[0].Status != "success" {
		t.Fatalf("node0 = %+v", nodes[0])
	}
	if nodes[1].Time != "never" || nodes[1].Mtime != "" {
		t.Fatalf("node1 times = %+v", nodes[1])
	}

	cfg, err := client.FetchConfig("core/rtr1")
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg != "hostname rtr1\n" {
		t.Fatalf("config = %q", cfg)
	}

	versions, err := client.ListVersions("core/rtr1")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].OID != "aaa111" || versions[0].Author.Name != "oxidized" {
		t.Fatalf("versions = %+v", versions)
	}

	blob, err := client.GetVersion("core/rtr1", "aaa111")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if !strings.Contains(blob, "interface Eth1") {
		t.Fatalf("blob = %q", blob)
	}

	diff, err := client.GetDiff("core/rtr1", "aaa111", "bbb222")
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if diff.Added != 1 || diff.Removed != 1 {
		t.Fatalf("diff stats = %+v", diff)
	}
	if !strings.Contains(diff.Patch, "+interface Eth1") {
		t.Fatalf("patch = %q", diff.Patch)
	}
}

func TestListNodesRequiresURL(t *testing.T) {
	client := NewOxidizedClient(util.ConfigOxidized{})
	_, err := client.ListNodes()
	if err != ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestFetchConfigMissingNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "unable to find 'gone'")
	}))
	t.Cleanup(srv.Close)
	client := NewOxidizedClient(util.ConfigOxidized{URL: srv.URL})
	_, err := client.FetchConfig("gone")
	if err == nil || !strings.Contains(err.Error(), "unable to find") {
		t.Fatalf("err = %v", err)
	}
}
