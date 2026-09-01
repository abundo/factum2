package util

import "testing"

func TestFormatName(t *testing.T) {
	tests := []struct {
		domain, name, want string
	}{
		{"example.com", "rtr1", "rtr1.example.com"},
		{"example.com", "rtr1.example.com", "rtr1.example.com"},
		{"example.com", "10.1.1.1", "10.1.1.1"},
		{"", "rtr1", "rtr1"},
		{"example.com", "", ""},
	}
	for _, tt := range tests {
		if got := FormatName(tt.domain, tt.name); got != tt.want {
			t.Errorf("FormatName(%q, %q) = %q, want %q", tt.domain, tt.name, got, tt.want)
		}
	}
}
