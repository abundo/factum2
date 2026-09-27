package netboxtool

import "testing"

func TestFormatSourceRef(t *testing.T) {
	if got := FormatSourceRef("factum", "42"); got != "factum:42" {
		t.Fatalf("FormatSourceRef = %q", got)
	}
	if got := FormatSourceRef("becs", "17"); got != "becs:17" {
		t.Fatalf("FormatSourceRef = %q", got)
	}
}

func TestParseSourceRef(t *testing.T) {
	tests := []struct {
		in, system, id string
	}{
		{"factum:42", "factum", "42"},
		{"becs:17", "becs", "17"},
		{"becs:oid:9", "becs", "oid:9"},
		{" factum : 42 ", "factum", "42"},
		{"factum", "factum", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		system, id := ParseSourceRef(tt.in)
		if system != tt.system || id != tt.id {
			t.Errorf("ParseSourceRef(%q) = %q, %q; want %q, %q", tt.in, system, id, tt.system, tt.id)
		}
	}
}
