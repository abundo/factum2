package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestValidateBranding(t *testing.T) {
	if err := validateBranding("", ""); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := validateBranding("data:image/png;base64,AAAA", "Acme"); err != nil {
		t.Fatalf("data URL: %v", err)
	}
	if err := validateBranding("https://example.com/logo.png", ""); err != nil {
		t.Fatalf("https URL: %v", err)
	}
	if err := validateBranding("javascript:alert(1)", "x"); err == nil {
		t.Fatal("expected error for javascript URL")
	}
	if err := validateBranding("", strings.Repeat("a", maxBrandTextLen+1)); err == nil {
		t.Fatal("expected error for long text")
	}
	if err := validateBranding(strings.Repeat("x", maxBrandLogoBytes+1), ""); err == nil {
		t.Fatal("expected error for large logo")
	}
}

func TestApiBranding(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/branding", nil, nil, nil)
	if err := ctrl.ApiBranding(c); err != nil {
		t.Fatalf("ApiBranding: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["logo"] != "" || got["text"] != "" {
		t.Fatalf("empty branding: %+v", got)
	}

	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.BrandLogo = "data:image/png;base64,AAAA"
	settings.BrandText = "Acme Energy"
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/branding", nil, nil, nil)
	if err := ctrl.ApiBranding(c); err != nil {
		t.Fatalf("ApiBranding: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["logo"] != "data:image/png;base64,AAAA" {
		t.Errorf("logo = %v", got["logo"])
	}
	if got["text"] != "Acme Energy" {
		t.Errorf("text = %v", got["text"])
	}
}

func TestApiSettingsUpdateRejectsBadBranding(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.BrandLogo = "javascript:alert(1)"
	c, rec := jsonRequest(t, http.MethodPut, "/api/admin/settings", settings, nil, nil)
	if err := ctrl.ApiSettingsUpdate(c); err != nil {
		t.Fatalf("ApiSettingsUpdate: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
