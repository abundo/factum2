package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// jsonRequest builds an Echo context for method/path with a JSON body, wired
// up the same way Echo's router would (path params bound, Content-Type set)
// so handlers under test see exactly what they'd see in production.
func jsonRequest(t *testing.T, method, path string, body any, paramNames, paramValues []string) (*echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	pathValues := make(echo.PathValues, 0, len(paramNames))
	for i, name := range paramNames {
		pathValues = append(pathValues, echo.PathValue{Name: name, Value: paramValues[i]})
	}
	c.SetPathValues(pathValues)
	return c, rec
}

func TestSecureCRUDHandler_RoleLifecycle(t *testing.T) {
	db := newTestDB(t)
	handler := NewSecureCRUDHandler[models.Role, models.RoleDTO](db)

	// Create
	c, rec := jsonRequest(t, http.MethodPost, "/api/roles", models.RoleDTO{Name: "operator", Description: "day to day ops"}, nil, nil)
	if err := handler.Create(c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created models.Role
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("created role has no ID assigned")
	}
	if created.Name != "operator" {
		t.Errorf("created.Name = %q, want %q", created.Name, "operator")
	}

	// GetAll
	c, rec = jsonRequest(t, http.MethodGet, "/api/roles", nil, nil, nil)
	if err := handler.GetAll(c); err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	var all []models.Role
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode GetAll response: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("GetAll returned %d roles, want 1", len(all))
	}

	// GetOne
	id := created.ID
	c, rec = jsonRequest(t, http.MethodGet, "/api/roles/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(id), 10)})
	if err := handler.GetOne(c); err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("GetOne status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Update
	c, rec = jsonRequest(t, http.MethodPut, "/api/roles/x", models.RoleDTO{Name: "operator-renamed", Description: "renamed"}, []string{"id"}, []string{strconv.FormatUint(uint64(id), 10)})
	if err := handler.Update(c); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("Update status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var updated models.Role
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Name != "operator-renamed" {
		t.Errorf("updated.Name = %q, want %q", updated.Name, "operator-renamed")
	}
	if updated.ID != id {
		t.Errorf("updated.ID = %d, want unchanged %d", updated.ID, id)
	}
}

// TestSecureCRUDHandler_CreateIgnoresClientSuppliedID guards against
// mass-assignment of the primary key: every DTO carries an "id" field (its
// GET responses need it), so without deliberately stripping it on Create, a
// client could choose their own ID and collide with or overwrite an
// existing row instead of getting a fresh one from the DB.
func TestSecureCRUDHandler_CreateIgnoresClientSuppliedID(t *testing.T) {
	db := newTestDB(t)
	handler := NewSecureCRUDHandler[models.Role, models.RoleDTO](db)

	// Seed row 1 so id=1 is already taken - if Create honored the client's
	// id, this collision would surface as a create failure or, worse, a
	// silent overwrite.
	c, rec := jsonRequest(t, http.MethodPost, "/api/roles", models.RoleDTO{Name: "first"}, nil, nil)
	if err := handler.Create(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("seed create failed: err=%v status=%d body=%s", err, rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/roles", models.RoleDTO{ID: 1, Name: "attacker-chosen-id"}, nil, nil)
	if err := handler.Create(c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created models.Role
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == 1 {
		t.Error("Create honored a client-supplied id=1 instead of assigning a fresh one - primary key mass assignment")
	}
}

// TestSecureCRUDHandler_UpdateIgnoresClientSuppliedID guards against a PUT
// body's "id" field moving a row to a different primary key - the URL's :id
// must be the only thing that decides which row's key it is.
// TestSecureCRUDHandler_ContactCreateIgnoresSyncManagedFields guards
// ContactDTO's exclusion of Source/SourceID: those fields are populated by
// the Lime person-sync (internal/lime/lime.go), not something a caller
// creating a contact through the API should be able to fake. BeforeCreate
// then defaults Source to "factum". The request body is built from a raw
// map, not models.ContactDTO, since the whole point is to simulate a
// client sending fields the DTO struct doesn't even have a place for.
func TestSecureCRUDHandler_ContactCreateIgnoresSyncManagedFields(t *testing.T) {
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
}

// TestSecureCRUDHandler_ServiceUpdatePreservesSyncManagedFields guards the
// other direction: a synced service's Source/SourceID must survive an
// Update untouched even if the request body tries to overwrite them, since
// ServiceDTO has no field for them to bind into.
func TestSecureCRUDHandler_ServiceUpdatePreservesSyncManagedFields(t *testing.T) {
	db := newTestDB(t)
	handler := NewSecureCRUDHandler[models.Service, models.ServiceDTO](db)

	synced := models.Service{Name: "synced-service", Source: "lime", SourceID: "42", Comment: "original"}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatalf("seed synced service: %v", err)
	}

	body := map[string]any{
		"comment":   "updated by a user",
		"source":    "attacker-controlled",
		"source_id": "attacker-controlled-id",
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x", body, []string{"id"}, []string{strconv.FormatUint(uint64(synced.ID), 10)})
	if err := handler.Update(c); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("Update status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var updated models.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Comment != "updated by a user" {
		t.Errorf("Comment = %q, want the legitimate edit to go through", updated.Comment)
	}
	if updated.Source != "lime" || updated.SourceID != "42" {
		t.Errorf("Update let the request body overwrite sync-managed fields: source=%q source_id=%q, want unchanged \"lime\"/\"42\"", updated.Source, updated.SourceID)
	}
}

func TestSecureCRUDHandler_UpdateIgnoresClientSuppliedID(t *testing.T) {
	db := newTestDB(t)
	handler := NewSecureCRUDHandler[models.Role, models.RoleDTO](db)

	c, rec := jsonRequest(t, http.MethodPost, "/api/roles", models.RoleDTO{Name: "target"}, nil, nil)
	if err := handler.Create(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("seed create failed: err=%v status=%d", err, rec.Code)
	}
	var target models.Role
	json.Unmarshal(rec.Body.Bytes(), &target)

	c, rec = jsonRequest(t, http.MethodPut, "/api/roles/x", models.RoleDTO{ID: target.ID + 999, Name: "renamed"}, []string{"id"}, []string{strconv.FormatUint(uint64(target.ID), 10)})
	if err := handler.Update(c); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("Update status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var updated models.Role
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.ID != target.ID {
		t.Errorf("Update moved the row from id=%d to id=%d via the request body - primary key mass assignment", target.ID, updated.ID)
	}
}
