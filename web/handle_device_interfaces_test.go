package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/models"
	"github.com/abundo/factum2/internal/netboxtool"
)

type fakeRefreshNetbox struct {
	updatedIDs    []int
	deletedIDs    []int
	deleteErr     error
	deviceType    *netboxtool.NetboxDeviceTypeDetail
	deviceTypeErr error
}

func (f *fakeRefreshNetbox) InterfaceUpdate(id int, _ map[string]any) error {
	f.updatedIDs = append(f.updatedIDs, id)
	return nil
}

func (f *fakeRefreshNetbox) InterfaceDelete(id int) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}

func (f *fakeRefreshNetbox) GetDeviceType(_, _ string) (*netboxtool.NetboxDeviceTypeDetail, error) {
	if f.deviceTypeErr != nil {
		return nil, f.deviceTypeErr
	}
	return f.deviceType, nil
}

type refreshDriverStub struct {
	elinePackStub
	ifaces []*netboxtool.NBInterface
}

func (s *refreshDriverStub) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	return s.ifaces, nil
}

func TestApplyLiveInterfaceRefreshDeletesMissingSubinterface(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "lu17-lab-r0", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	keep := models.Interface{DeviceID: dev.ID, NetboxID: 10, Name: "Ethernet19", Description: "old", Type: "1000base-t"}
	gone := models.Interface{DeviceID: dev.ID, NetboxID: 11, Name: "Ethernet19.238", Description: "stale", Type: "virtual"}
	if err := db.Create(&keep).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&gone).Error; err != nil {
		t.Fatal(err)
	}
	dev.Interfaces = []models.Interface{keep, gone}

	nb := &fakeRefreshNetbox{}
	live := []*netboxtool.NBInterface{
		{Name: "Ethernet19", Description: "uplink"},
	}
	if err := applyLiveInterfaceRefresh(db, nb, dev, live, nil, false); err != nil {
		t.Fatal(err)
	}

	if len(nb.deletedIDs) != 1 || nb.deletedIDs[0] != 11 {
		t.Errorf("deletedIDs = %v, want [11]", nb.deletedIDs)
	}
	if len(nb.updatedIDs) != 1 || nb.updatedIDs[0] != 10 {
		t.Errorf("updatedIDs = %v, want [10]", nb.updatedIDs)
	}

	var names []string
	if err := db.Model(&models.Interface{}).Where("device_id = ?", dev.ID).Order("name").Pluck("name", &names).Error; err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "Ethernet19" {
		t.Errorf("factum interfaces = %v, want [Ethernet19]", names)
	}

	var updated models.Interface
	if err := db.Where("id = ?", keep.ID).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Description != "uplink" {
		t.Errorf("description = %q, want uplink", updated.Description)
	}
}

func TestApplyLiveInterfaceRefreshKeepsTemplatePort(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "sw1", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	mgmt := models.Interface{DeviceID: dev.ID, NetboxID: 20, Name: "Management1", Type: "1000base-t"}
	stale := models.Interface{DeviceID: dev.ID, NetboxID: 21, Name: "Ethernet2", Type: "1000base-t"}
	if err := db.Create(&mgmt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	dev.Interfaces = []models.Interface{mgmt, stale}

	nb := &fakeRefreshNetbox{}
	templates := map[string]string{"Management1": "1000base-t"}
	live := []*netboxtool.NBInterface{{Name: "Loopback0"}}
	if err := applyLiveInterfaceRefresh(db, nb, dev, live, templates, true); err != nil {
		t.Fatal(err)
	}
	if len(nb.deletedIDs) != 1 || nb.deletedIDs[0] != 21 {
		t.Errorf("deletedIDs = %v, want [21] (Management1 is a template port)", nb.deletedIDs)
	}

	var names []string
	if err := db.Model(&models.Interface{}).Where("device_id = ?", dev.ID).Order("name").Pluck("name", &names).Error; err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "Management1" {
		t.Errorf("factum interfaces = %v, want [Management1]", names)
	}
}

func TestApplyLiveInterfaceRefreshUnknownTemplatesSkipsPhysical(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "sw1", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	phys := models.Interface{DeviceID: dev.ID, NetboxID: 30, Name: "Ethernet3", Type: "1000base-t"}
	sub := models.Interface{DeviceID: dev.ID, NetboxID: 31, Name: "Ethernet3.50", Type: "virtual"}
	if err := db.Create(&phys).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	dev.Interfaces = []models.Interface{phys, sub}

	nb := &fakeRefreshNetbox{}
	live := []*netboxtool.NBInterface{{Name: "Loopback0"}}
	if err := applyLiveInterfaceRefresh(db, nb, dev, live, nil, false); err != nil {
		t.Fatal(err)
	}
	if len(nb.deletedIDs) != 1 || nb.deletedIDs[0] != 31 {
		t.Errorf("deletedIDs = %v, want [31] (physical Ethernet3 kept without templates)", nb.deletedIDs)
	}
}

func TestApplyLiveInterfaceRefreshEmptyLiveDoesNotPrune(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "sw1", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	sub := models.Interface{DeviceID: dev.ID, NetboxID: 51, Name: "Ethernet1.1", Type: "virtual"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	dev.Interfaces = []models.Interface{sub}

	nb := &fakeRefreshNetbox{}
	if err := applyLiveInterfaceRefresh(db, nb, dev, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if len(nb.deletedIDs) != 0 {
		t.Errorf("deletedIDs = %v, want none when live list is empty", nb.deletedIDs)
	}
}

func TestApplyLiveInterfaceRefreshNetboxDeleteErrorLeavesFactumRow(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "sw1", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	gone := models.Interface{DeviceID: dev.ID, NetboxID: 41, Name: "Ethernet1.1", Type: "virtual"}
	if err := db.Create(&gone).Error; err != nil {
		t.Fatal(err)
	}
	dev.Interfaces = []models.Interface{gone}

	nb := &fakeRefreshNetbox{deleteErr: errors.New("netbox down")}
	live := []*netboxtool.NBInterface{{Name: "Loopback0"}}
	err := applyLiveInterfaceRefresh(db, nb, dev, live, nil, false)
	if err == nil {
		t.Fatal("expected error")
	}

	var count int64
	if err := db.Model(&models.Interface{}).Where("id = ?", gone.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("factum row count = %d, want 1 (not deleted after netbox failure)", count)
	}
}

func TestApiDeviceInterfacesRefreshRemovesMissingSubinterface(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "sync-user", Password: "sync-pass",
	}).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{
		Name: "lu17-lab-r0", Platform: "eos", Manufacturer: "Arista", ModelName: "DCS-7280",
	}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	keep := models.Interface{DeviceID: dev.ID, NetboxID: 10, Name: "Ethernet19", Type: "1000base-t"}
	gone := models.Interface{DeviceID: dev.ID, NetboxID: 11, Name: "Ethernet19.238", Type: "virtual"}
	if err := db.Create(&keep).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&gone).Error; err != nil {
		t.Fatal(err)
	}

	nb := &fakeRefreshNetbox{
		deviceType: &netboxtool.NetboxDeviceTypeDetail{
			Interfaces: []netboxtool.NetboxInterfaceTemplate{
				{Name: "Ethernet19", Type: "1000base-t"},
			},
		},
	}
	ctrl := &Controller{DB: db}
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return &refreshDriverStub{ifaces: []*netboxtool.NBInterface{
			{Name: "Ethernet19", Description: "uplink"},
		}}, nil
	}
	ctrl.interfaceNetboxFn = func(_ *models.Settings) (interfaceRefreshNetbox, error) {
		return nb, nil
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/device/x/interfaces/refresh", nil, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := ctrl.ApiDeviceInterfacesRefresh(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var got models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Interfaces) != 1 || got.Interfaces[0].Name != "Ethernet19" {
		t.Errorf("returned interfaces = %+v, want [Ethernet19]", got.Interfaces)
	}
	if len(nb.deletedIDs) != 1 || nb.deletedIDs[0] != 11 {
		t.Errorf("deletedIDs = %v, want [11]", nb.deletedIDs)
	}
}

func TestDeviceSyncCredentials(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "defuser", Password: "defpass",
	}).Error; err != nil {
		t.Fatalf("create default: %v", err)
	}
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "lab-sw1", Username: "labuser", Password: "labpass",
	}).Error; err != nil {
		t.Fatalf("create override: %v", err)
	}

	got, err := ctrl.deviceSyncCredentials("lab-sw1")
	if err != nil {
		t.Fatalf("exact: %v", err)
	}
	if got.Username != "labuser" || got.Password != "labpass" {
		t.Errorf("exact = %+v, want labuser/labpass", got)
	}

	got, err = ctrl.deviceSyncCredentials("prod-sw1")
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if got.Username != "defuser" || got.Password != "defpass" {
		t.Errorf("fallback = %+v, want defuser/defpass", got)
	}

	if err := db.Where("name = ?", "default").Delete(&models.DeviceSyncAuth{}).Error; err != nil {
		t.Fatalf("delete default: %v", err)
	}
	if _, err := ctrl.deviceSyncCredentials("prod-sw1"); err == nil {
		t.Fatal("missing default: expected error")
	}
}

func TestDeviceSyncCredentialsIncomplete(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "onlyuser",
	}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := ctrl.deviceSyncCredentials("any"); err == nil {
		t.Fatal("incomplete: expected error")
	}
}

func TestServiceCommitComment(t *testing.T) {
	c, _ := jsonRequest(t, "POST", "/api/service/1/push", nil, nil, nil)
	if got := serviceCommitComment(c, "CN00042", "push"); got != "factum push CN00042" {
		t.Errorf("no user = %q", got)
	}

	c.Set("user", models.User{Username: "alice", Name: "Alice Andersson"})
	if got := serviceCommitComment(c, "CN00042", "push"); got != "factum push CN00042 by Alice Andersson" {
		t.Errorf("named user = %q", got)
	}

	c.Set("user", models.User{Username: "bob"})
	if got := serviceCommitComment(c, "CN00042", "remove"); got != "factum remove CN00042 by bob" {
		t.Errorf("username only = %q", got)
	}

	c.Set("user", models.User{Username: "carol", Name: "  "})
	if got := serviceCommitComment(c, "", "push"); got != "factum push service by carol" {
		t.Errorf("blank id/name = %q", got)
	}
}

func TestApiDeviceInterfacesRefreshUsesDeviceSyncAuth(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "sync-user", Password: "sync-pass",
	}).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "lab-sw1", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}
	var got deviceCredentialsRequest
	ctrl.driverFn = func(_ *models.Device, creds deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		got = creds
		return nil, errors.New("stop after creds")
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/device/x/interfaces/refresh", nil, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := ctrl.ApiDeviceInterfacesRefresh(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got.Username != "sync-user" || got.Password != "sync-pass" {
		t.Fatalf("creds = %+v, want DeviceSyncAuth default", got)
	}
}

func TestApiDeviceInterfacesRefreshRequiresDeviceSyncAuth(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "lab-sw1", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}
	called := false
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		called = true
		return nil, errors.New("should not open driver")
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/device/x/interfaces/refresh", nil, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := ctrl.ApiDeviceInterfacesRefresh(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no device-sync credentials") {
		t.Fatalf("body = %s, want stored-auth error", rec.Body.String())
	}
	if called {
		t.Fatal("driver opened without DeviceSyncAuth")
	}
}

func TestApiDeviceInterfacesUpdateUsesDeviceSyncAuth(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "lab-sw1", Username: "labuser", Password: "labpass",
	}).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "lab-sw1", Platform: "eos"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Description: "old"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}
	var got deviceCredentialsRequest
	ctrl.driverFn = func(_ *models.Device, creds deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		got = creds
		return nil, errors.New("stop after creds")
	}
	body := map[string]any{
		"interfaces": []map[string]any{{"id": iface.ID, "description": "new"}},
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/device/x/interfaces/update", body, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := ctrl.ApiDeviceInterfacesUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got.Username != "labuser" || got.Password != "labpass" {
		t.Fatalf("creds = %+v, want per-device DeviceSyncAuth", got)
	}
}
