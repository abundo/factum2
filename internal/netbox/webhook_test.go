package netbox

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

func TestApplyCable_UpsertAndDelete(t *testing.T) {
	db := newImportTestDB(t)
	devA := models.Device{Name: "a", NetboxID: 1, CfSource: "netbox"}
	devB := models.Device{Name: "b", NetboxID: 2, CfSource: "netbox"}
	if err := db.Create(&devA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&devB).Error; err != nil {
		t.Fatal(err)
	}
	ifa := models.Interface{DeviceID: devA.ID, NetboxID: 11, Name: "eth0"}
	ifb := models.Interface{DeviceID: devB.ID, NetboxID: 22, Name: "eth0"}
	if err := db.Create(&ifa).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ifb).Error; err != nil {
		t.Fatal(err)
	}

	created, updated, deleted, skipped, err := ApplyCable(db, 9, &netboxtool.NBCable{
		NetboxID: 9, AInterface: 11, BInterface: 22, Label: "uplink",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created != 1 || updated != 0 || deleted != 0 || skipped != 0 {
		t.Fatalf("create counts = %d/%d/%d/%d, want 1/0/0/0", created, updated, deleted, skipped)
	}

	created, updated, deleted, skipped, err = ApplyCable(db, 9, &netboxtool.NBCable{
		NetboxID: 9, AInterface: 11, BInterface: 22, Label: "renamed",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if created != 0 || updated != 1 {
		t.Fatalf("update counts = %d/%d, want 0/1", created, updated)
	}
	var conn models.Connection
	if err := db.Where("netbox_id = ?", 9).First(&conn).Error; err != nil {
		t.Fatal(err)
	}
	if conn.Label != "renamed" {
		t.Errorf("label = %q, want renamed", conn.Label)
	}

	created, updated, deleted, skipped, err = ApplyCable(db, 9, nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("delete count = %d, want 1", deleted)
	}
	if err := db.Where("netbox_id = ?", 9).First(&conn).Error; err == nil {
		t.Fatal("connection still present after delete")
	}
}

func TestApplyCable_UnresolvedEndpointsSkipped(t *testing.T) {
	db := newImportTestDB(t)
	created, updated, deleted, skipped, err := ApplyCable(db, 9, &netboxtool.NBCable{
		NetboxID: 9, AInterface: 11, BInterface: 22,
	})
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || created != 0 || updated != 0 || deleted != 0 {
		t.Fatalf("counts = %d/%d/%d/%d, want 0/0/0/1", created, updated, deleted, skipped)
	}
}

func TestApplySite_UpsertAndDelete(t *testing.T) {
	db := newImportTestDB(t)
	lat, lng := netboxtool.NBDecimal(59.3), netboxtool.NBDecimal(18.0)
	created, updated, deleted, err := ApplySite(db, 4, &netboxtool.NetboxSite{
		ID: 4, Name: "STO", Latitude: &lat, Longitude: &lng,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created != 1 || updated != 0 || deleted != 0 {
		t.Fatalf("create counts = %d/%d/%d, want 1/0/0", created, updated, deleted)
	}

	lat = 59.4
	created, updated, deleted, err = ApplySite(db, 4, &netboxtool.NetboxSite{
		ID: 4, Name: "Stockholm", Latitude: &lat, Longitude: &lng,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	var site models.Site
	if err := db.Where("netbox_id = ?", 4).First(&site).Error; err != nil {
		t.Fatal(err)
	}
	if site.Name != "Stockholm" {
		t.Errorf("name = %q, want Stockholm", site.Name)
	}

	created, updated, deleted, err = ApplySite(db, 4, nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

func TestDeleteSiteByNetboxID_NoopIfMissing(t *testing.T) {
	db := newImportTestDB(t)
	n, err := DeleteSiteByNetboxID(db, 99)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
}
