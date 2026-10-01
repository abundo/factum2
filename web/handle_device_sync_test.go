package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestApiDeviceSyncConfigInventoryMaps(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	createTestELINEType(t, db)
	if err := db.Create(&models.ServiceType{
		Name: "ELAN", SyncSource: models.SyncSourceELAN, NetboxType: models.NetboxTypeVPLS,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ServiceType{
		Name: "L3VPN", SyncSource: models.SyncSourceL3VPN, NetboxType: models.NetboxTypeVRF,
	}).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/device-sync-config", nil, nil, nil)
	if err := ctrl.ApiDeviceSyncConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body DeviceSyncConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.InventoryMaps[models.SyncSourceELINE] != models.NetboxTypeEVPL {
		t.Errorf("eline = %q, want evpl", body.InventoryMaps[models.SyncSourceELINE])
	}
	if body.InventoryMaps[models.SyncSourceELAN] != models.NetboxTypeVPLS {
		t.Errorf("elan = %q, want vpls", body.InventoryMaps[models.SyncSourceELAN])
	}
	if body.InventoryMaps[models.SyncSourceL3VPN] != models.NetboxTypeVRF {
		t.Errorf("l3vpn = %q, want vrf", body.InventoryMaps[models.SyncSourceL3VPN])
	}
	if body.ServiceSources != nil {
		t.Errorf("service_sources = %v, want null when unset", body.ServiceSources)
	}
}

func TestApiDeviceSyncConfigServiceSources(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	createTestELINEType(t, db)
	if err := db.Create(&models.ServiceType{
		Name: "ELAN", SyncSource: models.SyncSourceELAN, NetboxType: models.NetboxTypeVPLS,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ServiceType{
		Name: "L3VPN", SyncSource: models.SyncSourceL3VPN, NetboxType: models.NetboxTypeVRF,
	}).Error; err != nil {
		t.Fatal(err)
	}

	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	only := "elan\n"
	settings.DeviceSyncServiceSources = &only
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/device-sync-config", nil, nil, nil)
	if err := ctrl.ApiDeviceSyncConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	var body DeviceSyncConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ServiceSources == nil || len(*body.ServiceSources) != 1 || (*body.ServiceSources)[0] != models.SyncSourceELAN {
		t.Fatalf("service_sources = %v, want [elan]", body.ServiceSources)
	}
	// The mapping stays intact; device-sync applies the allow-list itself.
	if body.InventoryMaps[models.SyncSourceELINE] != models.NetboxTypeEVPL {
		t.Errorf("eline mapping = %q, want evpl", body.InventoryMaps[models.SyncSourceELINE])
	}
}

func TestServiceSourceListEmptyMeansNone(t *testing.T) {
	empty := ""
	got := serviceSourceList(&empty)
	if got == nil || len(*got) != 0 {
		t.Fatalf("service sources = %v, want empty slice", got)
	}
	if serviceSourceList(nil) != nil {
		t.Fatal("nil setting must stay nil")
	}
}
