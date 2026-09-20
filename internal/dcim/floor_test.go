package dcim

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestRotatedFootprintAndOverlap(t *testing.T) {
	a := rotatedFootprint(0, 0, 600, 1000, 0)
	if a.MaxX != 600 || a.MaxY != 1000 {
		t.Fatalf("0deg %+v", a)
	}
	b := rotatedFootprint(0, 0, 600, 1000, 90)
	if b.MaxX != 1000 || b.MaxY != 600 {
		t.Fatalf("90deg %+v", b)
	}
	c := rotatedFootprint(600, 0, 600, 1000, 0)
	if footprintsOverlap(a, c) {
		t.Fatal("adjacent racks should not overlap")
	}
	d := rotatedFootprint(100, 0, 600, 1000, 0)
	if !footprintsOverlap(a, d) {
		t.Fatal("offset racks should overlap")
	}
}

func TestSnapMM(t *testing.T) {
	if got := SnapMM(750, 600); got != 600 {
		t.Fatalf("snap = %d", got)
	}
	if got := SnapMM(900, 600); got != 1200 {
		t.Fatalf("snap = %d", got)
	}
}

func TestRenameFloorPlan(t *testing.T) {
	db := newTestDB(t)
	site := models.Site{Name: "Hall", Source: models.SiteSourceFactum}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := CreateFloorPlan(db, site.ID, "Room A", 20000, 15000, 600)
	if err != nil {
		t.Fatal(err)
	}
	rev := plan.Revision
	got, err := RenameFloorPlan(db, plan.ID, "  Room B  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Room B" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Revision != rev {
		t.Fatalf("rename bumped revision %d -> %d", rev, got.Revision)
	}
	if _, err := RenameFloorPlan(db, plan.ID, "   "); err == nil {
		t.Fatal("empty name should fail")
	}
	if _, err := RenameFloorPlan(db, 0, "Nope"); err == nil {
		t.Fatal("missing plan should fail")
	}
}
