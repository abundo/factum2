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

func TestBecsOID(t *testing.T) {
	if got := BecsOID(map[string]any{"source": "becs:3641645", "becs_oid": float64(1)}); got != 3641645 {
		t.Fatalf("source wins: %d", got)
	}
	if got := BecsOID(map[string]any{"becs_oid": float64(999)}); got != 999 {
		t.Fatalf("legacy: %d", got)
	}
	if got := BecsOID(map[string]any{"becs_oid": "34"}); got != 34 {
		t.Fatalf("legacy string: %d", got)
	}
	if got := BecsOID(map[string]any{"source": "factum:12", "becs_oid": float64(9)}); got != 9 {
		t.Fatalf("other source falls back to legacy: %d", got)
	}
	if got := BecsOID(map[string]any{"source": "becs:nope"}); got != 0 {
		t.Fatalf("bad source: %d", got)
	}
	if got := BecsOID(nil); got != 0 {
		t.Fatalf("nil: %d", got)
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
