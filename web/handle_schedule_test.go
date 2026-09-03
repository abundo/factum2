package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func TestScheduleCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/schedules", models.JobScheduleDTO{
		Name:    " Nightly all ",
		Enabled: true,
		Target:  "all",
		Cron:    "0 2 * * *",
	}, nil, nil)
	c.Set("user", models.User{Username: "ada"})
	if err := ctrl.ApiScheduleCreate(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.JobSchedule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == 0 || created.Name != "Nightly all" || created.CreatedBy != "ada" {
		t.Fatalf("created = %+v", created)
	}
	if created.NextRunAt == nil {
		t.Fatal("enabled schedule should have NextRunAt")
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/schedules", nil, nil, nil)
	if err := ctrl.ApiScheduleList(c); err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed []models.JobSchedule
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("list len = %d, want 1", len(listed))
	}

	id := strconv.FormatUint(uint64(created.ID), 10)
	c, rec = jsonRequest(t, http.MethodPut, "/api/schedules/"+id, models.JobScheduleDTO{
		Name:    "DNS hourly",
		Enabled: false,
		Target:  "dns",
		Cron:    "0 * * * *",
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiScheduleUpdate(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated models.JobSchedule
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Name != "DNS hourly" || updated.Enabled || updated.Target != "dns" {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.NextRunAt != nil {
		t.Fatalf("disabled schedule should clear NextRunAt, got %v", updated.NextRunAt)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/schedules/"+id, nil, []string{"id"}, []string{id})
	if err := ctrl.ApiScheduleDelete(c); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/schedules", nil, nil, nil)
	if err := ctrl.ApiScheduleList(c); err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("list len after delete = %d, want 0", len(listed))
	}
}

func TestScheduleCreateHousekeeping(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/schedules", models.JobScheduleDTO{
		Name:    "Nightly trim",
		Enabled: true,
		Target:  "housekeeping",
		Cron:    "0 3 * * *",
	}, nil, nil)
	if err := ctrl.ApiScheduleCreate(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.JobSchedule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Target != "housekeeping" {
		t.Fatalf("target = %q, want housekeeping", created.Target)
	}
}

func TestScheduleCreateRejectsInvalid(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	cases := []models.JobScheduleDTO{
		{Name: "", Enabled: true, Target: "dns", Cron: "* * * * *"},
		{Name: "x", Enabled: true, Target: "nope", Cron: "* * * * *"},
		{Name: "x", Enabled: true, Target: "dns", Cron: "bad"},
	}
	for i, dto := range cases {
		c, rec := jsonRequest(t, http.MethodPost, "/api/schedules", dto, nil, nil)
		if err := ctrl.ApiScheduleCreate(c); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d status = %d, want 400, body=%s", i, rec.Code, rec.Body.String())
		}
	}
}

func TestScheduleGetNotFound(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/schedules/99", nil, []string{"id"}, []string{"99"})
	if err := ctrl.ApiScheduleGet(c); err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// booleanOraclePayloads are the space-free GORM injections that turn
// First(&row, c.Param("id")) into a 200-versus-404 boolean oracle
// (Statement.BuildCondition treats a non-numeric string as raw SQL).
func booleanOraclePayloads() (truePayload, falsePayload string) {
	return "1=(SELECT(count(*))FROM(users)WHERE(substr(password_hash,1,4))=('$2a$'))",
		"1=(SELECT(count(*))FROM(users)WHERE(substr(password_hash,1,4))=('XXXX'))"
}

// TestScheduleGet_RejectsSQLInjectionInID drives Echo v5's real router the
// same way the original report did: a true subquery used to 200 and a false
// one used to 404, leaking bcrypt prefixes (and anything else in the DB)
// one character at a time. Both payloads must now be rejected with the
// same status, and a numeric id must still 200.
func TestScheduleGet_RejectsSQLInjectionInID(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	sched := models.JobSchedule{Name: "nightly", Target: "all", Cron: "0 2 * * *"}
	if err := db.Create(&sched).Error; err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	createTestUser(t, db, "admin", "secret", true)

	e := echo.New()
	e.GET("/api/schedules/:id", ctrl.ApiScheduleGet)

	hit := func(id string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/schedules/"+id, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	truePayload, falsePayload := booleanOraclePayloads()
	trueCode := hit(truePayload)
	falseCode := hit(falsePayload)
	if trueCode == http.StatusOK && falseCode == http.StatusNotFound {
		t.Fatalf("boolean SQL-injection oracle is open: true=%d false=%d", trueCode, falseCode)
	}
	if trueCode == http.StatusOK {
		t.Fatalf("true injection payload returned 200, want reject")
	}
	if trueCode != falseCode {
		t.Fatalf("true payload status %d != false payload status %d (boolean oracle)", trueCode, falseCode)
	}
	if trueCode != http.StatusNotFound && trueCode != http.StatusBadRequest {
		t.Fatalf("injection payload status = %d, want 400 or 404", trueCode)
	}

	numeric := strconv.FormatUint(uint64(sched.ID), 10)
	if code := hit(numeric); code != http.StatusOK {
		t.Fatalf("numeric id status = %d, want 200", code)
	}
}
