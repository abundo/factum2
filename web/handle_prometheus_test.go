package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestApiSettingsUpdateIgnoreInterfaces(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.PrometheusIgnoreInterfaces = ".*\n"
	c, rec := jsonRequest(t, http.MethodPut, "/api/admin/settings", settings, nil, nil)
	if err := ctrl.ApiSettingsUpdate(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	settings.PrometheusIgnoreInterfaces = "Vlan.*\nNull.*"
	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/settings", settings, nil, nil)
	if err := ctrl.ApiSettingsUpdate(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	if got.PrometheusIgnoreInterfaces != "Vlan.*\nNull.*" {
		t.Fatalf("stored = %q", got.PrometheusIgnoreInterfaces)
	}

	settings.CommonIgnoreInterfaces = "["
	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/settings", settings, nil, nil)
	if err := ctrl.ApiSettingsUpdate(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("common status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

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
	settings.PrometheusIgnoreInterfaces = "Vlan.*\nNull.*"
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
	if body.IgnoreInterfaces != "Vlan.*\nNull.*" {
		t.Fatalf("ignore_interfaces = %q", body.IgnoreInterfaces)
	}
	if body.DefaultDomain != "example.com" {
		t.Fatalf("default_domain = %q", body.DefaultDomain)
	}
}

func TestApiPrometheusConfigCommonIgnore(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.CommonIgnoreDevices = "shared-sw"
	settings.CommonIgnoreManufacturers = "shared-vendor"
	settings.CommonIgnoreModels = "shared-model"
	settings.CommonIgnorePlatforms = "shared-platform"
	settings.CommonIgnoreInterfaces = "Vlan.*\nNull.*"
	settings.CommonRolesEnabled = "edge"
	settings.PrometheusIgnoreDevices = "own-sw"
	settings.PrometheusIgnoreModels = " \n"
	settings.PrometheusIgnoreInterfaces = " \n"
	settings.PrometheusRolesEnabled = " \n"
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
	if body.IgnoreDevices != "own-sw" {
		t.Fatalf("ignore_devices = %q", body.IgnoreDevices)
	}
	if body.IgnoreModels != "shared-model" {
		t.Fatalf("ignore_models = %q", body.IgnoreModels)
	}
	if body.IgnoreManufacturers != "shared-vendor" {
		t.Fatalf("ignore_manufacturers = %q", body.IgnoreManufacturers)
	}
	if body.IgnorePlatforms != "shared-platform" {
		t.Fatalf("ignore_platforms = %q", body.IgnorePlatforms)
	}
	if body.IgnoreInterfaces != "Vlan.*\nNull.*" {
		t.Fatalf("ignore_interfaces = %q", body.IgnoreInterfaces)
	}
	if body.RolesEnabled != "edge" {
		t.Fatalf("roles_enabled = %q", body.RolesEnabled)
	}
}
