package ldapsrv

import "testing"

func TestLegoCertStem(t *testing.T) {
	got, err := LegoCertStem("*.ldap.example")
	if err != nil {
		t.Fatal(err)
	}
	if got != "_.ldap.example" {
		t.Fatalf("stem = %q", got)
	}
	if _, err := LegoCertStem(" "); err == nil {
		t.Fatal("empty domain was accepted")
	}
	if _, err := LegoCertStem("../x"); err == nil {
		t.Fatal("path domain was accepted")
	}
}
