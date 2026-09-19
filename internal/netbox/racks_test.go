package netbox

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestResolveRackSitePrefersLocation(t *testing.T) {
	db := newImportTestDB(t)
	site := models.Site{
		Name: "STO", Source: models.SiteSourceNetbox,
		NetboxKind: models.SiteNetboxKindSite, NetboxID: 10,
	}
	loc := models.Site{
		Name: "Row A", Source: models.SiteSourceNetbox,
		NetboxKind: models.SiteNetboxKindLocation, NetboxID: 20,
	}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&loc).Error; err != nil {
		t.Fatal(err)
	}
	got := resolveRackSite(db, nbRackREST{
		Site:     nbNestedName{ID: 10, Name: "STO"},
		Location: &nbNestedName{ID: 20, Name: "Row A"},
	})
	if got != loc.ID {
		t.Fatalf("site = %d, want location %d", got, loc.ID)
	}
}

func TestDeleteMissingImportedRacksKeepsLocal(t *testing.T) {
	db := newImportTestDB(t)
	site := models.Site{Name: "Hall", Source: models.SiteSourceFactum}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	local := models.Rack{SiteID: site.ID, Name: "local", Source: models.DCIMSourceFactum}
	imported := models.Rack{SiteID: site.ID, Name: "nb", Source: models.DCIMSourceNetbox, NetboxID: 44}
	keep := models.Rack{SiteID: site.ID, Name: "keep", Source: models.DCIMSourceNetbox, NetboxID: 45}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&imported).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&keep).Error; err != nil {
		t.Fatal(err)
	}
	if err := deleteMissingImportedRacks(db, []uint{45}); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.Rack{}).Count(&n)
	if n != 2 {
		t.Fatalf("racks = %d, want 2", n)
	}
	if err := db.First(&models.Rack{}, local.ID).Error; err != nil {
		t.Fatal("local rack was deleted")
	}
}
