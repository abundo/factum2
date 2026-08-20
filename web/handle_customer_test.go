package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func TestCustomer_BeforeCreate_DefaultsSourceToFactum(t *testing.T) {
	db := newTestDB(t)

	cust := models.Customer{Name: "manually created"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if cust.Source != "factum" {
		t.Errorf("Source = %q, want %q", cust.Source, "factum")
	}
}

func TestCustomer_BeforeCreate_DoesNotOverrideExplicitSource(t *testing.T) {
	db := newTestDB(t)

	cust := models.Customer{Name: "synced from lime", Source: "lime", SourceID: "42"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if cust.Source != "lime" {
		t.Errorf("Source = %q, want unchanged %q", cust.Source, "lime")
	}
}

func TestSecureCRUDHandler_CustomerCreateIgnoresSyncManagedFields(t *testing.T) {
	db := newTestDB(t)
	handler := NewSecureCRUDHandler[models.Customer, models.CustomerDTO](db)

	body := map[string]any{
		"name":      "Attacker Customer",
		"source":    "lime",
		"source_id": "fake-external-id",
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/customer", body, nil, nil)
	if err := handler.Create(c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created models.Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Source != "factum" || created.SourceID != "" {
		t.Errorf("Create honored client-supplied sync fields or failed to default source: source=%q source_id=%q, want source=%q source_id empty", created.Source, created.SourceID, "factum")
	}
	if created.Name != "Attacker Customer" {
		t.Errorf("Name = %q, want the legitimate field to go through", created.Name)
	}
}

func TestApiCustomerUpdate_RejectsLimeSourcedCustomer(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	customers := NewSecureCRUDHandler[models.Customer, models.CustomerDTO](db)

	synced := models.Customer{Name: "synced-customer", Source: "lime", SourceID: "42", Postalcity: "original"}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatalf("seed synced customer: %v", err)
	}

	body := map[string]any{"name": "an edit that should be rejected", "postalcity": "elsewhere"}
	c, rec := jsonRequest(t, http.MethodPut, "/api/customer/x", body, []string{"id"}, []string{strconv.FormatUint(uint64(synced.ID), 10)})
	if err := ctrl.ApiCustomerUpdate(customers)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	var stillSynced models.Customer
	if err := db.First(&stillSynced, synced.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stillSynced.Name != "synced-customer" || stillSynced.Postalcity != "original" {
		t.Errorf("row mutated after rejected update: name=%q city=%q", stillSynced.Name, stillSynced.Postalcity)
	}
}

func TestApiCustomerUpdate_AllowsFactumSourcedCustomer(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	customers := NewSecureCRUDHandler[models.Customer, models.CustomerDTO](db)

	local := models.Customer{Name: "local-customer", Postalcity: "original"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("seed local customer: %v", err)
	}
	if local.Source != "factum" {
		t.Fatalf("seed Source = %q, want factum so the update path under test is the local one", local.Source)
	}

	body := map[string]any{
		"name":       "renamed local",
		"postalcity": "Stockholm",
		"source":     "lime",
		"source_id":  "attacker-controlled",
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/customer/x", body, []string{"id"}, []string{strconv.FormatUint(uint64(local.ID), 10)})
	if err := ctrl.ApiCustomerUpdate(customers)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var updated models.Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Name != "renamed local" || updated.Postalcity != "Stockholm" {
		t.Errorf("legitimate edit did not apply: name=%q city=%q", updated.Name, updated.Postalcity)
	}
	if updated.Source != "factum" || updated.SourceID != "" {
		t.Errorf("Update let the request body overwrite sync-managed fields: source=%q source_id=%q, want unchanged factum/empty", updated.Source, updated.SourceID)
	}
}

func TestApiCustomerDelete_RemovesUnreferencedFactumCustomer(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	local := models.Customer{Name: "deletable"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("seed local customer: %v", err)
	}
	contact := models.Contact{Name: "linked"}
	if err := db.Create(&contact).Error; err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	if err := db.Create(&models.CustomerContact{CustomerID: local.ID, ContactID: contact.ID}).Error; err != nil {
		t.Fatalf("seed contact link: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodDelete, "/api/customer/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(local.ID), 10)})
	if err := ctrl.ApiCustomerDelete(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	if err := db.First(&models.Customer{}, local.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("customer row still present after delete: err=%v", err)
	}
	var links int64
	if err := db.Model(&models.CustomerContact{}).Where("customer_id = ?", local.ID).Count(&links).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 0 {
		t.Errorf("customer_contacts left behind: %d", links)
	}
	if err := db.First(&models.Contact{}, contact.ID).Error; err != nil {
		t.Errorf("contact row was deleted with the customer: %v", err)
	}
}

func TestApiCustomerDelete_RejectsCustomerWithServices(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	local := models.Customer{Name: "has-service"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("seed local customer: %v", err)
	}
	svc := models.Service{Name: "attached", CustomerID: local.ID, ServiceID: "CI00001"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("seed service: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodDelete, "/api/customer/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(local.ID), 10)})
	if err := ctrl.ApiCustomerDelete(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	if err := db.First(&models.Customer{}, local.ID).Error; err != nil {
		t.Errorf("customer row was deleted despite still having a service: %v", err)
	}
}

func TestApiCustomerDelete_RejectsLimeSourcedCustomer(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	synced := models.Customer{Name: "synced-customer", Source: "lime", SourceID: "42"}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatalf("seed synced customer: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodDelete, "/api/customer/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(synced.ID), 10)})
	if err := ctrl.ApiCustomerDelete(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	if err := db.First(&models.Customer{}, synced.ID).Error; err != nil {
		t.Errorf("lime customer row was deleted: %v", err)
	}
}
