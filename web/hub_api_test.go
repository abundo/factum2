package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
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
