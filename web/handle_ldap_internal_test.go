package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/ldapsrv"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestInternalDirectorySnapshot(t *testing.T) {
	db := newTestDB(t)
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	staff := models.Role{Name: "staff", Description: "ops"}
	wdm := models.Role{Name: "wdm"}
	if err := db.Create(&staff).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&wdm).Error; err != nil {
		t.Fatal(err)
	}
	noc := models.LdapOU{Name: "NOC"}
	if err := db.Create(&noc).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "ada", Name: "Ada Lovelace", Email: "ada@example.com", PasswordHash: hash, LdapOUID: &noc.ID, Roles: []*models.Role{&staff}}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.RoleMember{RoleID: wdm.ID, MemberRoleID: staff.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&staff).Update("ldap_ou_id", noc.ID).Error; err != nil {
		t.Fatal(err)
	}
	on := true
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	s.LdapInternalEnabled = &on
	s.LdapInternalBaseDN = "dc=example,dc=com"
	s.LdapInternalBindPassword = "bind-secret"
	s.RadiusUseInternalLDAP = &on
	s.RadiusEnabled = &on
	if err := db.Save(s).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/ldap-config", nil, nil, nil)
	if err := ctrl.ApiLdapConfig(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var cfg ldapsrv.Config
	mustJSON(t, rec.Body.Bytes(), &cfg)
	if !cfg.Enabled || cfg.BindPassword != "bind-secret" {
		t.Fatalf("config %+v", cfg)
	}
	var ada *ldapsrv.WireEntry
	for i := range cfg.Entries {
		if strings.HasPrefix(strings.ToLower(cfg.Entries[i].DN), "uid=ada,") {
			ada = &cfg.Entries[i]
		}
	}
	if ada == nil {
		t.Fatal("ada missing")
	}
	if !strings.Contains(strings.ToLower(ada.DN), "ou=noc,dc=example,dc=com") {
		t.Fatalf("dn = %s", ada.DN)
	}
	foundWDM := false
	for _, g := range ada.Attributes["memberof"] {
		if strings.Contains(strings.ToLower(g), "cn=wdm,") {
			foundWDM = true
		}
	}
	if !foundWDM {
		t.Fatalf("memberOf = %v", ada.Attributes["memberof"])
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/radius-config", nil, nil, nil)
	if err := ctrl.ApiRadiusConfig(c); err != nil {
		t.Fatal(err)
	}
	var radiusCfg struct {
		LDAP struct {
			Host   string `json:"Host"`
			BaseDN string `json:"BaseDN"`
			BindDN string `json:"BindDN"`
			Port   uint16 `json:"Port"`
		} `json:"ldap"`
	}
	// radius.Config uses default json names from the struct tags.
	mustJSON(t, rec.Body.Bytes(), &radiusCfg)
	if radiusCfg.LDAP.Host != "127.0.0.1" || radiusCfg.LDAP.BaseDN != "dc=example,dc=com" {
		t.Fatalf("radius ldap = %+v body %s", radiusCfg.LDAP, rec.Body.String())
	}
	if radiusCfg.LDAP.BindDN != "cn=svc-factum,dc=example,dc=com" {
		t.Fatalf("bind dn %s", radiusCfg.LDAP.BindDN)
	}
}

func TestInternalLDAPSCertificate(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	acc := models.CertAccount{Name: "letsencrypt", KeyType: "EC256"}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.CertChallenge{Name: "ns", Kind: "dns-01", Provider: "rfc2136"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	cert := models.Certificate{
		Name: "ldap", AccountID: acc.ID, ChallengeID: ch.ID,
		Domains: []models.CertificateDomain{
			{Rank: 1, Name: "ldap.example"},
			{Rank: 0, Name: "*.ldap.example"},
		},
	}
	if err := db.Create(&cert).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPut, "/api/admin/ldap/internal", map[string]any{
		"enabled": true, "tls_listen": ":636", "tls_certificate_id": cert.ID, "base_dn": "dc=example,dc=com",
	}, nil, nil)
	if err := ctrl.ApiLdapInternalUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}
	var view struct {
		TLSCertificateID *uint `json:"tls_certificate_id"`
		Certificates     []struct {
			Name    string   `json:"name"`
			Domains []string `json:"domains"`
		} `json:"certificates"`
	}
	mustJSON(t, rec.Body.Bytes(), &view)
	if view.TLSCertificateID == nil || *view.TLSCertificateID != cert.ID {
		t.Fatalf("id = %v", view.TLSCertificateID)
	}
	if len(view.Certificates) != 1 || view.Certificates[0].Name != "ldap" {
		t.Fatalf("choices = %+v", view.Certificates)
	}
	if len(view.Certificates[0].Domains) != 2 || view.Certificates[0].Domains[0] != "*.ldap.example" {
		t.Fatalf("domains = %v", view.Certificates[0].Domains)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ldap-config", nil, nil, nil)
	if err := ctrl.ApiLdapConfig(c); err != nil {
		t.Fatal(err)
	}
	var cfg ldapsrv.Config
	mustJSON(t, rec.Body.Bytes(), &cfg)
	if cfg.TLSCertificate != "ldap" || cfg.TLSDomain != "*.ldap.example" {
		t.Fatalf("snapshot cert = %q domain = %q", cfg.TLSCertificate, cfg.TLSDomain)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/ldap/internal", map[string]any{
		"enabled": true, "tls_listen": ":636", "tls_certificate_id": cert.ID + 9,
	}, nil, nil)
	if err := ctrl.ApiLdapInternalUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing cert %d %s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/ldap/internal", map[string]any{
		"enabled": true, "tls_listen": ":636", "tls_certificate_id": nil,
	}, nil, nil)
	if err := ctrl.ApiLdapInternalUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("clear %d %s", rec.Code, rec.Body.String())
	}
	c, rec = jsonRequest(t, http.MethodGet, "/api/ldap-config", nil, nil, nil)
	if err := ctrl.ApiLdapConfig(c); err != nil {
		t.Fatal(err)
	}
	cfg = ldapsrv.Config{}
	mustJSON(t, rec.Body.Bytes(), &cfg)
	if cfg.TLSCertificate != "" || cfg.TLSDomain != "" {
		t.Fatalf("cleared snapshot = %+v", cfg)
	}
}

func TestOUFolderAndNesting(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodPost, "/api/admin/ldap/ous", map[string]any{"name": "NOC"}, nil, nil)
	if err := ctrl.ApiLdapOUCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var parent struct {
		ID uint `json:"id"`
	}
	mustJSON(t, rec.Body.Bytes(), &parent)
	c, rec = jsonRequest(t, http.MethodPost, "/api/admin/ldap/ous", map[string]any{"name": "Night", "parent_id": parent.ID}, nil, nil)
	if err := ctrl.ApiLdapOUCreate(c); err != nil {
		t.Fatal(err)
	}
	var child struct {
		ID uint `json:"id"`
	}
	mustJSON(t, rec.Body.Bytes(), &child)
	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/ldap/ous/"+idOf(child.ID), map[string]any{"name": "Night", "parent_id": child.ID}, []string{"id"}, []string{idOf(child.ID)})
	if err := ctrl.ApiLdapOUUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cycle status %d %s", rec.Code, rec.Body.String())
	}

	staff := models.Role{Name: "staff"}
	wdm := models.Role{Name: "wdm"}
	if err := db.Create(&staff).Error; err != nil || db.Create(&wdm).Error != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/ldap/roles/"+idOf(wdm.ID)+"/members", map[string]any{"member_role_ids": []uint{staff.ID}}, []string{"id"}, []string{idOf(wdm.ID)})
	if err := ctrl.ApiRoleMembersUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("nest %d %s", rec.Code, rec.Body.String())
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/ldap/roles/"+idOf(staff.ID)+"/members", map[string]any{"member_role_ids": []uint{wdm.ID}}, []string{"id"}, []string{idOf(staff.ID)})
	if err := ctrl.ApiRoleMembersUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("loop status %d %s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/ldap/ous/"+idOf(child.ID), map[string]any{"name": "Night", "parent_id": nil}, []string{"id"}, []string{idOf(child.ID)})
	if err := ctrl.ApiLdapOUUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("move root %d %s", rec.Code, rec.Body.String())
	}
	var moved models.LdapOU
	if err := db.First(&moved, child.ID).Error; err != nil {
		t.Fatal(err)
	}
	if moved.ParentID != nil {
		t.Fatalf("parent = %v", *moved.ParentID)
	}
}

func TestRoleRenameRewritesGroupDN(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	s.LdapInternalBaseDN = "dc=example,dc=com"
	if err := db.Save(s).Error; err != nil {
		t.Fatal(err)
	}
	role := models.Role{Name: "staff"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	policy := models.RadiusPolicy{GroupDN: "cn=staff,ou=Roles,dc=example,dc=com"}
	if err := db.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/roles/"+idOf(role.ID), map[string]any{"name": "noc", "description": "ops"}, []string{"id"}, []string{idOf(role.ID)})
	if err := ctrl.ApiRoleUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("rename %d %s", rec.Code, rec.Body.String())
	}
	var got models.RadiusPolicy
	if err := db.First(&got, policy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.GroupDN != "cn=noc,ou=Roles,dc=example,dc=com" {
		t.Fatalf("group_dn = %s", got.GroupDN)
	}
}

func mustJSON(t *testing.T, b []byte, dest any) {
	t.Helper()
	if err := json.Unmarshal(b, dest); err != nil {
		t.Fatalf("json: %v body %s", err, b)
	}
}

func idOf(n uint) string { return strconv.FormatUint(uint64(n), 10) }
