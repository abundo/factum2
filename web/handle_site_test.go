package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestSite_BeforeCreate_Defaults(t *testing.T) {
	db := newTestDB(t)
	local := models.Site{Name: "Lab"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	if local.Source != models.SiteSourceFactum {
		t.Errorf("source = %q, want factum", local.Source)
	}
	if local.Slug != "lab" {
		t.Errorf("slug = %q, want lab", local.Slug)
	}

	synced := models.Site{Name: "STO", NetboxID: 4}
	if err := db.Create(&synced).Error; err != nil {
		t.Fatal(err)
	}
	if synced.Source != models.SiteSourceNetbox || synced.NetboxKind != models.SiteNetboxKindSite {
		t.Errorf("synced source/kind = %q/%q", synced.Source, synced.NetboxKind)
	}
}

func TestApiSiteCreateAndTree(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/sites", models.SiteDTO{Name: "Sweden"}, nil, nil)
	if err := ctrl.ApiSiteCreate(c); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var parent models.Site
	if err := json.Unmarshal(rec.Body.Bytes(), &parent); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/sites", models.SiteDTO{
		Name: "Stockholm", ParentID: &parent.ID, Latitude: 59.3, Longitude: 18.0,
	}, nil, nil)
	if err := ctrl.ApiSiteCreate(c); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("child status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/sites/tree", nil, nil, nil)
	if err := ctrl.ApiSiteTree(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("tree status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var tree []siteTreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Title != "Sweden" {
		t.Fatalf("roots = %+v", tree)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].Title != "Stockholm" {
		t.Fatalf("children = %+v", tree[0].Children)
	}
}

func TestApiSiteUpdate_RejectsNetboxAndCycle(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	nb := models.Site{Name: "Region", Source: models.SiteSourceNetbox, NetboxKind: models.SiteNetboxKindRegion, NetboxID: 1}
	if err := db.Create(&nb).Error; err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/sites/x", models.SiteDTO{Name: "Nope"}, []string{"id"}, []string{fmtUint(nb.ID)})
	if err := ctrl.ApiSiteUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("netbox edit status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	a := models.Site{Name: "A"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	b := models.Site{Name: "B", ParentID: &a.ID}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/sites/x", models.SiteDTO{Name: "A", ParentID: &b.ID}, []string{"id"}, []string{fmtUint(a.ID)})
	if err := ctrl.ApiSiteUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cycle status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiSiteDelete_BlocksChildrenAndNetbox(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	parent := models.Site{Name: "Parent"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	child := models.Site{Name: "Child", ParentID: &parent.ID}
	if err := db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodDelete, "/api/sites/x", nil, []string{"id"}, []string{fmtUint(parent.ID)})
	if err := ctrl.ApiSiteDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("parent delete status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}

	nb := models.Site{Name: "NB", Source: models.SiteSourceNetbox, NetboxKind: models.SiteNetboxKindSite, NetboxID: 9}
	if err := db.Create(&nb).Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodDelete, "/api/sites/x", nil, []string{"id"}, []string{fmtUint(nb.ID)})
	if err := ctrl.ApiSiteDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("netbox delete status = %d, want 403", rec.Code)
	}
}

func fmtUint(id uint) string {
	b, _ := json.Marshal(id)
	return string(b)
}
