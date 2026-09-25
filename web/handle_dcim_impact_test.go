package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
)

func TestApiDeviceImpactByName(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe1.example.com", Status: "active"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	svc := models.Service{ServiceID: "CN00001", CustomerID: cust.ID, Name: "Acme line"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	ep := models.ServiceEndpoint{ServiceID: svc.ID, Role: models.EndpointRoleInterface, DeviceID: dev.ID, InterfaceID: 1}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/device/name/pe1.example.com/impact", nil, []string{"name"}, []string{"pe1.example.com"})
	if err := ctrl.ApiDeviceImpactByName(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var out optical.DeviceImpact
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.DeviceID != dev.ID || out.CustomerCount != 1 || out.ServiceCount != 1 {
		t.Fatalf("impact %+v", out)
	}
	if out.Services[0].Customer != "Acme" || out.Services[0].ServiceRef != "CN00001" {
		t.Fatalf("row %+v", out.Services[0])
	}

	missing, missRec := jsonRequest(t, http.MethodGet, "/api/device/name/nope/impact", nil, []string{"name"}, []string{"nope"})
	if err := ctrl.ApiDeviceImpactByName(missing); err != nil {
		t.Fatal(err)
	}
	if missRec.Code != http.StatusNotFound {
		t.Fatalf("missing status %d, want 404", missRec.Code)
	}
}
