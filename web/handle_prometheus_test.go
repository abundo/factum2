package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestApiPrometheusConfig(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.DefaultDomain = "example.com"
	settings.PrometheusDestFile = "/etc/prometheus/snmp_targets.json"
	settings.PrometheusReloadURL = "http://127.0.0.1:9090/-/reload"
	settings.PrometheusModule = "if_mib"
	settings.PrometheusAuth = "public_v2"
	settings.PrometheusIgnoreDevices = "lab-sw1"
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/prometheus-config", nil, nil, nil)
	if err := ctrl.ApiPrometheusConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	var body PrometheusConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.DestFile != settings.PrometheusDestFile {
		t.Fatalf("dest_file = %q", body.DestFile)
	}
	if body.ReloadURL != settings.PrometheusReloadURL {
		t.Fatalf("reload_url = %q", body.ReloadURL)
	}
	if body.Module != "if_mib" || body.Auth != "public_v2" {
		t.Fatalf("module/auth = %q %q", body.Module, body.Auth)
	}
	if body.IgnoreDevices != "lab-sw1" {
		t.Fatalf("ignore_devices = %q", body.IgnoreDevices)
	}
	if body.DefaultDomain != "example.com" {
		t.Fatalf("default_domain = %q", body.DefaultDomain)
	}
}
