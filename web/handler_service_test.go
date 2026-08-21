package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestService_BeforeCreate_DefaultsSourceToFactum(t *testing.T) {
	db := newTestDB(t)

	svc := models.Service{Name: "manually created"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if svc.Source != "factum" {
		t.Errorf("Source = %q, want %q", svc.Source, "factum")
	}
}

func TestService_BeforeCreate_DoesNotOverrideExplicitSource(t *testing.T) {
	db := newTestDB(t)

	svc := models.Service{Name: "synced from lime", Source: "lime", SourceID: "42"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if svc.Source != "lime" {
		t.Errorf("Source = %q, want unchanged %q", svc.Source, "lime")
	}
}

func TestApiServiceUpdate_RejectsLimeSourcedService(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	services := NewSecureCRUDHandler[models.Service, models.ServiceDTO](db)

	synced := models.Service{Name: "synced-service", Source: "lime", SourceID: "42", Comment: "original"}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatalf("seed synced service: %v", err)
	}

	body := map[string]any{"comment": "an edit that should be rejected"}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x", body, []string{"id"}, []string{strconv.FormatUint(uint64(synced.ID), 10)})
	if err := ctrl.ApiServiceUpdate(services)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	var stillSynced models.Service
	if err := db.First(&stillSynced, synced.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stillSynced.Comment != "original" {
		t.Errorf("Comment = %q, want unchanged %q - rejected update must not touch the row", stillSynced.Comment, "original")
	}
}

func TestApiServiceCreate_AutoAssignsServiceID(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	body := map[string]any{"category": "VI"}
	c, rec := jsonRequest(t, http.MethodPost, "/api/service", body, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created models.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ServiceID != "VI00001" {
		t.Errorf("ServiceID = %q, want %q", created.ServiceID, "VI00001")
	}
}

func TestApiServiceCreate_UsesCallerSuppliedServiceID(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	body := map[string]any{"category": "VI", "service_id": "VI42"}
	c, rec := jsonRequest(t, http.MethodPost, "/api/service", body, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created models.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ServiceID != "VI42" {
		t.Errorf("ServiceID = %q, want %q - a caller-supplied number must be used as-is", created.ServiceID, "VI42")
	}
}

func TestApiServiceCreate_RejectsDuplicateServiceID(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	existing := models.Service{ServiceID: "VI42"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("seed existing service: %v", err)
	}

	body := map[string]any{"category": "VI", "service_id": "VI42"}
	c, rec := jsonRequest(t, http.MethodPost, "/api/service", body, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestApiServiceCreate_RejectsInvalidServiceType(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	body := map[string]any{"category": "CI", "service_type": "NOT-A-TYPE"}
	c, rec := jsonRequest(t, http.MethodPost, "/api/service", body, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestApiServiceCreate_AllowsExternalCustomerPrefixes(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	body := map[string]any{"category": "CN", "service_type": "ELINE"}
	c, rec := jsonRequest(t, http.MethodPost, "/api/service", body, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created models.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ServiceID != "CN00001" {
		t.Errorf("ServiceID = %q, want %q", created.ServiceID, "CN00001")
	}
}

func TestApiServiceCreate_RejectsMissingServiceTypeForExternalCustomerPrefix(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	body := map[string]any{"category": "CN"}
	c, rec := jsonRequest(t, http.MethodPost, "/api/service", body, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body=%s - CN carries a service type just like CI", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAPIServiceListIncludesAgreementStatus(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	svc := models.Service{
		CustomerID:      cust.ID,
		ServiceID:       "20-73",
		AgreementStatus: "Active",
		Source:          "lime",
		SourceID:        "1609",
	}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("seed service: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/service", nil, nil, nil)
	if err := ctrl.APIServiceList(c); err != nil {
		t.Fatalf("APIServiceList: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []ServiceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1", len(got))
	}
	if got[0].AgreementStatus != "Active" {
		t.Errorf("agreement_status = %q, want Active", got[0].AgreementStatus)
	}
	if got[0].Customer != "Acme" || got[0].ServiceID != "20-73" {
		t.Errorf("list row = %+v", got[0])
	}
}

func TestApiServiceByIDIncludesAgreementStatus(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	svc := models.Service{ServiceID: "20-73", AgreementStatus: "Active", Source: "lime", SourceID: "1609"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("seed service: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/service/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceByID(c); err != nil {
		t.Fatalf("ApiServiceByID: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["agreement_status"] != "Active" {
		t.Errorf("agreement_status = %#v, want Active", got["agreement_status"])
	}
}

func TestApiServiceUpdate_AllowsFactumSourcedService(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	services := NewSecureCRUDHandler[models.Service, models.ServiceDTO](db)

	manual := models.Service{Name: "manual-service", Comment: "original"}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatalf("seed manual service: %v", err)
	}
	if manual.Source != "factum" {
		t.Fatalf("precondition failed: seeded service Source = %q, want %q", manual.Source, "factum")
	}

	body := map[string]any{"comment": "edited", "service_id": manual.ServiceID, "company": manual.CustomerID}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x", body, []string{"id"}, []string{strconv.FormatUint(uint64(manual.ID), 10)})
	if err := ctrl.ApiServiceUpdate(services)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var updated models.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Comment != "edited" {
		t.Errorf("Comment = %q, want %q - a factum-sourced service must be editable", updated.Comment, "edited")
	}
}
