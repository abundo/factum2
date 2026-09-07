package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func setupDNSZones(t *testing.T) *Controller {
	t.Helper()
	db := newTestDB(t)
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	on := true
	s.DnsZonesEnabled = &on
	if err := db.Save(s).Error; err != nil {
		t.Fatalf("enable dns zones: %v", err)
	}
	return &Controller{DB: db}
}

func TestRequireDnsZonesEnabled_OffIs404(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/dns/zones", nil, nil, nil)
	called := false
	err := ctrl.RequireDnsZonesEnabled(func(c *echo.Context) error {
		called = true
		return c.JSON(http.StatusOK, map[string]any{"ok": true})
	})(c)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if called {
		t.Fatal("next ran while DNS zone editor is disabled")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestDnsZoneEditorLifecycle(t *testing.T) {
	ctrl := setupDNSZones(t)

	c, rec := jsonRequest(t, http.MethodPost, "/api/dns/soa-templates", dnsSOABody{
		Name: "default", Mname: "ns1.example.com.", Rname: "hostmaster.example.com.",
		Refresh: 36000, Retry: 3600, Expire: 604800, TTL: 900,
	}, nil, nil)
	if err := ctrl.ApiDnsSOACreate(c); err != nil {
		t.Fatalf("create soa: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create soa status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var soa models.DnsSOATemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &soa); err != nil {
		t.Fatalf("decode soa: %v", err)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dns/dnssec-policies", dnsPolicyBody{
		Name: "dnssec-policy", KSKAlgorithm: "ecdsap256sha256", ZSKAlgorithm: "ecdsap256sha256",
	}, nil, nil)
	if err := ctrl.ApiDnsPolicyCreate(c); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create policy status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var policy models.DnsDNSSECPolicy
	if err := json.Unmarshal(rec.Body.Bytes(), &policy); err != nil {
		t.Fatalf("decode policy: %v", err)
	}

	policyID := policy.ID
	c, rec = jsonRequest(t, http.MethodPost, "/api/dns/templates", dnsTemplateBody{
		Name: "default_dns", SOATemplateID: soa.ID, DefaultTTL: 900,
		DNSSECPolicyID: &policyID,
		Nameservers:    []string{"ns1.example.com.", "ns2.example.com."},
	}, nil, nil)
	if err := ctrl.ApiDnsTemplateCreate(c); err != nil {
		t.Fatalf("create template: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create template status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var tmpl models.DnsTemplateDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &tmpl); err != nil {
		t.Fatalf("decode template: %v", err)
	}
	if tmpl.SOATemplate != "default" || tmpl.DNSSECPolicy != "dnssec-policy" || len(tmpl.Nameservers) != 2 {
		t.Fatalf("unexpected template: %+v", tmpl)
	}

	ttl := uint(300)
	c, rec = jsonRequest(t, http.MethodPost, "/api/dns/zones", dnsZoneBody{
		Name: "example.com", Type: "forward", DnsTemplateID: tmpl.ID,
		Comment: "lab zone",
		Records: []models.DnsZoneRecordDTO{
			{Name: "www", Type: "A", Value: "192.0.2.10", TTL: &ttl},
			{Name: "@", Type: "MX", Value: "10 mail.example.com."},
		},
	}, nil, nil)
	if err := ctrl.ApiDnsZoneCreate(c); err != nil {
		t.Fatalf("create zone: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create zone status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var zone models.DnsZoneDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &zone); err != nil {
		t.Fatalf("decode zone: %v", err)
	}
	if zone.Name != "example.com" || zone.DnsTemplate != "default_dns" || len(zone.Records) != 2 {
		t.Fatalf("unexpected zone: %+v", zone)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dns/zones/"+itoa(zone.ID), dnsZoneBody{
		Name: "example.com", Type: "forward", DnsTemplateID: tmpl.ID,
		Records: []models.DnsZoneRecordDTO{
			{Name: "www", Type: "A", Value: "192.0.2.11"},
		},
	}, []string{"id"}, []string{itoa(zone.ID)})
	if err := ctrl.ApiDnsZoneUpdate(c); err != nil {
		t.Fatalf("update zone: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update zone status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &zone); err != nil {
		t.Fatalf("decode updated zone: %v", err)
	}
	if len(zone.Records) != 1 || zone.Records[0].Value != "192.0.2.11" {
		t.Fatalf("records not replaced: %+v", zone.Records)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dns/templates/"+itoa(tmpl.ID), nil, []string{"id"}, []string{itoa(tmpl.ID)})
	if err := ctrl.ApiDnsTemplateDelete(c); err != nil {
		t.Fatalf("delete template in use: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in-use template status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dns/zones/"+itoa(zone.ID), nil, []string{"id"}, []string{itoa(zone.ID)})
	if err := ctrl.ApiDnsZoneDelete(c); err != nil {
		t.Fatalf("delete zone: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete zone status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dns/zones", dnsZoneBody{
		Name: "192.0.2.1/24", Type: "reverse4", DnsTemplateID: tmpl.ID,
	}, nil, nil)
	if err := ctrl.ApiDnsZoneCreate(c); err != nil {
		t.Fatalf("create reverse with host bits: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("host bits status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/dns-config", nil, nil, nil)
	if err := ctrl.ApiDNSConfig(c); err != nil {
		t.Fatalf("dns-config: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("dns-config status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var cfg DNSConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode dns-config: %v", err)
	}
	if !cfg.ZonesEnabled || len(cfg.SOATemplates) != 1 || len(cfg.Templates) != 1 {
		t.Fatalf("unexpected dns-config: %+v", cfg)
	}
}

func TestDnsZoneBadRecord(t *testing.T) {
	ctrl := setupDNSZones(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/dns/soa-templates", dnsSOABody{Name: "soa", Mname: "ns."}, nil, nil)
	if err := ctrl.ApiDnsSOACreate(c); err != nil {
		t.Fatal(err)
	}
	var soa models.DnsSOATemplate
	json.Unmarshal(rec.Body.Bytes(), &soa)
	c, rec = jsonRequest(t, http.MethodPost, "/api/dns/templates", dnsTemplateBody{
		Name: "t", SOATemplateID: soa.ID, Nameservers: []string{"ns."},
	}, nil, nil)
	if err := ctrl.ApiDnsTemplateCreate(c); err != nil {
		t.Fatal(err)
	}
	var tmpl models.DnsTemplateDTO
	json.Unmarshal(rec.Body.Bytes(), &tmpl)

	c, rec = jsonRequest(t, http.MethodPost, "/api/dns/zones", dnsZoneBody{
		Name: "example.com", Type: "forward", DnsTemplateID: tmpl.ID,
		Records: []models.DnsZoneRecordDTO{{Name: "www", Type: "A", Value: "not-an-ip"}},
	}, nil, nil)
	if err := ctrl.ApiDnsZoneCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
