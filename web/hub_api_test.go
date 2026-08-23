package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func decodeHubResponse(t *testing.T, env worker.Envelope) worker.ResponseMsg {
	t.Helper()
	if env.Type != worker.EnvelopeResponse {
		t.Fatalf("envelope type %q, want response", env.Type)
	}
	var resp worker.ResponseMsg
	if err := json.Unmarshal(env.Payload, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

func TestHubAPILibrenmsConfigMatchesHandler(t *testing.T) {
	db := newTestDB(t)
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.DefaultDomain = "example.com"
	settings.LibrenmsApiURL = "http://librenms.example"
	settings.LibrenmsApiToken = "secret-token"
	on := true
	settings.LibrenmsDelayedDeleteEnabled = &on
	settings.LibrenmsDelayedDeleteDays = 14
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	rm := worker.NewRemoteManager(db)
	ctrl := Controller{DB: db, RemoteManager: rm}
	e.GET("/api/librenms-config", ctrl.ApiLibrenmsConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	var settingsHits atomic.Int32
	e.GET("/api/admin/settings", func(c *echo.Context) error {
		settingsHits.Add(1)
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}, ctrl.RequireAPIAuth, ctrl.RequireAdmin)
	rm.SetAPIHandler(e)

	c, rec := jsonRequest(t, http.MethodGet, "/api/librenms-config", nil, nil, nil)
	if err := ctrl.ApiLibrenmsConfig(c); err != nil {
		t.Fatalf("ApiLibrenmsConfig: %v", err)
	}
	var want LibrenmsConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &want); err != nil {
		t.Fatalf("decode direct: %v", err)
	}

	outbox := make(chan worker.Envelope, 1)
	rm.HandleHubRequest(context.Background(), "node1", outbox, worker.RequestMsg{
		ID: "1", Method: http.MethodGet, Path: "/api/librenms-config",
	})
	resp := decodeHubResponse(t, <-outbox)
	if resp.Status != http.StatusOK {
		t.Fatalf("hub status %d body=%s, want 200", resp.Status, resp.Body)
	}
	if resp.Error != "" {
		t.Fatalf("transport Error %q, want empty", resp.Error)
	}
	var got LibrenmsConfigResponse
	if err := json.Unmarshal(resp.Body, &got); err != nil {
		t.Fatalf("decode hub: %v", err)
	}
	if got != want {
		t.Fatalf("hub JSON %+v != ApiLibrenmsConfig %+v", got, want)
	}

	outbox = make(chan worker.Envelope, 1)
	rm.HandleHubRequest(context.Background(), "node1", outbox, worker.RequestMsg{
		ID: "2", Method: http.MethodGet, Path: "/api/admin/settings",
	})
	denied := decodeHubResponse(t, <-outbox)
	if denied.Status != http.StatusForbidden {
		t.Fatalf("admin settings status %d, want 403 from allowlist", denied.Status)
	}
	if settingsHits.Load() != 0 {
		t.Fatal("allowlist 403 must happen before the settings handler")
	}
}

func TestHubAPIDeviceIncludeInterfaces(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "dns-sync-r1", NetboxID: 424242, PrimaryIPv4: "10.0.0.1/32"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Ethernet1/1"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatalf("create interface: %v", err)
	}
	addr := models.Address{InterfaceID: iface.ID, Address: "10.1.1.1/24"}
	if err := db.Create(&addr).Error; err != nil {
		t.Fatalf("create address: %v", err)
	}

	e := echo.New()
	rm := worker.NewRemoteManager(db)
	ctrl := Controller{DB: db, RemoteManager: rm}
	e.GET("/api/device", ctrl.ApiGetDevices, ctrl.RequireAPIAuth, ctrl.RequireRead)
	e.GET("/api/device/name/:name", ctrl.ApiGetDeviceByName, ctrl.RequireAPIAuth, ctrl.RequireRead)
	var byIDHits, impactHits atomic.Int32
	e.GET("/api/device/:id", func(c *echo.Context) error {
		byIDHits.Add(1)
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}, ctrl.RequireAPIAuth, ctrl.RequireRead)
	e.GET("/api/device/:id/impact", func(c *echo.Context) error {
		impactHits.Add(1)
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}, ctrl.RequireAPIAuth, ctrl.RequireRead)
	rm.SetAPIHandler(e)

	hubGet := func(id, path string) worker.ResponseMsg {
		t.Helper()
		outbox := make(chan worker.Envelope, 1)
		rm.HandleHubRequest(context.Background(), "node1", outbox, worker.RequestMsg{
			ID: id, Method: http.MethodGet, Path: path,
		})
		return decodeHubResponse(t, <-outbox)
	}

	shallow := hubGet("1", "/api/device")
	if shallow.Status != http.StatusOK {
		t.Fatalf("shallow status %d body=%s", shallow.Status, shallow.Body)
	}
	var shallowDevs []models.Device
	if err := json.Unmarshal(shallow.Body, &shallowDevs); err != nil {
		t.Fatalf("decode shallow: %v", err)
	}
	if len(shallowDevs) != 1 {
		t.Fatalf("shallow count %d, want 1", len(shallowDevs))
	}
	if len(shallowDevs[0].Interfaces) != 0 {
		t.Fatalf("shallow interfaces = %d, want 0", len(shallowDevs[0].Interfaces))
	}

	full := hubGet("2", "/api/device?include=interfaces")
	if full.Status != http.StatusOK {
		t.Fatalf("include status %d body=%s", full.Status, full.Body)
	}
	var fullDevs []models.Device
	if err := json.Unmarshal(full.Body, &fullDevs); err != nil {
		t.Fatalf("decode include: %v", err)
	}
	if len(fullDevs) != 1 || len(fullDevs[0].Interfaces) != 1 {
		t.Fatalf("include devices=%d interfaces=%d, want 1/1", len(fullDevs), len(fullDevs[0].Interfaces))
	}
	if fullDevs[0].Interfaces[0].Name != "Ethernet1/1" {
		t.Errorf("interface name = %q", fullDevs[0].Interfaces[0].Name)
	}
	if len(fullDevs[0].Interfaces[0].Addresses) != 1 || fullDevs[0].Interfaces[0].Addresses[0].Address != "10.1.1.1/24" {
		t.Errorf("addresses = %+v", fullDevs[0].Interfaces[0].Addresses)
	}

	byName := hubGet("3", "/api/device/name/dns-sync-r1")
	if byName.Status != http.StatusOK {
		t.Fatalf("by-name status %d body=%s", byName.Status, byName.Body)
	}

	denied := hubGet("4", "/api/device/1")
	if denied.Status != http.StatusForbidden {
		t.Fatalf("GET /api/device/1 status %d, want 403", denied.Status)
	}
	impact := hubGet("5", "/api/device/1/impact")
	if impact.Status != http.StatusForbidden {
		t.Fatalf("GET /api/device/1/impact status %d, want 403", impact.Status)
	}
	if byIDHits.Load() != 0 || impactHits.Load() != 0 {
		t.Fatal("allowlist 403 must happen before /api/device/:id handlers")
	}
}

func TestHubAPIPendingDeletesRoundTrip(t *testing.T) {
	db := newTestDB(t)
	e := echo.New()
	rm := worker.NewRemoteManager(db)
	ctrl := Controller{DB: db, RemoteManager: rm}
	e.GET("/api/librenms/pending-deletes", ctrl.ApiLibrenmsPendingDeleteList, ctrl.RequireAPIAuth, ctrl.RequireRead)
	e.PUT("/api/librenms/pending-deletes/:device_id", ctrl.ApiLibrenmsPendingDeleteUpsert, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	e.DELETE("/api/librenms/pending-deletes/:device_id", ctrl.ApiLibrenmsPendingDeleteRemove, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	var nextSyncHits atomic.Int32
	e.POST("/api/librenms/pending-deletes/:device_id/delete-next-sync", func(c *echo.Context) error {
		nextSyncHits.Add(1)
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	rm.SetAPIHandler(e)

	hub := func(id, method, path string, body json.RawMessage) worker.ResponseMsg {
		t.Helper()
		outbox := make(chan worker.Envelope, 1)
		rm.HandleHubRequest(context.Background(), "node1", outbox, worker.RequestMsg{
			ID: id, Method: method, Path: path, Body: body,
		})
		return decodeHubResponse(t, <-outbox)
	}

	when := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	putBody, err := json.Marshal(models.LibrenmsPendingDelete{
		DeviceID:    7,
		Hostname:    "10.1.1.1",
		Display:     "rtr1",
		Reason:      "no_match",
		ScheduledAt: when,
	})
	if err != nil {
		t.Fatal(err)
	}
	created := hub("1", http.MethodPut, "/api/librenms/pending-deletes/7", putBody)
	if created.Status != http.StatusOK {
		t.Fatalf("PUT status %d body=%s", created.Status, created.Body)
	}
	var row models.LibrenmsPendingDelete
	if err := json.Unmarshal(created.Body, &row); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	if row.DeviceID != 7 || row.Hostname != "10.1.1.1" {
		t.Fatalf("created = %+v", row)
	}

	listed := hub("2", http.MethodGet, "/api/librenms/pending-deletes", nil)
	if listed.Status != http.StatusOK {
		t.Fatalf("GET status %d body=%s", listed.Status, listed.Body)
	}
	var rows []models.LibrenmsPendingDelete
	if err := json.Unmarshal(listed.Body, &rows); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if len(rows) != 1 || rows[0].DeviceID != 7 {
		t.Fatalf("list = %#v", rows)
	}

	removed := hub("3", http.MethodDelete, "/api/librenms/pending-deletes/7", nil)
	if removed.Status != http.StatusNoContent {
		t.Fatalf("DELETE status %d, want 204 body=%s", removed.Status, removed.Body)
	}

	after := hub("4", http.MethodGet, "/api/librenms/pending-deletes", nil)
	if err := json.Unmarshal(after.Body, &rows); err != nil {
		t.Fatalf("decode GET after delete: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("list after delete = %#v", rows)
	}

	denied := hub("5", http.MethodPost, "/api/librenms/pending-deletes/7/delete-next-sync", nil)
	if denied.Status != http.StatusForbidden {
		t.Fatalf("delete-next-sync status %d, want 403", denied.Status)
	}
	if nextSyncHits.Load() != 0 {
		t.Fatal("allowlist 403 must happen before delete-next-sync handler")
	}
}
