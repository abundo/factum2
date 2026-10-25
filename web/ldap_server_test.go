package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestLdapServerCRUDHidesPassword(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/admin/ldap/servers", map[string]any{
		"name": "Lab", "server_type": "generic", "host": "ldap.example.com", "port": 389,
		"enabled": true, "tls_mode": "none", "bind_dn": "cn=admin,dc=example,dc=com",
		"bind_password": "secret", "base_dn": "dc=example,dc=com", "user_filter": "(uid=%s)",
		"allow_password_change": true,
	}, nil, nil)
	if err := ctrl.ApiLdapServerCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created LdapServerDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.BindPassword != "" || !created.BindPasswordSet || created.ID == 0 {
		t.Fatalf("create dto = %+v", created)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("password leaked: %s", rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/ldap/servers/"+strconv.FormatUint(uint64(created.ID), 10), map[string]any{
		"name": "Lab 2", "server_type": "generic", "host": "ldap.example.com", "port": 389,
		"enabled": true, "bind_dn": "cn=admin,dc=example,dc=com", "base_dn": "dc=example,dc=com",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiLdapServerUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}
	var row models.LdapServer
	if err := db.First(&row, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.BindPassword != "secret" || row.Name != "Lab 2" {
		t.Fatalf("stored = %+v", row)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/admin/ldap/servers", nil, nil, nil)
	if err := ctrl.ApiLdapServers(c); err != nil {
		t.Fatal(err)
	}
	var list []LdapServerDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "Lab 2" || list[0].Position != 1 {
		t.Fatalf("list = %+v", list)
	}
}
