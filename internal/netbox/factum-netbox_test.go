package netbox

import (
	"testing"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func TestUpsertOpticalPortRole_CreatesAndPreservesFreq(t *testing.T) {
	db := newImportTestDB(t)
	_, ifaceIDs := seedDeviceWithIfaces(t, db, "roadm1", 1, []models.Interface{
		{NetboxID: 10, Name: "LINE-1"},
	})
	id := ifaceIDs["LINE-1"]

	if err := upsertOpticalPortRole(db, id, models.PortROADMDegree); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.OpticalPort{}).Where("interface_id = ?", id).Update("freq_hz", uint64(193100000000000)).Error; err != nil {
		t.Fatal(err)
	}
	if err := upsertOpticalPortRole(db, id, models.PortROADMAddDrop); err != nil {
		t.Fatal(err)
	}
	var port models.OpticalPort
	if err := db.Where("interface_id = ?", id).First(&port).Error; err != nil {
		t.Fatal(err)
	}
	if port.Role != models.PortROADMAddDrop {
		t.Errorf("role = %q, want %s", port.Role, models.PortROADMAddDrop)
	}
	if port.FreqHz != 193100000000000 {
		t.Errorf("freq_hz = %d, want preserved", port.FreqHz)
	}
}

func TestDeleteDeviceByNetboxID_RemovesDeviceAndChildren(t *testing.T) {
	db := newImportTestDB(t)

	deviceID, ifaceIDs := seedDeviceWithIfaces(t, db, "rtr1", 42, []models.Interface{
		{NetboxID: 100, Name: "eth0"},
		{NetboxID: 101, Name: "eth1"},
	})
	eth0ID := ifaceIDs["eth0"]
	eth1ID := ifaceIDs["eth1"]

	if err := db.Create(&models.Address{InterfaceID: eth0ID, NetboxID: 200, Address: "10.0.0.1/24"}).Error; err != nil {
		t.Fatalf("create address: %v", err)
	}
	devID := deviceID
	if err := db.Create(&models.Tag{DeviceID: &devID, NetboxID: 300, Name: "core"}).Error; err != nil {
		t.Fatalf("create device tag: %v", err)
	}
	if err := db.Create(&models.Tag{InterfaceID: &eth0ID, NetboxID: 301, Name: "uplink"}).Error; err != nil {
		t.Fatalf("create iface tag: %v", err)
	}
	if err := db.Create(&models.Connection{
		NetboxID:     400,
		DeviceAID:    deviceID,
		InterfaceAID: eth0ID,
		DeviceBID:    deviceID,
		InterfaceBID: eth1ID,
		Label:        "loop",
	}).Error; err != nil {
		t.Fatalf("create connection: %v", err)
	}

	n, err := DeleteDeviceByNetboxID(db, 42, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}

	if !gone[models.Device](t, db, deviceID) {
		t.Error("device row still present")
	}
	if !gone[models.Interface](t, db, eth0ID) || !gone[models.Interface](t, db, eth1ID) {
		t.Error("interface rows still present")
	}
	var addresses int64
	if err := db.Model(&models.Address{}).Count(&addresses).Error; err != nil {
		t.Fatalf("count addresses: %v", err)
	}
	if addresses != 0 {
		t.Errorf("addresses remaining = %d, want 0", addresses)
	}
	var tags int64
	if err := db.Model(&models.Tag{}).Count(&tags).Error; err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tags != 0 {
		t.Errorf("tags remaining = %d, want 0", tags)
	}
	var conns int64
	if err := db.Model(&models.Connection{}).Count(&conns).Error; err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if conns != 0 {
		t.Errorf("connections remaining = %d, want 0", conns)
	}
}

func TestDeleteDeviceByNetboxID_NoopIfMissing(t *testing.T) {
	db := newImportTestDB(t)

	n, err := DeleteDeviceByNetboxID(db, 99, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
}

func TestDeleteDeviceByNetboxID_LeavesNonNetboxSource(t *testing.T) {
	db := newImportTestDB(t)

	d := models.Device{Name: "manual", NetboxID: 42, CfSource: "manual"}
	if err := db.Create(&d).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}

	n, err := DeleteDeviceByNetboxID(db, 42, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
	if gone[models.Device](t, db, d.ID) {
		t.Error("non-netbox device was deleted")
	}
}

func TestDeleteDeviceByNetboxID_DoesNotCrossVMBoundary(t *testing.T) {
	db := newImportTestDB(t)

	phys := models.Device{Name: "rtr1", NetboxID: 7, VM: false, CfSource: "netbox"}
	vm := models.Device{Name: "vm1", NetboxID: 7, VM: true, CfSource: "netbox"}
	if err := db.Create(&phys).Error; err != nil {
		t.Fatalf("create physical: %v", err)
	}
	if err := db.Create(&vm).Error; err != nil {
		t.Fatalf("create vm: %v", err)
	}

	n, err := DeleteDeviceByNetboxID(db, 7, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}
	if !gone[models.Device](t, db, phys.ID) {
		t.Error("physical device still present")
	}
	if gone[models.Device](t, db, vm.ID) {
		t.Error("VM with the same netbox_id was deleted")
	}
}

func gone[T any](t *testing.T, db *gorm.DB, id uint) bool {
	t.Helper()
	var row T
	err := db.First(&row, id).Error
	if err == nil {
		return false
	}
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("lookup %T id=%d: %v", row, id, err)
	}
	return true
}
