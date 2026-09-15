package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func setupCerts(t *testing.T) *Controller {
	t.Helper()
	db := newTestDB(t)
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	on := true
	s.CertsEnabled = &on
	s.CertsDefaultKeyType = "EC256"
	if err := db.Save(s).Error; err != nil {
		t.Fatalf("enable certs: %v", err)
	}
	return &Controller{DB: db}
}

func TestRequireCertsEnabled_OffIs404(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/certs/certificates", nil, nil, nil)
	called := false
	err := ctrl.RequireCertsEnabled(func(c *echo.Context) error {
		called = true
		return c.JSON(http.StatusOK, map[string]any{"ok": true})
	})(c)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if called {
		t.Fatal("next ran while certs is disabled")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCertificateLifecycle(t *testing.T) {
	ctrl := setupCerts(t)

	c, rec := jsonRequest(t, http.MethodPost, "/api/certs/accounts", certAccountBody{
		Name: "letsencrypt", Email: "ops@example.com",
		AcceptsTermsOfService: true, KeyType: "EC256",
	}, nil, nil)
	if err := ctrl.ApiCertAccountCreate(c); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var acc models.CertAccount
	if err := json.Unmarshal(rec.Body.Bytes(), &acc); err != nil {
		t.Fatalf("decode account: %v", err)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/certs/challenges", certChallengeBody{
		Name: "ns1", RFC2136Nameserver: "192.0.2.53", RFC2136TSIGKey: "k",
	}, nil, nil)
	if err := ctrl.ApiCertChallengeCreate(c); err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create challenge status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var ch models.CertChallenge
	if err := json.Unmarshal(rec.Body.Bytes(), &ch); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	if ch.Provider != models.CertProviderRFC2136 {
		t.Fatalf("provider = %s", ch.Provider)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/certs/certificates", certificateBody{
		Name: "web", AccountID: acc.ID, ChallengeID: ch.ID,
		Domains: []string{"example.com", "*.example.com"},
	}, nil, nil)
	if err := ctrl.ApiCertificateCreate(c); err != nil {
		t.Fatalf("create cert: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create cert status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var cert models.CertificateDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cert); err != nil {
		t.Fatalf("decode cert: %v", err)
	}
	if len(cert.Domains) != 2 {
		t.Fatalf("domains = %#v", cert.Domains)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/certs-config", nil, nil, nil)
	if err := ctrl.ApiCertsConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("config status = %d, body=%s", rec.Code, rec.Body.String())
	}
}
