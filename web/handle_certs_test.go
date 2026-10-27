package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
		Host:    "lu1-vm15.example.com",
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
	if cert.Host != "lu1-vm15.example.com" {
		t.Fatalf("host = %q", cert.Host)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/certs-config", nil, nil, nil)
	if err := ctrl.ApiCertsConfig(c); err != nil {
		t.Fatalf("config: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("config status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/certs/certificates/"+strconv.FormatUint(uint64(cert.ID), 10), certificateBody{
		Name: "web", AccountID: acc.ID, ChallengeID: ch.ID,
		Host: "not a host", Domains: []string{"example.com"},
	}, []string{"id"}, []string{strconv.FormatUint(uint64(cert.ID), 10)})
	if err := ctrl.ApiCertificateUpdate(c); err != nil {
		t.Fatalf("update invalid host: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid host status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCertsImportLegoYAML(t *testing.T) {
	ctrl := setupCerts(t)
	body := certImportBody{
		YAML: `
accounts:
  letsencrypt:
    email: ops@example.com
    acceptsTermsOfService: true
challenges:
  ns:
    dns:
      provider: rfc2136
      resolvers: ["192.0.2.53:53"]
certificates:
  web:
    domains: ["example.com", "*.example.com"]
`,
		Env: "RFC2136_NAMESERVER=192.0.2.53:53\nRFC2136_TSIG_SECRET=c2VjcmV0\n",
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/certs/import", body, nil, nil)
	if err := ctrl.ApiCertsImport(c); err != nil {
		t.Fatalf("import: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var result certImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.AccountsCreated != 1 || result.ChallengesCreated != 1 || result.CertificatesCreated != 1 {
		t.Fatalf("result = %+v", result)
	}
	var ch models.CertChallenge
	if err := ctrl.DB.Where("name = ?", "ns").First(&ch).Error; err != nil {
		t.Fatal(err)
	}
	if ch.RFC2136TSIGSecret != "c2VjcmV0" || ch.RFC2136TTL != 120 || ch.Resolvers != "192.0.2.53:53" {
		t.Fatalf("challenge = %+v", ch)
	}

	body.Env = ""
	body.YAML = strings.Replace(body.YAML, "ops@example.com", "new@example.com", 1)
	c, rec = jsonRequest(t, http.MethodPost, "/api/certs/import", body, nil, nil)
	if err := ctrl.ApiCertsImport(c); err != nil {
		t.Fatalf("reimport: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("reimport status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.AccountsUpdated != 1 || result.CertificatesUpdated != 1 || result.AccountsCreated != 0 {
		t.Fatalf("reimport result = %+v", result)
	}
	if err := ctrl.DB.Where("name = ?", "ns").First(&ch).Error; err != nil {
		t.Fatal(err)
	}
	if ch.RFC2136TSIGSecret != "c2VjcmV0" {
		t.Fatalf("secret wiped: %+v", ch)
	}
	var acc models.CertAccount
	if err := ctrl.DB.Where("name = ?", "letsencrypt").First(&acc).Error; err != nil {
		t.Fatal(err)
	}
	if acc.Email != "new@example.com" {
		t.Fatalf("email = %s", acc.Email)
	}

	bad := certImportBody{YAML: `
accounts:
  other:
    email: other@example.com
certificates:
  extra:
    account: other
    challenge: missing
    domains: [example.com]
`}
	c, rec = jsonRequest(t, http.MethodPost, "/api/certs/import", bad, nil, nil)
	if err := ctrl.ApiCertsImport(c); err != nil {
		t.Fatalf("bad import: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var n int64
	if err := ctrl.DB.Model(&models.CertAccount{}).Where("name = ?", "other").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rolled back account count = %d", n)
	}
}

func TestValidCertHost(t *testing.T) {
	ok := []string{"", "192.0.2.10", "2001:db8::10", "[2001:db8::10]", "lu1-vm15.itn.nu", "localhost"}
	for _, h := range ok {
		if err := validCertHost(h); err != nil {
			t.Errorf("validCertHost(%q) = %v", h, err)
		}
	}
	bad := []string{"http://example.com", "1.2.3.4/24", "has space.com", "*.example.com", "a_b.example.com"}
	for _, h := range bad {
		if err := validCertHost(h); err == nil {
			t.Errorf("validCertHost(%q) = nil", h)
		}
	}
}
