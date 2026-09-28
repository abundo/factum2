package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/radius"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestRadiusConfigAndPolicy(t *testing.T) {
	db := newTestDB(t)
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	s.RadiusEnabled = &on
	s.RadiusListen = ":1812"
	s.LdapHost = "ldap.example.com"
	s.LdapBindPassword = "bind-secret"
	s.LdapBaseDN = "dc=example,dc=com"
	s.LdapUserFilter = "(uid=%s)"
	if err := db.Save(s).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "wdm1", Role: "WDM", Enabled: true, PrimaryIPv4: "10.0.0.5/32"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Management1", Enabled: true}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}
	addr := models.Address{InterfaceID: iface.ID, Address: "10.0.0.8/24"}
	if err := db.Create(&addr).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.RadiusClient{Name: "wdm1", Address: "10.0.0.5", Secret: "nas-secret", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.RadiusPolicy{GroupDN: "cn=wdm-ops,dc=example,dc=com", Roles: "WDM"}).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/radius-config", nil, nil, nil)
	if err := ctrl.ApiRadiusConfig(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var cfg radius.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.Reply, `Cisco-AVPair = "shell:priv-lvl=15"`) {
		t.Fatalf("reply default missing: %q", cfg.Reply)
	}
	if !cfg.Enabled || cfg.LDAP.BindPassword != "bind-secret" || cfg.LDAP.Host != "ldap.example.com" {
		t.Fatalf("ldap %+v enabled %v", cfg.LDAP, cfg.Enabled)
	}
	if len(cfg.Clients) != 1 || cfg.Clients[0].Secret != "nas-secret" {
		t.Fatalf("clients %+v", cfg.Clients)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].Role != "WDM" {
		t.Fatalf("devices %+v", cfg.Devices)
	}
	found := map[string]bool{}
	for _, a := range cfg.Devices[0].Addresses {
		found[a] = true
	}
	if !found["10.0.0.5"] || !found["10.0.0.8"] {
		t.Fatalf("addresses %v", cfg.Devices[0].Addresses)
	}
	if len(cfg.Policies) != 1 || len(cfg.Policies[0].Roles) != 1 || cfg.Policies[0].Roles[0] != "WDM" {
		t.Fatalf("policies %+v", cfg.Policies)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/admin/radius/clients", nil, nil, nil)
	if err := ctrl.ApiRadiusClients(c); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() == "" || strings.Contains(rec.Body.String(), "nas-secret") {
		t.Fatalf("client list leaked the secret: %s", rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/radius-events", radiusEventBatch{Events: []models.RadiusEvent{{
		Username: "ada", NASIP: "10.0.0.5", DeviceName: "wdm1", DeviceRole: "WDM", Result: "accept", Reason: "ok", Worker: "w1",
	}}}, nil, nil)
	if err := ctrl.ApiRadiusEvents(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("events %d %s", rec.Code, rec.Body.String())
	}
	c, rec = jsonRequest(t, http.MethodGet, "/api/admin/radius/events", nil, nil, nil)
	if err := ctrl.ApiRadiusEvents(c); err != nil {
		t.Fatal(err)
	}
	var events []models.RadiusEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Username != "ada" || events[0].Result != "accept" {
		t.Fatalf("stored %+v", events)
	}
	c, rec = jsonRequest(t, http.MethodGet, "/api/admin/radius/device-roles", nil, nil, nil)
	if err := ctrl.ApiRadiusDeviceRoles(c); err != nil {
		t.Fatal(err)
	}
	var roles []string
	if err := json.Unmarshal(rec.Body.Bytes(), &roles); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0] != "WDM" {
		t.Fatalf("roles %+v", roles)
	}
}

func TestRadiusPolicyValidation(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodPost, "/api/admin/radius/policies", radiusPolicyDTO{
		GroupDN: "CN=net-admins,DC=example,DC=com", AllDevices: true,
	}, nil, nil)
	if err := ctrl.ApiRadiusPolicyCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	c, rec = jsonRequest(t, http.MethodPost, "/api/admin/radius/policies", radiusPolicyDTO{
		GroupDN: "not a dn", Roles: []string{"WDM"},
	}, nil, nil)
	if err := ctrl.ApiRadiusPolicyCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid dn status %d %s", rec.Code, rec.Body.String())
	}
}
