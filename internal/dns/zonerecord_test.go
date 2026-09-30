package dns

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestParseZoneRecords(t *testing.T) {
	ttl := uint(300)
	recs, err := ParseZoneRecords([]models.DnsZoneRecordDTO{
		{Name: "www", Type: "A", Value: "192.0.2.1", TTL: &ttl, MAC: "AA-BB-CC-DD-EE-FF"},
		{Name: ";", Type: "COMMENT", Value: "note"},
		{Name: "sub.example.com", Type: "$DOMAIN"},
		{Name: "@", Type: "MX", Value: "10 mail.example.com."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("len = %d", len(recs))
	}
	if recs[0].Type != "A" || recs[0].TTL == nil || *recs[0].TTL != 300 {
		t.Fatalf("A record: %+v", recs[0])
	}
	if recs[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("MAC = %q", recs[0].MAC)
	}
	if recs[1].Type != "COMMENT" || recs[1].Name != ";" {
		t.Fatalf("comment: %+v", recs[1])
	}
	if recs[2].Type != "$DOMAIN" {
		t.Fatalf("domain: %+v", recs[2])
	}
}

func TestParseZoneRecordsRejectsMACOnMX(t *testing.T) {
	_, err := ParseZoneRecords([]models.DnsZoneRecordDTO{
		{Name: "@", Type: "MX", Value: "10 mail.example.com.", MAC: "aa:bb:cc:dd:ee:ff"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"},
		{"aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee:ff"},
		{"aabb.ccdd.eeff", "aa:bb:cc:dd:ee:ff"},
		{"aabbccddeeff", "aa:bb:cc:dd:ee:ff"},
	}
	for _, tc := range cases {
		got, err := NormalizeMAC(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeMAC(%q) = %q, %v want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := NormalizeMAC("not-a-mac"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseZoneRecordsRejectsBadA(t *testing.T) {
	_, err := ParseZoneRecords([]models.DnsZoneRecordDTO{
		{Name: "www", Type: "A", Value: "not-an-ip"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseTemplateNameservers(t *testing.T) {
	ns, err := ParseTemplateNameservers([]models.DnsTemplateNameserverDTO{
		{Hostname: " ns1.example.com. ", Address: "192.0.2.53"},
		{Hostname: "ns1.example.com.", Address: "2001:db8::53"},
		{Hostname: "ns2.example.com."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 3 || ns[0].Hostname != "ns1.example.com." || ns[0].Address != "192.0.2.53" || ns[2].Rank != 2 {
		t.Fatalf("%+v", ns)
	}
	if ns[1].Address != "2001:db8::53" {
		t.Fatalf("address = %q", ns[1].Address)
	}
	if _, err := ParseTemplateNameservers(nil); err == nil {
		t.Fatal("expected error for empty nameservers")
	}
	if _, err := ParseTemplateNameservers([]models.DnsTemplateNameserverDTO{
		{Hostname: "ns1.example.com.", Address: "not-an-ip"},
	}); err == nil {
		t.Fatal("expected error for bad address")
	}
	if _, err := ParseTemplateNameservers([]models.DnsTemplateNameserverDTO{
		{Hostname: "ns1.example.com.", Address: "192.0.2.53"},
		{Hostname: "NS1.Example.COM.", Address: "::ffff:192.0.2.53"},
	}); err == nil {
		t.Fatal("expected error for duplicate address")
	}
}
