package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

func TestRequireStorageEnabled_OffIs404(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/software/files", nil, nil, nil)
	called := false
	err := ctrl.RequireStorageEnabled(func(c *echo.Context) error {
		called = true
		return c.JSON(http.StatusOK, map[string]any{"ok": true})
	})(c)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if called {
		t.Fatal("next ran while storage is disabled")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiStorageConfig(t *testing.T) {
	db := newTestDB(t)
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	s.StorageRoot = "/data/images"
	s.StorageHTTPURL = "http://10.0.0.5:8088"
	if err := db.Save(s).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/storage-config", nil, nil, nil)
	if err := ctrl.ApiStorageConfig(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["root"] != "/data/images" {
		t.Fatalf("root %v", got["root"])
	}
	if got["http_url"] != "http://10.0.0.5:8088" {
		t.Fatalf("http_url %v", got["http_url"])
	}
}
