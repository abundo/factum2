package dbmigrate

import (
	"io/fs"
	"strings"
	"testing"
)

func TestMigrationsIncludeDNSZoneEditor(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var found string
	for _, e := range entries {
		names = append(names, e.Name())
		if strings.HasPrefix(e.Name(), "00002_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatalf("missing 00002_*.sql (adopted AutoMigrate DBs are stamped at version 1; dns_zones_enabled must not live only in 00001), have %v", names)
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"dns_zones_enabled",
		"dhcp_enabled",
		"dns_zones",
		"IF NOT EXISTS",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}
