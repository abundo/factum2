package models

import "testing"

func TestIgnoreOrDefault(t *testing.T) {
	if got := IgnoreOrDefault("", "common"); got != "common" {
		t.Fatalf("empty = %q", got)
	}
	if got := IgnoreOrDefault(" \n\t\n", "common"); got != "common" {
		t.Fatalf("blank lines = %q", got)
	}
	if got := IgnoreOrDefault("lab-sw1\n", "common"); got != "lab-sw1\n" {
		t.Fatalf("specific = %q", got)
	}
	if got := IgnoreOrDefault("lab-sw1", ""); got != "lab-sw1" {
		t.Fatalf("no fallback = %q", got)
	}
}
