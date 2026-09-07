package librenms

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestLibrenmsAPIURL(t *testing.T) {
	tests := []struct{ base, endpoint, want string }{
		{"http://librenms:8000", "/devices", "http://librenms:8000/api/v0/devices"},
		{"http://librenms:8000/", "/devices", "http://librenms:8000/api/v0/devices"},
		{"http://librenms:8000/api/v0", "/devices", "http://librenms:8000/api/v0/devices"},
		{"http://librenms:8000/api/v0/", "devices", "http://librenms:8000/api/v0/devices"},
		{" http://librenms:8000/API/v0 ", "/devices", "http://librenms:8000/API/v0/devices"},
	}
	for _, tt := range tests {
		if got := librenmsAPIURL(tt.base, tt.endpoint); got != tt.want {
			t.Errorf("librenmsAPIURL(%q, %q) = %q, want %q", tt.base, tt.endpoint, got, tt.want)
		}
	}
}

func TestCallAPIPostsJSONToAPIv0(t *testing.T) {
	var gotPath, gotContentType, gotAccept, gotToken string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		gotToken = r.Header.Get("X-Auth-Token")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","devices":[{"device_id":9}]}`))
	}))
	t.Cleanup(srv.Close)

	client := NewLibrenmsClient(&util.ConfigLibrenms{URL: srv.URL, Key: "tok"})
	id, err := client.DeviceCreate("10.1.1.1", "rtr1", true, "v2c", "public")
	if err != nil {
		t.Fatal(err)
	}
	if id != 9 {
		t.Fatalf("device_id=%d, want 9", id)
	}
	if gotPath != "/api/v0/devices" {
		t.Fatalf("path=%q, want /api/v0/devices", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type=%q", gotContentType)
	}
	if gotAccept != "application/json" || gotToken != "tok" {
		t.Fatalf("Accept=%q token=%q", gotAccept, gotToken)
	}
	var payload map[string]string
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["hostname"] != "10.1.1.1" || payload["community"] != "public" || payload["force_add"] != "1" {
		t.Fatalf("body=%v", payload)
	}
}

func TestCallAPIHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"CSRF token mismatch."}`))
	}))
	t.Cleanup(srv.Close)

	client := NewLibrenmsClient(&util.ConfigLibrenms{URL: srv.URL, Key: "tok"})
	_, err := client.DeviceCreate("10.1.1.1", "rtr1", false, "v2c", "public")
	if err == nil || !strings.Contains(err.Error(), "CSRF token mismatch") {
		t.Fatalf("err=%v", err)
	}
}

func TestHostnameIsIP(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"255.255.255.255", true},
		{"2001:db8::1", true},
		{"::1", true},
		{"fe80::1", true},
		{"router1.example.com", false},
		{"localhost", false},
		{"", false},
		{"10.0.0.1/24", false},
		{" 10.0.0.1", false},
		{"[2001:db8::1]", false},
	}
	for _, tc := range cases {
		if got := hostnameIsIP(tc.in); got != tc.want {
			t.Errorf("hostnameIsIP(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRequireIPHostnames(t *testing.T) {
	t.Run("all IPs", func(t *testing.T) {
		rep := &recReporter{}
		devices := []*LibrenmsDevice{
			{DeviceID: 1, Hostname: "10.0.0.1"},
			{DeviceID: 2, Hostname: "2001:db8::1"},
			nil,
		}
		if err := requireIPHostnames(devices, rep); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rep.msgs) != 0 {
			t.Fatalf("unexpected log: %v", rep.msgs)
		}
	})

	t.Run("non-IP hostnames abort", func(t *testing.T) {
		rep := &recReporter{}
		devices := []*LibrenmsDevice{
			{DeviceID: 1, Hostname: "10.0.0.1"},
			{DeviceID: 3, Hostname: "rtr1.example.com"},
			{DeviceID: 4, Hostname: "localhost"},
		}
		err := requireIPHostnames(devices, rep)
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "sync aborted: 2 LibreNMS device(s)") {
			t.Fatalf("error = %q, want count 2", msg)
		}
		if !strings.Contains(msg, "rtr1.example.com (id=3)") {
			t.Fatalf("error = %q, want rtr1.example.com", msg)
		}
		if !strings.Contains(msg, "localhost (id=4)") {
			t.Fatalf("error = %q, want localhost", msg)
		}
		if !strings.Contains(msg, "normalize-hostnames") {
			t.Fatalf("error = %q, want hint to run normalize-hostnames", msg)
		}
		if len(rep.msgs) < 3 {
			t.Fatalf("expected per-device errors plus summary, got %v", rep.msgs)
		}
	})
}
