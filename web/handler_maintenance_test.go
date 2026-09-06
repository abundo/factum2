package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/abundo/factum2/models"
)

func TestApiMaintenanceCreateMultipleResources(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	devA := models.Device{Name: "roadm-a", NetboxID: 101}
	devB := models.Device{Name: "roadm-b", NetboxID: 102}
	if err := db.Create(&devA).Error; err != nil {
		t.Fatalf("device a: %v", err)
	}
	if err := db.Create(&devB).Error; err != nil {
		t.Fatalf("device b: %v", err)
	}
	ia := models.Interface{DeviceID: devA.ID, Name: "D1"}
	ib := models.Interface{DeviceID: devB.ID, Name: "D2"}
	if err := db.Create(&ia).Error; err != nil {
		t.Fatalf("iface a: %v", err)
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatalf("iface b: %v", err)
	}
	conn := models.Connection{
		DeviceAID: devA.ID, InterfaceAID: ia.ID,
		DeviceBID: devB.ID, InterfaceBID: ib.ID,
		Label: "span-1",
	}
	if err := db.Create(&conn).Error; err != nil {
		t.Fatalf("conn: %v", err)
	}
	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("cust: %v", err)
	}
	wl := models.Service{ServiceID: "VL00001", CustomerID: cust.ID}
	if err := db.Create(&wl).Error; err != nil {
		t.Fatalf("wl: %v", err)
	}
	fiber := models.Service{ServiceID: "LF00001", CustomerID: cust.ID}
	if err := db.Create(&fiber).Error; err != nil {
		t.Fatalf("fiber: %v", err)
	}

	starts := time.Now().UTC().Truncate(time.Second)
	c, rec := jsonRequest(t, http.MethodPost, "/api/maintenance", map[string]any{
		"title":       "ROADM work",
		"description": "replace amp",
		"status":      "planned",
		"starts_at":   starts,
		"resources": []map[string]any{
			{"resource_type": "device", "resource_id": devA.ID},
			{"resource_type": "device", "resource_id": devB.ID},
			{"resource_type": "connection", "resource_id": conn.ID},
			{"resource_type": "fiber", "resource_id": fiber.ID},
			{"resource_type": "wavelength", "resource_id": wl.ID},
		},
	}, nil, nil)
	c.Set("user", models.User{FactumModel: models.FactumModel{ID: 9}, Username: "ada"})
	if err := ctrl.ApiMaintenanceCreate(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.MaintenanceWindow
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == 0 || created.CreatedBy != 9 {
		t.Fatalf("created = %+v", created)
	}
	if created.ResourceType != "device" || created.ResourceID != devA.ID {
		t.Fatalf("denormalized resource = %s/%d, want device/%d", created.ResourceType, created.ResourceID, devA.ID)
	}
	if len(created.Resources) != 5 {
		t.Fatalf("resources = %d, want 5: %+v", len(created.Resources), created.Resources)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/maintenance", nil, nil, nil)
	if err := ctrl.ApiMaintenanceList(c); err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed []models.MaintenanceWindow
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 1 || len(listed[0].Resources) != 5 {
		t.Fatalf("list = %+v", listed)
	}

	id := strconv.FormatUint(uint64(created.ID), 10)
	c, rec = jsonRequest(t, http.MethodGet, "/api/maintenance/"+id, nil, []string{"id"}, []string{id})
	if err := ctrl.ApiMaintenanceGet(c); err != nil {
		t.Fatalf("get: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	resources, ok := got["resources"].([]any)
	if !ok || len(resources) != 5 {
		t.Fatalf("get resources = %#v", got["resources"])
	}
	impact, _ := got["impact"].([]any)
	if len(impact) != 2 {
		t.Fatalf("impact = %#v, want VL + LF", got["impact"])
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/maintenance", map[string]any{
		"title":         "legacy",
		"resource_type": "device",
		"resource_id":   devA.ID,
		"starts_at":     starts.Add(time.Hour),
		"status":        "planned",
	}, nil, nil)
	if err := ctrl.ApiMaintenanceCreate(c); err != nil {
		t.Fatalf("legacy create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("legacy create status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/maintenance", map[string]any{
		"title":     "empty",
		"starts_at": starts,
		"status":    "planned",
	}, nil, nil)
	if err := ctrl.ApiMaintenanceCreate(c); err != nil {
		t.Fatalf("empty create: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty create status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
