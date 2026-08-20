package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func TestApiContact_IncludesRelatedCompanyNames(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	alpha := models.Customer{Name: "Alpha Corp"}
	zeta := models.Customer{Name: "Zeta Ltd"}
	if err := db.Create(&alpha).Error; err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	if err := db.Create(&zeta).Error; err != nil {
		t.Fatalf("seed zeta: %v", err)
	}

	linked := models.Contact{Name: "Has one company"}
	multi := models.Contact{Name: "Has two companies"}
	unlinked := models.Contact{Name: "Has no company"}
	if err := db.Create(&linked).Error; err != nil {
		t.Fatalf("seed linked: %v", err)
	}
	if err := db.Create(&multi).Error; err != nil {
		t.Fatalf("seed multi: %v", err)
	}
	if err := db.Create(&unlinked).Error; err != nil {
		t.Fatalf("seed unlinked: %v", err)
	}
	if err := db.Create(&models.CustomerContact{CustomerID: zeta.ID, ContactID: linked.ID}).Error; err != nil {
		t.Fatalf("seed linked join: %v", err)
	}
	if err := db.Create(&models.CustomerContact{CustomerID: zeta.ID, ContactID: multi.ID}).Error; err != nil {
		t.Fatalf("seed multi join zeta: %v", err)
	}
	if err := db.Create(&models.CustomerContact{CustomerID: alpha.ID, ContactID: multi.ID}).Error; err != nil {
		t.Fatalf("seed multi join alpha: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/contact", nil, nil, nil)
	if err := ctrl.ApiContact(c); err != nil {
		t.Fatalf("ApiContact: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var items []struct {
		ID      uint   `json:"id"`
		Name    string `json:"name"`
		Company string `json:"company"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := make(map[uint]string, len(items))
	for _, item := range items {
		got[item.ID] = item.Company
	}
	if got[linked.ID] != "Zeta Ltd" {
		t.Errorf("linked company = %q, want %q", got[linked.ID], "Zeta Ltd")
	}
	if got[multi.ID] != "Alpha Corp, Zeta Ltd" {
		t.Errorf("multi company = %q, want sorted comma-separated names", got[multi.ID])
	}
	if got[unlinked.ID] != "" {
		t.Errorf("unlinked company = %q, want empty", got[unlinked.ID])
	}
}

func TestContact_BeforeCreate_DefaultsSourceToFactum(t *testing.T) {
	db := newTestDB(t)

	contact := models.Contact{Name: "manually created"}
	if err := db.Create(&contact).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if contact.Source != "factum" {
		t.Errorf("Source = %q, want %q", contact.Source, "factum")
	}
}

func TestContact_BeforeCreate_DoesNotOverrideExplicitSource(t *testing.T) {
	db := newTestDB(t)

	contact := models.Contact{Name: "synced from lime", Source: "lime", SourceID: "42"}
	if err := db.Create(&contact).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if contact.Source != "lime" {
		t.Errorf("Source = %q, want unchanged %q", contact.Source, "lime")
	}
}

func TestSecureCRUDHandler_ContactCreateDefaultsSourceToFactum(t *testing.T) {
	db := newTestDB(t)
	handler := NewSecureCRUDHandler[models.Contact, models.ContactDTO](db)

	body := map[string]any{
		"name":      "Attacker Contact",
		"source":    "lime",
		"source_id": "fake-external-id",
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/contact", body, nil, nil)
	if err := handler.Create(c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created models.Contact
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Source != "factum" || created.SourceID != "" {
		t.Errorf("Create honored client-supplied sync fields or failed to default source: source=%q source_id=%q, want source=%q source_id empty", created.Source, created.SourceID, "factum")
	}
	if created.Name != "Attacker Contact" {
		t.Errorf("Name = %q, want the legitimate field to go through", created.Name)
	}
}

func TestApiContactUpdate_LimeAllowsOnlyNotifyMaintenance(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	contacts := NewSecureCRUDHandler[models.Contact, models.ContactDTO](db)

	synced := models.Contact{Name: "synced-contact", Source: "lime", SourceID: "42", Email: "orig@example.com", NotifyMaintenance: true}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatalf("seed synced contact: %v", err)
	}

	body := map[string]any{
		"name":               "an edit that should be ignored",
		"email":              "else@example.com",
		"phone":              "000",
		"notify_maintenance": false,
		"source":             "factum",
		"source_id":          "attacker",
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/contact/x", body, []string{"id"}, []string{strconv.FormatUint(uint64(synced.ID), 10)})
	if err := ctrl.ApiContactUpdate(contacts)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var stillSynced models.Contact
	if err := json.Unmarshal(rec.Body.Bytes(), &stillSynced); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stillSynced.Name != "synced-contact" || stillSynced.Email != "orig@example.com" {
		t.Errorf("lime-owned fields mutated: name=%q email=%q", stillSynced.Name, stillSynced.Email)
	}
	if stillSynced.NotifyMaintenance {
		t.Error("NotifyMaintenance was not updated")
	}
	if stillSynced.Source != "lime" || stillSynced.SourceID != "42" {
		t.Errorf("sync fields mutated: source=%q source_id=%q", stillSynced.Source, stillSynced.SourceID)
	}
}

func TestApiContactUpdate_AllowsFactumSourcedContact(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	contacts := NewSecureCRUDHandler[models.Contact, models.ContactDTO](db)

	local := models.Contact{Name: "local-contact", Email: "orig@example.com"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("seed local contact: %v", err)
	}
	if local.Source != "factum" {
		t.Fatalf("seed Source = %q, want factum so the update path under test is the local one", local.Source)
	}

	body := map[string]any{
		"name":      "renamed local",
		"email":     "new@example.com",
		"source":    "lime",
		"source_id": "attacker-controlled",
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/contact/x", body, []string{"id"}, []string{strconv.FormatUint(uint64(local.ID), 10)})
	if err := ctrl.ApiContactUpdate(contacts)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var updated models.Contact
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Name != "renamed local" || updated.Email != "new@example.com" {
		t.Errorf("legitimate edit did not apply: name=%q email=%q", updated.Name, updated.Email)
	}
	if updated.Source != "factum" || updated.SourceID != "" {
		t.Errorf("Update let the request body overwrite sync-managed fields: source=%q source_id=%q, want unchanged factum/empty", updated.Source, updated.SourceID)
	}
}

func TestApiContactDelete_RemovesUnreferencedFactumContact(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	local := models.Contact{Name: "deletable"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("seed local contact: %v", err)
	}
	cust := models.Customer{Name: "linked-customer"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&models.CustomerContact{CustomerID: cust.ID, ContactID: local.ID}).Error; err != nil {
		t.Fatalf("seed contact link: %v", err)
	}

	window := models.MaintenanceWindow{
		Title:        "window",
		ResourceType: "device",
		ResourceID:   1,
		StartsAt:     time.Now(),
		Status:       "planned",
	}
	if err := db.Create(&window).Error; err != nil {
		t.Fatalf("seed window: %v", err)
	}
	cid := local.ID
	note := models.MaintenanceNotification{
		WindowID:   window.ID,
		CustomerID: cust.ID,
		ContactID:  &cid,
		Email:      "kept@example.com",
		Status:     "sent",
	}
	if err := db.Create(&note).Error; err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodDelete, "/api/contact/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(local.ID), 10)})
	if err := ctrl.ApiContactDelete(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	if err := db.First(&models.Contact{}, local.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("contact row still present after delete: err=%v", err)
	}
	var links int64
	if err := db.Model(&models.CustomerContact{}).Where("contact_id = ?", local.ID).Count(&links).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 0 {
		t.Errorf("customer_contacts left behind: %d", links)
	}
	if err := db.First(&models.Customer{}, cust.ID).Error; err != nil {
		t.Errorf("customer row was deleted with the contact: %v", err)
	}

	var stillNote models.MaintenanceNotification
	if err := db.First(&stillNote, note.ID).Error; err != nil {
		t.Fatalf("notification row was deleted with the contact: %v", err)
	}
	if stillNote.ContactID != nil {
		t.Errorf("notification ContactID = %v, want nil", stillNote.ContactID)
	}
	if stillNote.Email != "kept@example.com" {
		t.Errorf("notification Email = %q, want preserved", stillNote.Email)
	}
}

func TestApiContactDelete_RejectsLimeSourcedContact(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	synced := models.Contact{Name: "synced-contact", Source: "lime", SourceID: "42"}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatalf("seed synced contact: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodDelete, "/api/contact/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(synced.ID), 10)})
	if err := ctrl.ApiContactDelete(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	if err := db.First(&models.Contact{}, synced.ID).Error; err != nil {
		t.Errorf("lime contact row was deleted: %v", err)
	}
}

func TestApiContactCustomersPut_RejectsLimeSourcedContact(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	synced := models.Contact{Name: "synced-contact", Source: "lime", SourceID: "42"}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatalf("seed synced contact: %v", err)
	}
	cust := models.Customer{Name: "target"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	body := map[string]any{"customer_ids": []uint{cust.ID}}
	c, rec := jsonRequest(t, http.MethodPut, "/api/contact/x/customers", body, []string{"id"}, []string{strconv.FormatUint(uint64(synced.ID), 10)})
	if err := ctrl.ApiContactCustomersPut(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	var links int64
	if err := db.Model(&models.CustomerContact{}).Where("contact_id = ?", synced.ID).Count(&links).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 0 {
		t.Errorf("lime contact gained customer links: %d", links)
	}
}
