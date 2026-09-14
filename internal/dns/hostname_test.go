package dns

import (
	"strings"
	"testing"
)

func TestNormalizeDNSName(t *testing.T) {
	ok := []struct{ in, want string }{
		{"", ""},
		{"  ", ""},
		{"Example.COM.", "example.com"},
		{"lo0.r1", "lo0.r1"},
		{"ns1", "ns1"},
	}
	for _, tc := range ok {
		got, err := NormalizeDNSName(tc.in)
		if err != nil {
			t.Errorf("NormalizeDNSName(%q) = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeDNSName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"foo..bar", "foo/bar", "a b", strings.Repeat("a", 64) + ".com"} {
		if _, err := NormalizeDNSName(bad); err == nil {
			t.Errorf("NormalizeDNSName(%q) = nil, want error", bad)
		}
	}
}

func TestDnsNameRelative(t *testing.T) {
	cases := []struct {
		name, zone     string
		unqualified    bool
		want           string
	}{
		{"lo0.r1.example.com", "example.com", true, "lo0.r1"},
		{"example.com", "example.com", true, "@"},
		{"lo0", "example.com", true, "lo0"},
		{"lo0", "example.com", false, ""},
		{"host.other.com", "example.com", true, ""},
		{"host.other.com", "other.com", false, "host"},
	}
	for _, tc := range cases {
		if got := dnsNameRelative(tc.name, tc.zone, tc.unqualified); got != tc.want {
			t.Errorf("dnsNameRelative(%q, %q, %v) = %q, want %q",
				tc.name, tc.zone, tc.unqualified, got, tc.want)
		}
	}
}
