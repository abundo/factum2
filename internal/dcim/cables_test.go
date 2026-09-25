package dcim

import (
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestCreateUpdateDeleteLocalCable(t *testing.T) {
	db := newTestDB(t)
	a := models.Device{Name: "leaf-1", CfSource: "factum"}
	b := models.Device{Name: "spine-1", CfSource: "factum"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ia1 := models.Interface{DeviceID: a.ID, Name: "Ethernet1", Description: "uplink"}
	ia2 := models.Interface{DeviceID: a.ID, Name: "Ethernet2"}
	ib1 := models.Interface{DeviceID: b.ID, Name: "Ethernet1", Description: "to leaf"}
	ib2 := models.Interface{DeviceID: b.ID, Name: "Ethernet2"}
	if err := db.Create(&ia1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ia2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib2).Error; err != nil {
		t.Fatal(err)
	}

	c1, err := CreateCable(db, ia1.ID, ib1.ID, "uplink")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c1.NetboxID != 0 || c1.DeviceAID != a.ID || c1.InterfaceBID != ib1.ID {
		t.Fatalf("created = %+v", c1)
	}

	c2, err := CreateCable(db, ia2.ID, ib2.ID, "")
	if err != nil {
		t.Fatalf("second local cable: %v", err)
	}
	if c2.ID == 0 || c2.ID == c1.ID {
		t.Fatalf("want a second local row, got %+v", c2)
	}

	if _, err := CreateCable(db, ia1.ID, ib2.ID, "busy"); err == nil ||
		!strings.Contains(err.Error(), "leaf-1 Ethernet1") ||
		!strings.Contains(err.Error(), "spine-1 Ethernet2") ||
		!strings.Contains(err.Error(), "spine-1 Ethernet1") {
		t.Fatalf("want conflict naming both ends and the existing cable, got %v", err)
	}

	if _, err := CreateCable(db, ia1.ID, ia1.ID, ""); err == nil {
		t.Fatal("want error for same interface")
	}

	updated, err := UpdateCable(db, c1.ID, ia1.ID, ib1.ID, "renamed")
	if err != nil {
		t.Fatalf("update label: %v", err)
	}
	if updated.Label != "renamed" {
		t.Fatalf("label = %q", updated.Label)
	}

	moved, err := UpdateCable(db, c1.ID, ia1.ID, ib2.ID, "moved")
	if err == nil {
		t.Fatalf("move onto occupied port should fail, got %+v", moved)
	}

	if err := db.Delete(c2).Error; err != nil {
		t.Fatal(err)
	}
	moved, err = UpdateCable(db, c1.ID, ia1.ID, ib2.ID, "moved")
	if err != nil {
		t.Fatalf("retimate: %v", err)
	}
	if moved.InterfaceBID != ib2.ID {
		t.Fatalf("moved = %+v", moved)
	}

	nb := models.Connection{
		NetboxID: 77, DeviceAID: a.ID, InterfaceAID: ia2.ID, DeviceBID: b.ID, InterfaceBID: ib1.ID, Label: "nb",
	}
	if err := db.Create(&nb).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateCable(db, nb.ID, ia2.ID, ib1.ID, "renamed-nb"); err != nil {
		t.Fatalf("db update of a netbox-id row: %v", err)
	}

	if err := DeleteCable(db, c1.ID); err != nil {
		t.Fatalf("delete local: %v", err)
	}

	pair, err := ConnectionPair(db, a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pair.DeviceA.Name != "leaf-1" || pair.DeviceB.Name != "spine-1" {
		t.Fatalf("devices = %+v %+v", pair.DeviceA, pair.DeviceB)
	}
	if len(pair.DeviceA.Interfaces) != 2 || pair.DeviceA.Interfaces[0].Description != "uplink" {
		t.Fatalf("ifaces a = %+v", pair.DeviceA.Interfaces)
	}
	if len(pair.Cables) != 1 || pair.Cables[0].Source != "netbox" {
		t.Fatalf("cables = %+v", pair.Cables)
	}
	if pair.DeviceA.Interfaces[1].Connection == nil || pair.DeviceA.Interfaces[1].Connection.PeerDeviceName != "spine-1" {
		t.Fatalf("peer on Ethernet2 = %+v", pair.DeviceA.Interfaces[1].Connection)
	}
}

func TestCreateLinkedCableKeepsWebhookRow(t *testing.T) {
	db := newTestDB(t)
	a := models.Device{Name: "lu17-lab-r0.itn.nu"}
	b := models.Device{Name: "lu17-lab-r2.itn.nu"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ia := models.Interface{DeviceID: a.ID, Name: "Ethernet4"}
	ib := models.Interface{DeviceID: b.ID, Name: "Ethernet4"}
	if err := db.Create(&ia).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	wh := models.Connection{
		NetboxID: 8, DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: b.ID, InterfaceBID: ib.ID,
	}
	dup := models.Connection{
		DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: b.ID, InterfaceBID: ib.ID,
	}
	if err := db.Create(&wh).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&dup).Error; err != nil {
		t.Fatal(err)
	}

	got, err := CreateLinkedCable(db, ia.ID, ib.ID, "uplink", 8)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if got.ID != wh.ID || got.NetboxID != 8 {
		t.Fatalf("kept = %+v, want webhook id %d", got, wh.ID)
	}
	var n int64
	if err := db.Model(&models.Connection{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("connections = %d, want the netbox row only", n)
	}

	again, err := CreateLinkedCable(db, ia.ID, ib.ID, "uplink", 8)
	if err != nil || again.ID != wh.ID {
		t.Fatalf("second link = %+v %v", again, err)
	}
}

func TestConnectionPairThirdPartyCable(t *testing.T) {
	db := newTestDB(t)
	a := models.Device{Name: "a"}
	b := models.Device{Name: "b"}
	c := models.Device{Name: "c"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	ia := models.Interface{DeviceID: a.ID, Name: "Eth1"}
	ib := models.Interface{DeviceID: b.ID, Name: "Eth1"}
	ic := models.Interface{DeviceID: c.ID, Name: "Eth1"}
	if err := db.Create(&ia).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ic).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Connection{
		DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: c.ID, InterfaceBID: ic.ID, Label: "elsewhere",
	}).Error; err != nil {
		t.Fatal(err)
	}

	pair, err := ConnectionPair(db, a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Cables) != 0 {
		t.Fatalf("between-device cables = %+v", pair.Cables)
	}
	if pair.DeviceA.Interfaces[0].Connection == nil || pair.DeviceA.Interfaces[0].Connection.PeerDeviceName != "c" {
		t.Fatalf("third-party = %+v", pair.DeviceA.Interfaces[0].Connection)
	}
}
