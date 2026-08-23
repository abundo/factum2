package factum

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func serveUnixHTTP(t *testing.T, h http.Handler) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "api.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	deadline := time.Now().Add(time.Second)
	for {
		c, err := net.DialTimeout("unix", socket, 50*time.Millisecond)
		if err == nil {
			c.Close()
			return socket
		}
		if time.Now().After(deadline) {
			t.Fatalf("unix server not ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGetDevicesWithInterfacesPreservesQueryOverUnix(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"core-sw1","interfaces":[{"name":"Ethernet1"}]}]`))
	}))
	var httpsHits atomic.Int32
	https := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpsHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(https.Close)

	client := NewFactumClient(&util.ConfigFactum{URL: https.URL, Token: "secret", Socket: sock})
	devs, err := client.GetDevicesWithInterfaces()
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/device" || gotQuery != "include=interfaces" {
		t.Fatalf("request %s?%s, want /api/device?include=interfaces", gotPath, gotQuery)
	}
	if gotAuth != "" {
		t.Fatalf("unix Authorization = %q, want empty", gotAuth)
	}
	if httpsHits.Load() != 0 {
		t.Fatalf("https hits %d, want 0", httpsHits.Load())
	}
	if len(devs) != 1 || devs[0].Name != "core-sw1" || len(devs[0].Interfaces) != 1 {
		t.Fatalf("devices = %+v", devs)
	}
}

func TestGetDevicesUnixEmptyURL(t *testing.T) {
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	devs, err := NewFactumClient(&util.ConfigFactum{Socket: sock}).GetDevices()
	if err != nil {
		t.Fatal(err)
	}
	if devs == nil {
		t.Fatal("want empty slice, not nil error")
	}
}

func TestGetDevicesHTTPSBearer(t *testing.T) {
	var gotAuth string
	https := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(https.Close)

	missing := filepath.Join(t.TempDir(), "missing.sock")
	devs, err := NewFactumClient(&util.ConfigFactum{
		URL:    https.URL,
		Token:  "secret",
		Socket: missing,
	}).GetDevices()
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", gotAuth)
	}
	if devs == nil {
		t.Fatal("want empty slice")
	}
}

func TestPendingDeletesRoundTripUnix(t *testing.T) {
	var methods []string
	var paths []string
	var putCT string
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		switch r.Method {
		case http.MethodPut:
			putCT = r.Header.Get("Content-Type")
			body, _ := io.ReadAll(r.Body)
			var row models.LibrenmsPendingDelete
			if err := json.Unmarshal(body, &row); err != nil {
				t.Errorf("unmarshal PUT: %v", err)
			}
			row.ID = 1
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(row)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	when := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	client := NewFactumClient(&util.ConfigFactum{Socket: sock})
	out, err := client.UpsertLibrenmsPendingDelete(&models.LibrenmsPendingDelete{
		DeviceID:    7,
		Hostname:    "10.1.1.1",
		Display:     "rtr1",
		Reason:      "no_match",
		ScheduledAt: when,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.DeviceID != 7 || out.Hostname != "10.1.1.1" {
		t.Fatalf("upsert = %+v", out)
	}
	if err := client.DeleteLibrenmsPendingDelete(7); err != nil {
		t.Fatal(err)
	}
	if putCT != "application/json" {
		t.Fatalf("PUT Content-Type = %q", putCT)
	}
	if len(methods) != 2 || methods[0] != http.MethodPut || methods[1] != http.MethodDelete {
		t.Fatalf("methods = %v", methods)
	}
	if len(paths) != 2 || paths[0] != "/api/librenms/pending-deletes/7" || paths[1] != "/api/librenms/pending-deletes/7" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestDoJSONUnix502DoesNotRetryHTTPS(t *testing.T) {
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"hub disconnected"}`))
	}))
	var httpsHits atomic.Int32
	https := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpsHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(https.Close)

	err := NewFactumClient(&util.ConfigFactum{
		URL:    https.URL,
		Token:  "secret",
		Socket: sock,
	}).doJSON(http.MethodGet, "/api/device", nil, nil)
	if err == nil {
		t.Fatal("want 502 error")
	}
	if httpsHits.Load() != 0 {
		t.Fatalf("https hits %d, unix 502 must not retry HTTPS", httpsHits.Load())
	}
}

func TestTriggerSyncEmptyBodySetsJSONContentType(t *testing.T) {
	var gotCT string
	var gotLen int
	sock := serveUnixHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotLen = len(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":9}`))
	}))
	id, err := NewFactumClient(&util.ConfigFactum{Socket: sock}).TriggerSync("dns")
	if err != nil {
		t.Fatal(err)
	}
	if id != 9 {
		t.Fatalf("job id %d, want 9", id)
	}
	if gotCT != "application/json" {
		t.Fatalf("Content-Type = %q, empty body must still be JSON", gotCT)
	}
	if gotLen != 0 {
		t.Fatalf("body len %d, want 0", gotLen)
	}
}
