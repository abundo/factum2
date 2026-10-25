package netbox

import (
	"testing"

	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
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
	var stamped models.Interface
	if err := db.First(&stamped, ifa.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stamped.CableID != 9 {
		t.Errorf("interface cable_id = %d, want 9", stamped.CableID)
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
	if err := db.First(&stamped, ifa.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stamped.CableID != 0 {
		t.Errorf("interface cable_id = %d after delete, want 0", stamped.CableID)
	}
}

func TestApplyCable_MovedCableClearsStaleCableID(t *testing.T) {
	db := newImportTestDB(t)
	devA := models.Device{Name: "pi1-r4", NetboxID: 1, CfSource: "netbox"}
	devB := models.Device{Name: "lu1-r3", NetboxID: 2, CfSource: "netbox"}
	devC := models.Device{Name: "other", NetboxID: 3, CfSource: "netbox"}
	for _, dev := range []*models.Device{&devA, &devB, &devC} {
		if err := db.Create(dev).Error; err != nil {
			t.Fatal(err)
		}
	}
	ifa := models.Interface{DeviceID: devA.ID, NetboxID: 11, Name: "Ethernet53/1", CableID: 244}
	ifb := models.Interface{DeviceID: devB.ID, NetboxID: 22, Name: "Ethernet53/1", CableID: 380}
	ifc := models.Interface{DeviceID: devC.ID, NetboxID: 33, Name: "Ethernet1", CableID: 380}
	oldB := models.Interface{DeviceID: devC.ID, NetboxID: 34, Name: "Ethernet2", CableID: 244}
	for _, iface := range []*models.Interface{&ifa, &ifb, &ifc, &oldB} {
		if err := db.Create(iface).Error; err != nil {
			t.Fatal(err)
		}
	}
	keep := models.Connection{NetboxID: 244, DeviceAID: devA.ID, InterfaceAID: ifa.ID, DeviceBID: devC.ID, InterfaceBID: oldB.ID, Label: "lldp"}
	stale := models.Connection{NetboxID: 380, DeviceAID: devB.ID, InterfaceAID: ifb.ID, DeviceBID: devC.ID, InterfaceBID: ifc.ID, Label: "lldp"}
	if err := db.Create(&keep).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}

	created, updated, deleted, skipped, err := ApplyCable(db, 244, &netboxtool.NBCable{
		NetboxID: 244, AInterface: 11, BInterface: 22, Label: "lldp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || updated != 1 || deleted != 1 || skipped != 0 {
		t.Fatalf("counts = %d/%d/%d/%d, want 0/1/1/0", created, updated, deleted, skipped)
	}
	if err := db.Where("netbox_id = ?", 380).First(&models.Connection{}).Error; err == nil {
		t.Fatal("connection 380 should have been removed when 244 took its port")
	}
	cableID := func(id uint) uint {
		t.Helper()
		var row models.Interface
		if err := db.First(&row, id).Error; err != nil {
			t.Fatalf("interface %d: %v", id, err)
		}
		return row.CableID
	}
	if cableID(ifb.ID) != 244 {
		t.Errorf("lu1-r3 cable_id = %d, want 244", cableID(ifb.ID))
	}
	if cableID(ifc.ID) != 0 {
		t.Errorf("other end of cable 380 cable_id = %d, want 0", cableID(ifc.ID))
	}
	if cableID(oldB.ID) != 0 {
		t.Errorf("previous side of cable 244 cable_id = %d, want 0", cableID(oldB.ID))
	}
}

func TestApplyCable_ReplacesLocalCableOnPort(t *testing.T) {
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
	local := models.Connection{DeviceAID: devA.ID, InterfaceAID: ifa.ID, DeviceBID: devB.ID, InterfaceBID: ifb.ID, Label: "lab"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}

	created, updated, deleted, skipped, err := ApplyCable(db, 9, &netboxtool.NBCable{
		NetboxID: 9, AInterface: 11, BInterface: 22, Label: "from-nb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || updated != 0 || deleted != 0 || skipped != 0 {
		t.Fatalf("counts = %d/%d/%d/%d, want 1/0/0/0", created, updated, deleted, skipped)
	}
	if err := db.First(&models.Connection{}, local.ID).Error; err == nil {
		t.Fatal("local cable should have been replaced")
	}
	var n int64
	if err := db.Model(&models.Connection{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("connections = %d, want 1", n)
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
