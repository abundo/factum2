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
		"dns_db_file",
		"dns_zones",
		"IF NOT EXISTS",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}

func TestMigrationsIncludeDnsDbFile(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var found string
	for _, e := range entries {
		names = append(names, e.Name())
		if strings.HasPrefix(e.Name(), "00003_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatalf("missing 00003_*.sql (v1.0.8 applied 00002 without dns_db_file; adopted DBs stay broken without a later file), have %v", names)
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"dns_db_file",
		"IF NOT EXISTS",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}

func TestMigrationsIncludeCerts(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "00010_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatal("missing 00010_*.sql for certificate tables")
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"certs_enabled",
		"cert_accounts",
		"cert_challenges",
		"certificates",
		"certificate_domains",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}

func TestMigrationsIncludeServiceDefinitions(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var found string
	for _, e := range entries {
		names = append(names, e.Name())
		if strings.HasPrefix(e.Name(), "00004_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatalf("missing 00004_*.sql, have %v", names)
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"interfaces",
		"DROP COLUMN IF EXISTS endpoint_roles",
		"service_connection_types",
		"idx_svc_ct_type_name",
		"DEFERRABLE",
		"applied_device_id",
		"connection_type_id",
		"DELETE FROM public.services",
		"maintenance_notifications",
		"cli_tree",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}

func TestMigrationsIncludeBranding(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "00021_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatal("missing 00021_*.sql for settings.brand_logo / brand_text")
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"brand_logo",
		"brand_text",
		"IF NOT EXISTS",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}

func TestMigrationsIncludeIcingaCertChecks(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "00020_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatal("missing 00020_*.sql for icinga cert checks")
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"icinga_certs_file",
		"icinga_cert_template",
		"certificates",
		"host",
		"IF NOT EXISTS",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}

func TestMigrationsIncludeDhcpPrefixesFile(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "00019_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatal("missing 00019_*.sql for settings.dhcp_prefixes_file")
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"dhcp_prefixes_file",
		"IF NOT EXISTS",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}

func TestMigrationsIncludeDnsZonesFile(t *testing.T) {
	entries, err := fs.ReadDir(migrationFS, "sql")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "00018_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatal("missing 00018_*.sql for settings.dns_zones_file")
	}
	body, err := fs.ReadFile(migrationFS, "sql/"+found)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"dns_zones_file",
		"IF NOT EXISTS",
		"-- +goose Up",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s: missing %q", found, want)
		}
	}
}
