package netbox

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abundo/netboxtool"
)

func newContactTestClient(t *testing.T, handler http.HandlerFunc) *contactClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	return &contactClient{nb}
}

func TestContactClient_GetContacts(t *testing.T) {
	nb := newContactTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tenancy/contacts/" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"next": null,
			"results": [{
				"id": 4,
				"name": "Ada Lovelace",
				"email": "ada@example.com",
				"phone": "1",
				"custom_fields": {"source": "factum", "source_id": "12"}
			}]
		}`))
	})
	got, err := nb.GetContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	c := got[0]
	if c.NetboxID != 4 || c.Name != "Ada Lovelace" || c.Email != "ada@example.com" || c.Phone != "1" {
		t.Fatalf("contact = %+v", c)
	}
	if c.CfSource != "factum" || c.CfSourceID != "12" {
		t.Fatalf("CFs = %s/%s", c.CfSource, c.CfSourceID)
	}
}

func TestContactClient_CreateContact(t *testing.T) {
	nb := newContactTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/tenancy/contacts/" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["name"] != "Ada" {
			t.Fatalf("payload = %s", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7,"name":"Ada","email":"ada@example.com","phone":"","custom_fields":{"source":"factum","source_id":"3"}}`))
	})
	got, err := nb.CreateContact("Ada", map[string]any{
		"email":         "ada@example.com",
		"custom_fields": contactCustomFields("3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.NetboxID != 7 || got.CfSourceID != "3" {
		t.Fatalf("created = %+v", got)
	}
}

func TestContactClient_EnsureContactRole_CreatesWhenMissing(t *testing.T) {
	var posts int
	nb := newContactTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tenancy/contact-roles/":
			if r.URL.Query().Get("slug") != factumContactRoleSlug {
				t.Fatalf("slug = %s", r.URL.Query().Get("slug"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"next":null,"results":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/tenancy/contact-roles/":
			posts++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2,"name":"Factum","slug":"factum"}`))
		default:
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
	})
	got, err := nb.EnsureContactRole(factumContactRoleName, factumContactRoleSlug)
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 || got.NetboxID != 2 || got.Slug != "factum" {
		t.Fatalf("posts=%d role=%+v", posts, got)
	}
}

func TestContactClient_GetContactAssignments(t *testing.T) {
	nb := newContactTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tenancy/contact-assignments/" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("role_id") != "3" {
			t.Fatalf("role_id = %s", r.URL.Query().Get("role_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"next": null,
			"results": [{
				"id": 8,
				"object_type": "tenancy.tenant",
				"object_id": 50,
				"contact": {"id": 9, "name": "Ada"},
				"role": {"id": 3, "name": "Factum", "slug": "factum"}
			}]
		}`))
	})
	got, err := nb.GetContactAssignments(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	a := got[0]
	if a.NetboxID != 8 || a.ObjectType != tenantObjectType || a.ObjectID != 50 || a.ContactID != 9 || a.RoleID != 3 {
		t.Fatalf("assignment = %+v", a)
	}
}
