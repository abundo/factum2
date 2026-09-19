package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func TestInterfaceTypeCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/interface-types", nil, nil, nil)
	if err := ctrl.ApiGetInterfaceTypes(c); err != nil {
		t.Fatal(err)
	}
	var empty []models.InterfaceType
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty == nil {
		t.Fatal("want [] not null")
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/interface-types", models.InterfaceTypeDTO{
		Value: "custom-bar", Label: "Custom BAR",
	}, nil, nil)
	if err := ctrl.ApiCreateInterfaceType(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.InterfaceType
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Source != "factum" || created.Value != "custom-bar" {
		t.Fatalf("created = %+v", created)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/interface-types", models.InterfaceTypeDTO{
		Value: "custom-bar", Label: "dup",
	}, nil, nil)
	if err := ctrl.ApiCreateInterfaceType(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dup status = %d, want 400", rec.Code)
	}

	id := strconv.FormatUint(uint64(created.ID), 10)
	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/interface-types/"+id, models.InterfaceTypeDTO{
		Value: "custom-baz", Label: "Custom BAZ", SortOrder: 3,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiUpdateInterfaceType(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/interface-types/"+id, nil, []string{"id"}, []string{id})
	if err := ctrl.ApiDeleteInterfaceType(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInterfaceTypeRenameCascadesAndDeleteInUse(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	typ := models.InterfaceType{Value: "old-type", Label: "Old", Source: "factum"}
	if err := db.Create(&typ).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "sw1", CfSource: "factum"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Interface{DeviceID: dev.ID, Name: "Eth1", Type: "old-type"}).Error; err != nil {
		t.Fatal(err)
	}

	id := strconv.FormatUint(uint64(typ.ID), 10)
	c, rec := jsonRequest(t, http.MethodPut, "/api/dcim/interface-types/"+id, models.InterfaceTypeDTO{
		Value: "new-type", Label: "New",
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiUpdateInterfaceType(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body=%s", rec.Code, rec.Body.String())
	}
	var iface models.Interface
	if err := db.First(&iface, "name = ?", "Eth1").Error; err != nil {
		t.Fatal(err)
	}
	if iface.Type != "new-type" {
		t.Fatalf("interface type = %q, want new-type", iface.Type)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/interface-types/"+id, nil, []string{"id"}, []string{id})
	if err := ctrl.ApiDeleteInterfaceType(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in use status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
}

func TestGuardFactumCatalogInterfaceType(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	row := models.InterfaceType{Value: "virtual", Label: "Virtual", Source: "netbox"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(row.ID), 10)
	next := func(c *echo.Context) error { return c.NoContent(http.StatusOK) }
	handler := ctrl.guardFactumCatalog("interface type", next)

	c, rec := jsonRequest(t, http.MethodPut, "/api/dcim/interface-types/"+id, nil, []string{"id"}, []string{id})
	if err := handler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
}
