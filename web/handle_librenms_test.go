package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestApiLibrenmsPendingDeletes(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/librenms/pending-deletes", nil, nil, nil)
	if err := ctrl.ApiLibrenmsPendingDeleteList(c); err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var listed []models.LibrenmsPendingDelete
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listed == nil || len(listed) != 0 {
		t.Fatalf("empty list = %#v, want []", listed)
	}

	when := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	c, rec = jsonRequest(t, http.MethodPut, "/api/librenms/pending-deletes/7", models.LibrenmsPendingDelete{
		DeviceID:    7,
		Hostname:    "10.1.1.1",
		Display:     "rtr1",
		Reason:      "no_match",
		ScheduledAt: when,
	}, []string{"device_id"}, []string{"7"})
	if err := ctrl.ApiLibrenmsPendingDeleteUpsert(c); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.LibrenmsPendingDelete
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode upsert: %v", err)
	}
	if created.DeviceID != 7 || created.ForceDelete || created.Hostname != "10.1.1.1" {
		t.Fatalf("created = %+v", created)
	}

	later := when.Add(24 * time.Hour)
	c, rec = jsonRequest(t, http.MethodPut, "/api/librenms/pending-deletes/7", models.LibrenmsPendingDelete{
		DeviceID:    7,
		Hostname:    "10.1.1.1",
		Display:     "rtr1-renamed",
		Reason:      "disabled",
		ScheduledAt: later,
		ForceDelete: true,
	}, []string{"device_id"}, []string{"7"})
	if err := ctrl.ApiLibrenmsPendingDeleteUpsert(c); err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	var updated models.LibrenmsPendingDelete
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode second upsert: %v", err)
	}
	if updated.Display != "rtr1-renamed" || updated.Reason != "disabled" {
		t.Fatalf("updated fields = %+v", updated)
	}
	if updated.ForceDelete {
		t.Fatal("upsert must not set ForceDelete")
	}
	if !updated.ScheduledAt.Equal(created.ScheduledAt) {
		t.Fatalf("scheduled_at reset: %v -> %v", created.ScheduledAt, updated.ScheduledAt)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/librenms/pending-deletes/7/delete-next-sync", nil, []string{"device_id"}, []string{"7"})
	if err := ctrl.ApiLibrenmsPendingDeleteNextSync(c); err != nil {
		t.Fatalf("next-sync: %v", err)
	}
	var forced models.LibrenmsPendingDelete
	if err := json.Unmarshal(rec.Body.Bytes(), &forced); err != nil {
		t.Fatalf("decode next-sync: %v", err)
	}
	if !forced.ForceDelete {
		t.Fatal("force_delete not set")
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/librenms/pending-deletes/99/delete-next-sync", nil, []string{"device_id"}, []string{"99"})
	if err := ctrl.ApiLibrenmsPendingDeleteNextSync(c); err != nil {
		t.Fatalf("next-sync missing: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing next-sync status = %d, want 404", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/librenms/pending-deletes/7", nil, []string{"device_id"}, []string{"7"})
	if err := ctrl.ApiLibrenmsPendingDeleteRemove(c); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove status = %d, want 204", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/librenms/pending-deletes", nil, nil, nil)
	if err := ctrl.ApiLibrenmsPendingDeleteList(c); err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("list after delete = %#v", listed)
	}
}

func TestApiLibrenmsConfigDelayedDelete(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	settings.LibrenmsDelayedDeleteEnabled = &on
	settings.LibrenmsDelayedDeleteDays = 0
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/librenms-config", nil, nil, nil)
	if err := ctrl.ApiLibrenmsConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	var body LibrenmsConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.DelayedDeleteEnabled {
		t.Fatal("delayed_delete_enabled should be true")
	}
	if body.DelayedDeleteDays != 30 {
		t.Fatalf("days = %d, want 30 default", body.DelayedDeleteDays)
	}
}
