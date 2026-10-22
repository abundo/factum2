package netbox

import (
	"encoding/json"
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

func TestNBRackRESTAcceptsChoiceWidth(t *testing.T) {
	const page = `{
		"next": null,
		"results": [{
			"id": 7,
			"name": "A1",
			"site": {"id": 1, "name": "Lab"},
			"location": null,
			"u_height": 42,
			"width": {"value": 19, "label": "19 inches"},
			"outer_width": 24,
			"outer_depth": 42,
			"outer_unit": {"value": "in", "label": "Inches"},
			"starting_unit": 1,
			"desc_units": false
		}]
	}`
	var got restListPage[nbRackREST]
	if err := json.Unmarshal([]byte(page), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 {
		t.Fatalf("results = %d", len(got.Results))
	}
	row := got.Results[0]
	if row.Width != 19 {
		t.Fatalf("width = %d, want 19", row.Width)
	}
	if string(row.OuterUnit) != "in" {
		t.Fatalf("outer unit = %q", row.OuterUnit)
	}
	w, d := rackFootprintMM(row)
	if w != 610 || d != 1067 {
		t.Fatalf("footprint = %d x %d, want 610 x 1067", w, d)
	}
}

func TestNBRackRESTLegacyWidthAndDefaultFootprint(t *testing.T) {
	raw := []byte(`{"id":1,"name":"A","site":{"id":1,"name":"S"},"u_height":42,"width":19,"starting_unit":1,"desc_units":true}`)
	var row nbRackREST
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	if row.Width != 19 || !row.DescUnits {
		t.Fatalf("width %d desc %v", row.Width, row.DescUnits)
	}
	w, d := rackFootprintMM(row)
	if w != 600 || d != 1000 {
		t.Fatalf("footprint = %d x %d, want 600 x 1000", w, d)
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
