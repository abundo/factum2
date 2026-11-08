package netbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
)

func TestSyncDeviceRoles(t *testing.T) {
	db := newImportTestDB(t)
	if err := db.Create(&models.DeviceRole{Name: "WDM", Slug: "wdm", Source: "factum"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Device{Name: "wdm1", Role: "old", RoleID: 7, CfSource: "netbox"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.DeviceRole{Name: "Gone", Slug: "gone", Source: "netbox", NetboxID: 9}).Error; err != nil {
		t.Fatal(err)
	}

	roles := []nbDeviceRoleREST{
		{ID: 7, Name: "WDM", Slug: "wdm", Color: "aabbcc", Description: "shelf", VMRole: true},
		{ID: 8, Name: "Router", Slug: "router", Color: "112233"},
	}
	if err := syncDeviceRoles(db, deviceRoleClient(t, roles), jobevent.NewSlogReporter("test", "roles")); err != nil {
		t.Fatal(err)
	}

	var wdm models.DeviceRole
	if err := db.Where("netbox_id = ?", 7).First(&wdm).Error; err != nil {
		t.Fatal(err)
	}
	if wdm.Name != "WDM" || wdm.Slug != "wdm" || wdm.Color != "aabbcc" || wdm.Description != "shelf" || !wdm.VMRole || wdm.Source != "netbox" {
		t.Fatalf("netbox role = %+v", wdm)
	}
	var local models.DeviceRole
	if err := db.Where("source = ? AND slug = ?", "factum", "wdm").First(&local).Error; err != nil {
		t.Fatal(err)
	}
	if local.NetboxID != 0 {
		t.Fatalf("factum role was connected to netbox: %+v", local)
	}
	var dev models.Device
	if err := db.Where("name = ?", "wdm1").First(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if dev.Role != "WDM" {
		t.Fatalf("device role = %q", dev.Role)
	}
	var n int64
	if err := db.Model(&models.DeviceRole{}).Where("netbox_id = ?", 9).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("unused role was kept")
	}
	if err := db.Model(&models.DeviceRole{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("roles = %d, want 3", n)
	}

	// A role that NetBox dropped stays while a device still points at it.
	if err := db.Model(&models.Device{}).Where("id = ?", dev.ID).Update("role_id", 8).Error; err != nil {
		t.Fatal(err)
	}
	if err := syncDeviceRoles(db, deviceRoleClient(t, roles[1:]), jobevent.NewSlogReporter("test", "roles")); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.DeviceRole{}).Where("netbox_id = ?", 7).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("role with no device should be removed")
	}

	if err := db.Model(&dev).Updates(map[string]any{"role_id": 8, "role": "old"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := syncDeviceRoles(db, deviceRoleClient(t, nil), jobevent.NewSlogReporter("test", "roles")); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.DeviceRole{}).Where("slug = ?", "router").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("empty role list should leave the catalog")
	}
	if err := db.Where("name = ?", "wdm1").First(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if dev.Role != "old" {
		t.Fatalf("empty list renamed the device to %q", dev.Role)
	}
}

func TestSyncDeviceRecordsRole(t *testing.T) {
	db := newImportTestDB(t)
	if _, err := syncDevice(db, &netboxtool.NBDevice{
		NetboxID: 10,
		Name:     "sw1",
		Role:     "Access",
		RoleID:   4,
		Status:   "active",
		CfSource: "netbox",
	}, nil); err != nil {
		t.Fatal(err)
	}
	var role models.DeviceRole
	if err := db.Where("netbox_id = ?", 4).First(&role).Error; err != nil {
		t.Fatal(err)
	}
	if role.Name != "Access" || role.Slug != "access" || role.Source != "netbox" {
		t.Fatalf("role = %+v", role)
	}
	if _, err := syncDevice(db, &netboxtool.NBDevice{
		NetboxID: 10,
		Name:     "sw1",
		Role:     "Access switch",
		RoleID:   4,
		Status:   "active",
		CfSource: "netbox",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("netbox_id = ?", 4).First(&role).Error; err != nil {
		t.Fatal(err)
	}
	if role.Name != "Access switch" || role.Slug != "access" {
		t.Fatalf("role after rename = %+v", role)
	}
}

func TestDeltaPlanDeviceRole(t *testing.T) {
	t0 := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	plan, err := newDeltaPlan([]netboxtool.ObjectChange{
		{ID: 1, Time: t0, Action: "update", ChangedType: "dcim.devicerole", ChangedID: 7},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.roles {
		t.Fatal("device role change was not planned")
	}
	if plan.devTypes || plan.racks {
		t.Fatal("device role change should not refresh device types or racks")
	}
}

func deviceRoleClient(t *testing.T, roles []nbDeviceRoleREST) *netboxtool.NetboxClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/dcim/device-roles/") {
			if roles == nil {
				roles = []nbDeviceRoleREST{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": roles, "next": nil})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return nb
}
