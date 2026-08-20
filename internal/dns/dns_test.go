package dns

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestDnsInterfaceLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Ethernet1/1", "ethernet1-1"},
		{"Ethernet1/1.100", "ethernet1-1-100"},
		{"loopback0", "loopback0"},
		{"1/1/1:3704.*", "1-1-1-3704"},
		{"GigabitEthernet0/0/0/0", "gigabitethernet0-0-0-0"},
		{"---", ""},
		{"", ""},
		{"eth 0", "eth-0"},
	}
	for _, tc := range cases {
		if got := dnsInterfaceLabel(tc.in); got != tc.want {
			t.Errorf("dnsInterfaceLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDnsDeviceName(t *testing.T) {
	cases := []struct {
		name, domain, want string
	}{
		{"r1", "example.com", "r1"},
		{"R1", "example.com", "r1"},
		{"r1.example.com", "example.com", "r1"},
		{"r1.example.com.", "example.com", "r1"},
		{"My Device", "example.com", "my-device"},
		{"r1_core", "example.com", "r1-core"},
		{"r1.site", "example.com", "r1.site"},
		{"---", "example.com", ""},
		{"", "example.com", ""},
	}
	for _, tc := range cases {
		if got := dnsDeviceName(tc.name, tc.domain); got != tc.want {
			t.Errorf("dnsDeviceName(%q, %q) = %q, want %q", tc.name, tc.domain, got, tc.want)
		}
	}
}

func TestParseRecord(t *testing.T) {
	cases := []struct {
		in, ip, rrtype string
		ok             bool
	}{
		{"10.0.0.1/24", "10.0.0.1", "A", true},
		{"10.0.0.1", "10.0.0.1", "A", true},
		{"2001:db8::1/64", "2001:db8::1", "AAAA", true},
		{"2001:db8::1", "2001:db8::1", "AAAA", true},
		{"", "", "", false},
		{"not-an-ip", "", "", false},
		{"::ffff:10.0.0.1", "10.0.0.1", "A", true},
	}
	for _, tc := range cases {
		ip, rrtype, ok := parseRecord(tc.in)
		if ok != tc.ok || ip != tc.ip || rrtype != tc.rrtype {
			t.Errorf("parseRecord(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, ip, rrtype, ok, tc.ip, tc.rrtype, tc.ok)
		}
	}
}

func TestWriteRecords(t *testing.T) {
	devices := []*models.Device{
		{
			Name:        "r1",
			PrimaryIPv4: "10.0.0.1/32",
			PrimaryIPv6: "2001:db8::1/128",
			Interfaces: []models.Interface{
				{
					Name: "Ethernet1/1",
					Addresses: []models.Address{
						{Address: "10.1.1.1/24"},
						{Address: "2001:db8:1::1/64"},
					},
				},
				{
					Name: "loopback0",
					Addresses: []models.Address{
						{Address: "10.0.0.1/32"},
					},
				},
				{
					Name: "empty",
				},
			},
		},
		{
			Name: "no-ip",
		},
	}

	var buf bytes.Buffer
	n := writeRecords(&buf, "example.com", devices)
	got := buf.String()

	wantLines := []string{
		"$DOMAIN example.com",
		"r1                                        A         10.0.0.1",
		"r1                                        AAAA      2001:db8::1",
		"ethernet1-1.r1                            A         10.1.1.1",
		"ethernet1-1.r1                            AAAA      2001:db8:1::1",
		"loopback0.r1                              A         10.0.0.1",
	}
	if n != 5 {
		t.Errorf("wrote %d records, want 5", n)
	}
	gotLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("got %d lines, want %d:\n%s", len(gotLines), len(wantLines), got)
	}
	for i, want := range wantLines {
		if gotLines[i] != want {
			t.Errorf("line %d:\n  got  %q\n  want %q", i, gotLines[i], want)
		}
	}
}

func TestWriteRecordsSanitizesDeviceName(t *testing.T) {
	devices := []*models.Device{
		{
			Name:        "Core_SW 1.example.com",
			PrimaryIPv4: "10.0.0.1/32",
			Interfaces: []models.Interface{
				{
					Name:      "Ethernet1/1",
					Addresses: []models.Address{{Address: "10.1.1.1/24"}},
				},
			},
		},
		{
			Name:        "---",
			PrimaryIPv4: "10.9.9.9/32",
		},
	}

	var buf bytes.Buffer
	n := writeRecords(&buf, "example.com", devices)
	got := buf.String()
	wantLines := []string{
		"$DOMAIN example.com",
		"core-sw-1                                 A         10.0.0.1",
		"ethernet1-1.core-sw-1                     A         10.1.1.1",
	}
	if n != 2 {
		t.Errorf("wrote %d records, want 2", n)
	}
	gotLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("got %d lines, want %d:\n%s", len(gotLines), len(wantLines), got)
	}
	for i, want := range wantLines {
		if gotLines[i] != want {
			t.Errorf("line %d:\n  got  %q\n  want %q", i, gotLines[i], want)
		}
	}
}

func TestFilterDevices(t *testing.T) {
	client := &DNSClient{
		DNS: &util.ConfigDNS{
			CommonConfig:    util.CommonConfig{DefaultDomain: "example.com"},
			IgnoreModels:    "ignored-model\n",
			IgnorePlatforms: "ignored-platform",
		},
	}
	all := []*models.Device{
		{Name: "ok", Enabled: true, PrimaryIPv4: "10.0.0.1/32"},
		{Name: "off", Enabled: false, PrimaryIPv4: "10.0.0.2/32"},
		{Name: "by-model", Enabled: true, ModelName: "ignored-model", PrimaryIPv4: "10.0.0.3/32"},
		{Name: "by-platform", Enabled: true, Platform: "ignored-platform", PrimaryIPv4: "10.0.0.4/32"},
		{Name: "---", Enabled: true, PrimaryIPv4: "10.0.0.5/32"},
	}
	got, counts := client.filterDevices(all)
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("got %d devices, want [ok]", len(got))
	}
	if counts.notEnabled != 1 || counts.ignoreModel != 1 || counts.ignorePlatform != 1 || counts.emptyName != 1 {
		t.Errorf("counts = %+v", counts)
	}
}

func TestValidate(t *testing.T) {
	client := &DNSClient{}
	if err := client.validate(); err == nil {
		t.Fatal("expected error for missing config")
	}
	client.DNS = &util.ConfigDNS{}
	if err := client.validate(); err == nil || !strings.Contains(err.Error(), "default_domain") {
		t.Fatalf("expected default_domain error, got %v", err)
	}
	client.DNS.DefaultDomain = "example.com"
	if err := client.validate(); err == nil || !strings.Contains(err.Error(), "dest_file") {
		t.Fatalf("expected dest_file error, got %v", err)
	}
	client.DNS.DestFile = "/tmp/records"
	if err := client.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncDevicesWritesAndUpdates(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "records")
	updated := 0
	client := &DNSClient{
		DNS: &util.ConfigDNS{
			CommonConfig: util.CommonConfig{DefaultDomain: "example.com"},
			DestFile:     dest,
		},
		update: func() error {
			updated++
			return nil
		},
	}
	devices := []*models.Device{
		{Name: "r1", Enabled: true, PrimaryIPv4: "10.0.0.1/32"},
		{Name: "off", Enabled: false, PrimaryIPv4: "10.0.0.2/32"},
	}
	reporter := jobevent.NewConsoleReporter(os.Stdout)
	if err := client.syncDevices(reporter, devices); err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("update called %d times, want 1", updated)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	want := "$DOMAIN example.com\nr1                                        A         10.0.0.1\n"
	if string(got) != want {
		t.Errorf("records file:\n  got  %q\n  want %q", got, want)
	}

	// Unchanged content still retries dnsmgr2 update.
	if err := client.syncDevices(reporter, devices); err != nil {
		t.Fatal(err)
	}
	if updated != 2 {
		t.Fatalf("update called %d times after second sync, want 2", updated)
	}
}

func TestSyncDevicesUpdateError(t *testing.T) {
	dir := t.TempDir()
	client := &DNSClient{
		DNS: &util.ConfigDNS{
			CommonConfig: util.CommonConfig{DefaultDomain: "example.com"},
			DestFile:     filepath.Join(dir, "records"),
		},
		update: func() error { return errors.New("dnsmgr2 failed") },
	}
	devices := []*models.Device{
		{Name: "r1", Enabled: true, PrimaryIPv4: "10.0.0.1/32"},
	}
	err := client.syncDevices(jobevent.NewConsoleReporter(os.Stdout), devices)
	if err == nil || err.Error() != "dnsmgr2 failed" {
		t.Fatalf("got %v, want dnsmgr2 failed", err)
	}
}
