package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestApiDeviceSyncConfigInventoryMaps(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

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
}
