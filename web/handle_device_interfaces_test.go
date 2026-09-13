package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/models"
)

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
