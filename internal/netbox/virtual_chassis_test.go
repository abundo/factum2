package netbox

import (
	"testing"
	"time"

	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
)

func roadmMember(netboxID uint, name string, position, priority int, master bool) *netboxtool.NBDevice {
	masterID := uint(10)
	if master {
		masterID = netboxID
	}
	return &netboxtool.NBDevice{
		NetboxID:               netboxID,
		Name:                   name,
		Status:                 "active",
		VirtualChassisID:       7,
		VirtualChassisName:     "roadm-a",
		VirtualChassisDomain:   "wdm",
		VirtualChassisMasterID: masterID,
		VCPosition:             position,
		VCPriority:             priority,
	}
}

func TestSyncDeviceVirtualChassisControllerAndDegrees(t *testing.T) {
	db := newImportTestDB(t)

	// Degree first, before the shelf controller exists.
	if _, err := syncDevice(db, roadmMember(11, "roadm-a-deg1", 2, 0, false), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := syncDevice(db, roadmMember(10, "roadm-a-ctrl", 1, 255, true), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := syncDevice(db, roadmMember(12, "roadm-a-deg2", 3, 0, false), nil); err != nil {
		t.Fatal(err)
	}
	if err := finishVirtualChassis(db); err != nil {
		t.Fatal(err)
	}

	var chassis models.VirtualChassis
	if err := db.Where("netbox_id = ?", 7).First(&chassis).Error; err != nil {
		t.Fatal(err)
	}
	if chassis.Name != "roadm-a" || chassis.Domain != "wdm" || chassis.MasterNetboxID != 10 {
		t.Fatalf("chassis = %+v", chassis)
	}

	var controller models.Device
	if err := db.Where("netbox_id = ?", 10).First(&controller).Error; err != nil {
		t.Fatal(err)
	}
	if chassis.MasterDeviceID != controller.ID {
		t.Fatalf("master device id = %d, want %d", chassis.MasterDeviceID, controller.ID)
	}
	if controller.VirtualChassisID != chassis.ID || controller.VCPosition != 1 || controller.VCPriority != 255 {
		t.Fatalf("controller membership = id %d position %d priority %d", controller.VirtualChassisID, controller.VCPosition, controller.VCPriority)
	}

	var degree models.Device
	if err := db.Where("netbox_id = ?", 11).First(&degree).Error; err != nil {
		t.Fatal(err)
	}
	if degree.VirtualChassisID != chassis.ID || degree.VCPosition != 2 {
		t.Fatalf("degree membership = id %d position %d", degree.VirtualChassisID, degree.VCPosition)
	}
	if degree.ID == chassis.MasterDeviceID {
		t.Fatal("degree must not be the shelf controller")
	}

	// Leaving the chassis clears the device. The chassis stays while the controller remains.
	if _, err := syncDevice(db, &netboxtool.NBDevice{NetboxID: 11, Name: "roadm-a-deg1", Status: "active"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := finishVirtualChassis(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("netbox_id = ?", 11).First(&degree).Error; err != nil {
		t.Fatal(err)
	}
	if degree.VirtualChassisID != 0 || degree.VCPosition != 0 || degree.VCPriority != 0 {
		t.Fatalf("degree still on chassis: %+v", degree)
	}
	if err := db.Where("netbox_id = ?", 7).First(&chassis).Error; err != nil {
		t.Fatal(err)
	}

	// A rename on a later member updates the chassis. Removing the last members deletes it.
	renamed := roadmMember(10, "roadm-a-ctrl", 1, 255, true)
	renamed.VirtualChassisName = "roadm-b"
	if _, err := syncDevice(db, renamed, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := syncDevice(db, &netboxtool.NBDevice{NetboxID: 12, Name: "roadm-a-deg2", Status: "active"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := syncDevice(db, &netboxtool.NBDevice{NetboxID: 10, Name: "roadm-a-ctrl", Status: "active"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := finishVirtualChassis(db); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.VirtualChassis{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("virtual chassis rows = %d, want 0", n)
	}
}

func TestDeleteVirtualChassisByNetboxID(t *testing.T) {
	db := newImportTestDB(t)
	if _, err := syncDevice(db, roadmMember(10, "roadm-a-ctrl", 1, 1, true), nil); err != nil {
		t.Fatal(err)
	}
	if err := finishVirtualChassis(db); err != nil {
		t.Fatal(err)
	}
	if err := deleteVirtualChassisByNetboxID(db, 7); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.VirtualChassis{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows = %d", n)
	}
	var device models.Device
	if err := db.Where("netbox_id = ?", 10).First(&device).Error; err != nil {
		t.Fatal(err)
	}
	if device.VirtualChassisID != 0 || device.VCPosition != 0 {
		t.Fatalf("device still linked: %+v", device)
	}
	if err := deleteVirtualChassisByNetboxID(db, 7); err != nil {
		t.Fatal(err)
	}
}

func TestDeltaPlanVirtualChassis(t *testing.T) {
	t0 := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	plan, err := newDeltaPlan([]netboxtool.ObjectChange{
		{ID: 1, Time: t0, Action: "update", ChangedType: "dcim.virtualchassis", ChangedID: 9},
		{ID: 2, Time: t0.Add(time.Second), Action: "delete", ChangedType: "dcim.virtualchassis", ChangedID: 4},
		{ID: 3, Time: t0.Add(2 * time.Second), Action: "create", ChangedType: "dcim.virtualchassis", ChangedID: 4},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.virtualChassis[9] != "update" {
		t.Fatalf("chassis 9 = %q", plan.virtualChassis[9])
	}
	if plan.virtualChassis[4] != "create" {
		t.Fatalf("chassis 4 = %q, want last action create", plan.virtualChassis[4])
	}
}
