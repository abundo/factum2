package librenms

import (
	"strings"
	"testing"
)

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
