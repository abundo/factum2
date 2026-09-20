package web

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/internal/dns"
	"github.com/abundo/factum2/internal/util"
)

func TestApiDnsDhcpLeases_DhcpOffIs404(t *testing.T) {
	ctrl := setupDNSZones(t)
	c, rec := jsonRequest(t, http.MethodGet, "/api/dns/leases", nil, nil, nil)
	if err := ctrl.ApiDnsDhcpLeases(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiDnsDhcpLeases_ListsLeases(t *testing.T) {
	ctrl := setupDNSZones(t)
	s, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	s.DhcpEnabled = &on
	if err := ctrl.DB.Save(s).Error; err != nil {
		t.Fatal(err)
	}
	ctrl.dhcpLeasesFn = func(context.Context) ([]dns.DHCPLease, error) {
		return []dns.DHCPLease{
			{Family: "ipv4", IP: "192.0.2.100", MAC: "02:00:00:00:00:01", Hostname: "one.lab."},
			{Family: "ipv6", IP: "2001:db8::10", MAC: "02:00:00:00:00:01", Hostname: "one.lab."},
		}, nil
	}
	c, rec := jsonRequest(t, http.MethodGet, "/api/dns/leases", nil, nil, nil)
	if err := ctrl.ApiDnsDhcpLeases(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got dnsLeasesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Leases) != 2 {
		t.Fatalf("leases=%+v", got.Leases)
	}
	if got.Leases[0].MAC != "02:00:00:00:00:01" || got.Leases[0].Hostname != "one.lab." {
		t.Fatalf("first %+v", got.Leases[0])
	}
	if got.Leases[1].Family != "ipv6" {
		t.Fatalf("second %+v", got.Leases[1])
	}
}
