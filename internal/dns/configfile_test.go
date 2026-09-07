package dns

import (
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestRenderDnsmgrConfig(t *testing.T) {
	cfg := &Config{
		ConfigDNS: util.ConfigDNS{
			CommonConfig: util.CommonConfig{DefaultDomain: "example.com"},
			DestFile:     "/etc/dnsmgr2/records",
		},
		DbFile: "/var/lib/dnsmgr2/dnsmgr2.sqlite",
		Host:   ConfigDNSHost{Name: "isc_bind_ubuntu"},
		SOATemplates: []ConfigDNSSOA{
			{Name: "default_soa", Mname: "ns1.example.com.", Rname: "hostmaster.example.com.", Refresh: 36000, Retry: 3600, Expire: 604800, Minimum: 900},
		},
		Templates: []ConfigDNSTemplate{
			{Name: "default_dns", SOA: "default_soa", DefaultTTL: "900", DNSSECPolicy: "dnssec-policy", NS: []string{"ns1.example.com.", "ns2.example.com."}},
		},
		Zones: []ConfigDNSZone{
			{Name: "example.com", Type: "forward", DnsTemplate: "default_dns"},
			{Name: "192.168.0.0/16", Type: "reverse4", DnsTemplate: "default_dns"},
		},
	}
	out, err := RenderDnsmgrConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"default_domain: example.com",
		"dbfile: /var/lib/dnsmgr2/dnsmgr2.sqlite",
		"name: /etc/dnsmgr2/records",
		"default_soa:",
		"default_dns:",
		"dns_template: default_dns",
		"type: reverse4",
		"dnssec_policy: dnssec-policy",
		"ns1.example.com.",
		"cmd_reload_all: sudo rndc reload",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("yaml missing %q\n%s", want, s)
		}
	}
}

func TestWriteRecordsWithZonesMergesDefaultDomain(t *testing.T) {
	devices := []*models.Device{
		{Name: "r1", PrimaryIPv4: "10.0.0.1/32"},
	}
	var buf strings.Builder
	n := writeRecordsWithZones(&buf, "example.com", devices, []ConfigDNSZone{
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
	got := buf.String()
	if n != 3 {
		t.Errorf("wrote %d records, want 3 (got file:\n%s)", n, got)
	}
	if strings.Count(got, "$DOMAIN example.com") != 1 {
		t.Fatalf("default domain written twice:\n%s", got)
	}
	if !strings.Contains(got, "www") || !strings.Contains(got, "r1") || !strings.Contains(got, "$DOMAIN other.com") {
		t.Fatalf("expected device + zone records:\n%s", got)
	}
}
