package dns

import (
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestRenderZoneInclude(t *testing.T) {
	cfg := &Config{
		ConfigDNS: util.ConfigDNS{
			CommonConfig: util.CommonConfig{DefaultDomain: "example.com"},
			DestFile:     "/etc/dnsmgr2/records",
		},
		Zones: []ConfigDNSZone{
			{Name: "example.com", Type: "forward", DnsTemplate: "default_dns"},
			{Name: "192.168.0.0/16", Type: "reverse4", DnsTemplate: "default_dns"},
		},
	}
	out, err := RenderZoneInclude(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"WARNING! do not edit, factum2-dns will overwrite",
		"name: example.com",
		"dns_template: default_dns",
		"type: reverse4",
		"name: 192.168.0.0/16",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("yaml missing %q\n%s", want, s)
		}
	}
	for _, not := range []string{
		"dbfile:",
		"host_templates:",
		"cmd_reload_all:",
		"soa_templates:",
	} {
		if strings.Contains(s, not) {
			t.Errorf("zone include should not contain %q\n%s", not, s)
		}
	}
}

func TestWriteRecordsWithZonesMergesDefaultDomain(t *testing.T) {
	devices := []*models.Device{
		{Name: "r1", PrimaryIPv4: "10.0.0.1/32"},
	}
	var buf strings.Builder
	n, err := writeRecordsWithZones(&buf, "example.com", devices, []ConfigDNSZone{
		{
			Name: "example.com",
			Records: []ConfigDNSRecord{
				{Name: "www", Type: "A", Value: "192.0.2.10"},
			},
		},
		{
			Name: "other.com",
			Records: []ConfigDNSRecord{
				{Name: "mail", Type: "A", Value: "192.0.2.20"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := parseRecordsJSON(t, []byte(buf.String()))
	if n != 3 {
		t.Errorf("wrote %d records, want 3 (got file:\n%s)", n, buf.String())
	}
	if len(got.Domains) != 2 {
		t.Fatalf("domains = %#v", got.Domains)
	}
	if got.Domains[0].Name != "example.com" || got.Domains[1].Name != "other.com" {
		t.Fatalf("domain names = %q, %q", got.Domains[0].Name, got.Domains[1].Name)
	}
	if !recordsJSONEqual(got.Domains[0].Records, []recordsJSONRecord{
		{Name: "r1", Type: "A", Value: "10.0.0.1"},
		{Name: "www", Type: "A", Value: "192.0.2.10"},
	}) {
		t.Errorf("example.com records = %#v", got.Domains[0].Records)
	}
	if !recordsJSONEqual(got.Domains[1].Records, []recordsJSONRecord{
		{Name: "mail", Type: "A", Value: "192.0.2.20"},
	}) {
		t.Errorf("other.com records = %#v", got.Domains[1].Records)
	}
}

func TestWriteRecordsWithZonesSkipsReverse(t *testing.T) {
	var buf strings.Builder
	n, err := writeRecordsWithZones(&buf, "example.com", nil, []ConfigDNSZone{
		{Name: "172.27.0.0/16", Type: "reverse4"},
		{
			Name: "example.com",
			Type: "forward",
			Records: []ConfigDNSRecord{
				{Name: "www", Type: "A", Value: "192.0.2.10"},
			},
		},
		{Name: "2001:db8::/32", Type: "reverse6"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := parseRecordsJSON(t, []byte(buf.String()))
	if n != 1 {
		t.Errorf("wrote %d records, want 1 (got file:\n%s)", n, buf.String())
	}
	if len(got.Domains) != 1 || got.Domains[0].Name != "example.com" {
		t.Fatalf("domains = %#v", got.Domains)
	}
	if !recordsJSONEqual(got.Domains[0].Records, []recordsJSONRecord{
		{Name: "www", Type: "A", Value: "192.0.2.10"},
	}) {
		t.Errorf("example.com records = %#v", got.Domains[0].Records)
	}
}

func TestWriteZoneRecordsJSONTXT(t *testing.T) {
	var buf strings.Builder
	n, err := writeRecordsWithZones(&buf, "", nil, []ConfigDNSZone{{
		Name: "example.com",
		Records: []ConfigDNSRecord{
			{Name: "abundo._domainkey", Type: "TXT", Value: "v=DKIM1; k=rsa; p=abc"},
			{Name: "@", Type: "TXT", Value: `"v=spf1 mx -all"`},
			{Name: "www", Type: "A", Value: "192.0.2.10"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := parseRecordsJSON(t, []byte(buf.String()))
	if n != 3 {
		t.Fatalf("wrote %d, want 3:\n%s", n, buf.String())
	}
	want := []recordsJSONRecord{
		{Name: "abundo._domainkey", Type: "TXT", Value: "v=DKIM1; k=rsa; p=abc"},
		{Name: "@", Type: "TXT", Value: `"v=spf1 mx -all"`},
		{Name: "www", Type: "A", Value: "192.0.2.10"},
	}
	if len(got.Domains) != 1 || !recordsJSONEqual(got.Domains[0].Records, want) {
		t.Fatalf("records = %#v", got.Domains)
	}
}

func TestRenderPrefixInclude(t *testing.T) {
	cfg := &Config{
		DhcpEnabled: true,
		DHCP: ConfigDHCP{
			DnsServers: []string{"192.0.2.53", "192.0.2.54"},
			Prefixes: []ConfigDHCPPrefix{
				{
					Name:       "192.0.2.0/24",
					Range:      "192.0.2.100-192.0.2.200",
					Gateway:    "192.0.2.1",
					DnsServers: []string{"192.0.2.53"},
				},
			},
		},
	}
	out, err := RenderPrefixInclude(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"WARNING! do not edit, factum2-dns will overwrite",
		"name: 192.0.2.0/24",
		"range: 192.0.2.100-192.0.2.200",
		"gateway: 192.0.2.1",
		"192.0.2.53",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("yaml missing %q\n%s", want, s)
		}
	}
	for _, not := range []string{
		"host_dhcp_template",
		"kea-dhcp4",
		"zones:",
		"dns_template:",
	} {
		if strings.Contains(s, not) {
			t.Errorf("prefix include should not contain %q\n%s", not, s)
		}
	}
}

func TestRenderZoneIncludeOmitsPrefixes(t *testing.T) {
	cfg := &Config{
		DhcpEnabled: true,
		Zones: []ConfigDNSZone{
			{Name: "example.com", Type: "forward", DnsTemplate: "default_dns"},
		},
		DHCP: ConfigDHCP{
			Prefixes: []ConfigDHCPPrefix{{Name: "192.0.2.0/24"}},
		},
	}
	out, err := RenderZoneInclude(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "192.0.2.0/24") || strings.Contains(s, "prefixes:") {
		t.Errorf("zone include should omit prefixes\n%s", s)
	}
}

func TestWriteZoneRecordsSkipsCommentAndDomain(t *testing.T) {
	ttl := uint(600)
	var buf strings.Builder
	n, err := writeRecordsWithZones(&buf, "", nil, []ConfigDNSZone{{
		Name: "example.com",
		Records: []ConfigDNSRecord{
			{Type: zoneRecordTypeComment, Value: "ignored"},
			{Name: "other.com", Type: zoneRecordTypeDomain},
			{Name: "www", Type: "A", Value: "192.0.2.10", TTL: &ttl},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := parseRecordsJSON(t, []byte(buf.String()))
	if n != 1 {
		t.Fatalf("wrote %d, want 1:\n%s", n, buf.String())
	}
	want := []recordsJSONRecord{
		{Name: "www", Type: "A", Value: "192.0.2.10", TTL: 600},
	}
	if len(got.Domains) != 1 || got.Domains[0].Name != "example.com" || !recordsJSONEqual(got.Domains[0].Records, want) {
		t.Fatalf("records = %#v", got.Domains)
	}
}

func TestWriteZoneRecordsMAC(t *testing.T) {
	var buf strings.Builder
	n, err := writeRecordsWithZones(&buf, "", nil, []ConfigDNSZone{{
		Name: "example.com",
		Records: []ConfigDNSRecord{
			{Name: "test", Type: "A", Value: "192.0.2.4", MAC: "aa:bb:cc:dd:ee:ff"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := parseRecordsJSON(t, []byte(buf.String()))
	if n != 1 {
		t.Fatalf("wrote %d:\n%s", n, buf.String())
	}
	want := []recordsJSONRecord{
		{Name: "test", Type: "A", Value: "192.0.2.4", MAC: "aa:bb:cc:dd:ee:ff"},
	}
	if len(got.Domains) != 1 || !recordsJSONEqual(got.Domains[0].Records, want) {
		t.Fatalf("records = %#v", got.Domains)
	}
	if strings.Contains(buf.String(), "; mac=") {
		t.Fatalf("MAC still a comment:\n%s", buf.String())
	}
}
