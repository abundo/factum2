package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestDeviceRoleFactumCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	if err := db.Create(&models.Device{Name: "local1", Role: "Shelf", CfSource: "factum"}).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/device-roles", models.DeviceRoleDTO{
		Name: "Shelf", Color: "#AaBbCc", Description: "local", VMRole: true,
	}, nil, nil)
	if err := ctrl.ApiCreateDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created models.DeviceRole
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Source != "factum" || created.NetboxID != 0 || created.Slug != "shelf" || created.Color != "aabbcc" || !created.VMRole {
		t.Fatalf("created %+v", created)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/device-roles/"+strconv.FormatUint(uint64(created.ID), 10), models.DeviceRoleDTO{
		Name: "Shelf A", Slug: "shelf",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiUpdateDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}
	var dev models.Device
	if err := db.Where("name = ?", "local1").First(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if dev.Role != "Shelf A" || dev.RoleID != 0 {
		t.Fatalf("device after rename %+v", dev)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/device-roles/"+strconv.FormatUint(uint64(created.ID), 10), nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeleteDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in use %d %s", rec.Code, rec.Body.String())
	}
	if err := db.Model(&dev).Update("role", "other").Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/device-roles/"+strconv.FormatUint(uint64(created.ID), 10), nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeleteDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeviceRoleNetboxCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	fake := &deviceRoleNetboxFake{}
	useDeviceRoleNetbox(t, ctrl, fake.serve)
	if err := db.Create(&models.Device{Name: "sw1", Role: "Router", RoleID: 0, CfSource: "factum"}).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/device-roles", models.DeviceRoleDTO{
		Name: "Router", Netbox: true, VMRole: true,
	}, nil, nil)
	if err := ctrl.ApiCreateDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created models.DeviceRole
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Source != "netbox" || created.NetboxID == 0 || created.Slug != "router" || created.Color != "9e9e9e" {
		t.Fatalf("created %+v", created)
	}
	var dev models.Device
	if err := db.Where("name = ?", "sw1").First(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if dev.RoleID != created.NetboxID {
		t.Fatalf("device role id %d, want %d", dev.RoleID, created.NetboxID)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/x", models.DeviceRoleDTO{
		Name: "Core", Slug: "core", Color: "112233", Description: "edge",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiUpdateDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}
	fake.mu.Lock()
	patched := fake.patched
	fake.mu.Unlock()
	if patched["name"] != "Core" || patched["slug"] != "core" || patched["color"] != "112233" {
		t.Fatalf("patch %+v", patched)
	}
	if err := db.Where("name = ?", "sw1").First(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if dev.Role != "Core" {
		t.Fatalf("device role %q", dev.Role)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeleteDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in use %d %s", rec.Code, rec.Body.String())
	}
	if fake.deleted != 0 {
		t.Fatal("netbox delete ran while the role was in use")
	}
	if err := db.Model(&dev).Updates(map[string]any{"role_id": 0, "role": ""}).Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodDelete, "/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeleteDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
	if fake.deleted != created.NetboxID {
		t.Fatalf("deleted netbox id %d, want %d", fake.deleted, created.NetboxID)
	}
}

func TestDeviceRoleNetboxRequiresConfig(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/device-roles", models.DeviceRoleDTO{
		Name: "Router", Netbox: true,
	}, nil, nil)
	if err := ctrl.ApiCreateDeviceRole(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
}

type deviceRoleNetboxFake struct {
	mu      sync.Mutex
	next    uint
	patched map[string]any
	deleted uint
}

func (f *deviceRoleNetboxFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/dcim/device-roles/":
		f.next++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.next})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/dcim/device-roles/"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.patched = body
		_ = json.NewEncoder(w).Encode(body)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/dcim/device-roles/"):
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/dcim/device-roles/"), "/")
		n, _ := strconv.ParseUint(id, 10, 64)
		f.deleted = uint(n)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useDeviceRoleNetbox(t *testing.T, ctrl *Controller, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		t.Fatal(err)
	}
	settings.NetboxApiURL = srv.URL
	settings.NetboxApiToken = "t"
	if err := ctrl.DB.Save(settings).Error; err != nil {
		t.Fatal(err)
	}
}
