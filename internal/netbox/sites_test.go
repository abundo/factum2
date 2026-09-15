package netbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

type fakeDCIMTree struct {
	regions   []nbRegionREST
	sites     []nbSiteListREST
	locations []nbLocationREST
}

func (f *fakeDCIMTree) client(t *testing.T) *netboxtool.NetboxClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return nb
}

func (f *fakeDCIMTree) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	page := func(results any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "next": nil})
	}
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/dcim/regions/"):
		page(f.regions)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/dcim/sites/"):
		page(f.sites)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/dcim/locations/"):
		page(f.locations)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
	}
}

func TestSyncSites_BuildsHierarchyAndKeepsFactum(t *testing.T) {
	db := newImportTestDB(t)
	local := models.Site{Name: "Factum hall", Source: models.SiteSourceFactum}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}

	lat, lng := 59.3, 18.0
	fake := &fakeDCIMTree{
		regions: []nbRegionREST{
			{ID: 1, Name: "Sweden", Slug: "sweden"},
			{ID: 2, Name: "Stockholm county", Slug: "stockholm-county", Parent: &nbNestedID{ID: 1}},
		},
		sites: []nbSiteListREST{
			{ID: 3, Name: "Default", Slug: "default"},
			{ID: 4, Name: "STO", Slug: "sto", Region: &nbNestedID{ID: 2}, Latitude: &lat, Longitude: &lng},
		},
		locations: []nbLocationREST{
			{ID: 5, Name: "Floor 1", Slug: "floor-1", Site: nbNestedID{ID: 4}},
			{ID: 6, Name: "Room A", Slug: "room-a", Site: nbNestedID{ID: 4}, Parent: &nbNestedID{ID: 5}},
		},
	}

	if err := syncSites(db, fake.client(t), jobevent.NewSlogReporter("test", "sites")); err != nil {
		t.Fatal(err)
	}

	var rows []models.Site
	if err := db.Order("name").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	byKey := map[siteKey]models.Site{}
	byName := map[string]models.Site{}
	for _, row := range rows {
		byName[row.Name] = row
		if row.NetboxID != 0 {
			byKey[siteKey{row.NetboxKind, row.NetboxID}] = row
		}
	}
	if _, ok := byName["Default"]; ok {
		t.Fatal("placeholder Default site was imported")
	}
	if byName["Factum hall"].ID != local.ID {
		t.Fatal("factum site missing")
	}

	sweden := byKey[siteKey{models.SiteNetboxKindRegion, 1}]
	county := byKey[siteKey{models.SiteNetboxKindRegion, 2}]
	sto := byKey[siteKey{models.SiteNetboxKindSite, 4}]
	floor := byKey[siteKey{models.SiteNetboxKindLocation, 5}]
	room := byKey[siteKey{models.SiteNetboxKindLocation, 6}]
	if sweden.Name != "Sweden" || sweden.ParentID != nil {
		t.Fatalf("sweden = %+v", sweden)
	}
	if county.ParentID == nil || *county.ParentID != sweden.ID {
		t.Fatalf("county parent = %v, want %d", county.ParentID, sweden.ID)
	}
	if sto.ParentID == nil || *sto.ParentID != county.ID {
		t.Fatalf("sto parent = %v, want %d", sto.ParentID, county.ID)
	}
	if sto.Latitude != lat || sto.Longitude != lng {
		t.Fatalf("sto coords = %v,%v", sto.Latitude, sto.Longitude)
	}
	if floor.ParentID == nil || *floor.ParentID != sto.ID {
		t.Fatalf("floor parent = %v, want %d", floor.ParentID, sto.ID)
	}
	if room.ParentID == nil || *room.ParentID != floor.ID {
		t.Fatalf("room parent = %v, want %d", room.ParentID, floor.ID)
	}

	// Drop a location in NetBox; Factum child under it should be reparented, not deleted.
	if err := db.Model(&local).Update("parent_id", room.ID).Error; err != nil {
		t.Fatal(err)
	}
	fake.locations = fake.locations[:1] // Floor 1 only
	if err := syncSites(db, fake.client(t), jobevent.NewSlogReporter("test", "sites")); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&local, local.ID).Error; err != nil {
		t.Fatal(err)
	}
	if local.ParentID == nil || *local.ParentID != floor.ID {
		t.Fatalf("factum reparent = %v, want floor %d", local.ParentID, floor.ID)
	}
	var gone models.Site
	if err := db.Where("netbox_kind = ? AND netbox_id = ?", models.SiteNetboxKindLocation, 6).First(&gone).Error; err == nil {
		t.Fatal("deleted netbox location still present")
	}
}

func TestApplySite_KeepsUncoordinated(t *testing.T) {
	db := newImportTestDB(t)
	created, updated, deleted, err := ApplySite(db, 4, &netboxtool.NetboxSite{ID: 4, Name: "STO"})
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || updated != 0 || deleted != 0 {
		t.Fatalf("counts = %d/%d/%d, want 1/0/0", created, updated, deleted)
	}
	var site models.Site
	if err := db.Where("netbox_kind = ? AND netbox_id = ?", models.SiteNetboxKindSite, 4).First(&site).Error; err != nil {
		t.Fatal(err)
	}
	if site.HasCoordinates() {
		t.Fatalf("coords = %v,%v, want empty", site.Latitude, site.Longitude)
	}
}
