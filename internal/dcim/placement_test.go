package dcim

import (
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := util.MigrateDatabase(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func ptrInt(v int) *int { return &v }

func ptrBool(v bool) *bool { return &v }

func seedRackDevice(t *testing.T, db *gorm.DB, heightTicks int, full bool) (models.Site, models.Rack, models.Device) {
	t.Helper()
	site := models.Site{Name: "DC1", Source: models.SiteSourceFactum}
	if err := db.Create(&site).Error; err != nil {
		t.Fatalf("site: %v", err)
	}
	mfr := models.Manufacturer{Name: "Acme", Slug: "acme", Source: models.DCIMSourceFactum}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatalf("mfr: %v", err)
	}
	dt := models.DeviceType{
		ManufacturerID: mfr.ID, Model: "SW", Slug: "sw", Source: models.DCIMSourceFactum,
		HeightTicks: ptrInt(heightTicks), FullDepth: ptrBool(full),
	}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatalf("type: %v", err)
	}
	rack := models.Rack{SiteID: site.ID, Name: "R1", HeightU: 42, Source: models.DCIMSourceFactum}
	if err := db.Create(&rack).Error; err != nil {
		t.Fatalf("rack: %v", err)
	}
	dev := models.Device{Name: "sw1", DeviceTypeID: dt.ID, SiteID: site.ID, Site: site.Name, CfSource: "factum"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatalf("device: %v", err)
	}
	return site, rack, dev
}

func TestPlaceAndUnmount(t *testing.T) {
	db := newTestDB(t)
	_, rack, dev := seedRackDevice(t, db, 4, true)
	p, err := Place(db, PlaceRequest{DeviceID: dev.ID, RackID: rack.ID, OffsetTicks: 0, Face: models.DeviceFaceFront})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if p.OffsetTicks != 0 || p.Version != 1 {
		t.Fatalf("placement %+v", p)
	}
	if err := Unmount(db, dev.ID, p.Version); err != nil {
		t.Fatalf("unmount: %v", err)
	}
	var n int64
	db.Model(&models.DevicePlacement{}).Count(&n)
	if n != 0 {
		t.Fatalf("still %d placements", n)
	}
}

func TestPlaceRejectsOverlapAndVMAndUnknownHeight(t *testing.T) {
	db := newTestDB(t)
	site, rack, dev := seedRackDevice(t, db, 4, true)
	if _, err := Place(db, PlaceRequest{DeviceID: dev.ID, RackID: rack.ID, OffsetTicks: 0}); err != nil {
		t.Fatalf("first: %v", err)
	}
	dev2 := models.Device{Name: "sw2", DeviceTypeID: dev.DeviceTypeID, SiteID: site.ID, CfSource: "factum"}
	if err := db.Create(&dev2).Error; err != nil {
		t.Fatal(err)
	}
	_, err := Place(db, PlaceRequest{DeviceID: dev2.ID, RackID: rack.ID, OffsetTicks: 2})
	if !Is(err, ReasonOverlap) {
		t.Fatalf("overlap err = %v", err)
	}

	vm := models.Device{Name: "vm1", DeviceTypeID: dev.DeviceTypeID, SiteID: site.ID, VM: true, CfSource: "factum"}
	if err := db.Create(&vm).Error; err != nil {
		t.Fatal(err)
	}
	_, err = Place(db, PlaceRequest{DeviceID: vm.ID, RackID: rack.ID, OffsetTicks: 10})
	if !Is(err, ReasonInvalid) {
		t.Fatalf("vm err = %v", err)
	}

	unknown := models.DeviceType{ManufacturerID: 1, Model: "unk", Slug: "unk", Source: models.DCIMSourceFactum}
	if err := db.Create(&unknown).Error; err != nil {
		t.Fatal(err)
	}
	dev3 := models.Device{Name: "sw3", DeviceTypeID: unknown.ID, SiteID: site.ID, CfSource: "factum"}
	if err := db.Create(&dev3).Error; err != nil {
		t.Fatal(err)
	}
	_, err = Place(db, PlaceRequest{DeviceID: dev3.ID, RackID: rack.ID, OffsetTicks: 20})
	if !Is(err, ReasonUnknownDimensions) {
		t.Fatalf("unknown height err = %v", err)
	}
}

func TestPlaceRejectsImportedAndStaleVersion(t *testing.T) {
	db := newTestDB(t)
	_, rack, dev := seedRackDevice(t, db, 2, false)
	p := models.DevicePlacement{
		DeviceID: dev.ID, RackID: rack.ID, OffsetTicks: 0, Face: models.DeviceFaceFront,
		Source: models.DCIMSourceNetbox, Version: 1,
	}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	_, err := Place(db, PlaceRequest{DeviceID: dev.ID, RackID: rack.ID, OffsetTicks: 2, Version: 1})
	if !Is(err, ReasonImportedReadOnly) {
		t.Fatalf("imported err = %v", err)
	}

	dev2 := models.Device{Name: "local", DeviceTypeID: dev.DeviceTypeID, SiteID: rack.SiteID, CfSource: "factum"}
	if err := db.Create(&dev2).Error; err != nil {
		t.Fatal(err)
	}
	first, err := Place(db, PlaceRequest{DeviceID: dev2.ID, RackID: rack.ID, OffsetTicks: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Place(db, PlaceRequest{DeviceID: dev2.ID, RackID: rack.ID, OffsetTicks: 12, Version: first.Version + 5})
	if !Is(err, ReasonStaleVersion) {
		t.Fatalf("stale err = %v", err)
	}
}

func TestPlaceMoveIsAtomic(t *testing.T) {
	db := newTestDB(t)
	site, rack, dev := seedRackDevice(t, db, 2, false)
	rack2 := models.Rack{SiteID: site.ID, Name: "R2", HeightU: 42, Source: models.DCIMSourceFactum}
	if err := db.Create(&rack2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Place(db, PlaceRequest{DeviceID: dev.ID, RackID: rack.ID, OffsetTicks: 0}); err != nil {
		t.Fatal(err)
	}
	p, err := Place(db, PlaceRequest{DeviceID: dev.ID, RackID: rack2.ID, OffsetTicks: 4, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.RackID != rack2.ID || p.OffsetTicks != 4 {
		t.Fatalf("moved %+v", p)
	}
	var n int64
	db.Model(&models.DevicePlacement{}).Where("device_id = ?", dev.ID).Count(&n)
	if n != 1 {
		t.Fatalf("want 1 placement, got %d", n)
	}
}

func TestShallowDevicesShareUOnOppositeFaces(t *testing.T) {
	db := newTestDB(t)
	site, rack, dev := seedRackDevice(t, db, 2, false)
	if _, err := Place(db, PlaceRequest{DeviceID: dev.ID, RackID: rack.ID, OffsetTicks: 0, Face: models.DeviceFaceFront}); err != nil {
		t.Fatal(err)
	}
	dev2 := models.Device{Name: "rear", DeviceTypeID: dev.DeviceTypeID, SiteID: site.ID, CfSource: "factum"}
	if err := db.Create(&dev2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Place(db, PlaceRequest{DeviceID: dev2.ID, RackID: rack.ID, OffsetTicks: 0, Face: models.DeviceFaceRear}); err != nil {
		t.Fatalf("rear: %v", err)
	}
}

func TestValidateRackHeight(t *testing.T) {
	db := newTestDB(t)
	_, rack, dev := seedRackDevice(t, db, 4, true)
	if _, err := Place(db, PlaceRequest{DeviceID: dev.ID, RackID: rack.ID, OffsetTicks: 80}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRackHeight(db, rack, 40); err == nil || !Is(err, ReasonOutOfBounds) {
		t.Fatalf("shrink err = %v", err)
	}
	if err := ValidateRackHeight(db, rack, 42); err != nil {
		t.Fatalf("same height: %v", err)
	}
}

func TestLocalSiteMappingDoesNotTreatNetboxIDsAsLocal(t *testing.T) {
	db := newTestDB(t)
	local := models.Site{Name: "Local", Source: models.SiteSourceFactum}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	nb := models.Site{
		Name: "NB", Source: models.SiteSourceNetbox,
		NetboxKind: models.SiteNetboxKindSite, NetboxID: 99,
	}
	if err := db.Create(&nb).Error; err != nil {
		t.Fatal(err)
	}
	imported := models.Device{Name: "nb-sw", NetboxID: 5, SiteID: 99, Site: "NB", CfSource: "netbox"}
	if err := db.Create(&imported).Error; err != nil {
		t.Fatal(err)
	}
	ids, err := LocalSiteIDsForDevice(db, imported)
	if err != nil {
		t.Fatal(err)
	}
	foundNB := false
	foundLocal := false
	for _, id := range ids {
		if id == nb.ID {
			foundNB = true
		}
		if id == local.ID {
			foundLocal = true
		}
	}
	if !foundNB {
		t.Fatalf("imported device should map to local NetBox site row, got %v", ids)
	}
	if foundLocal {
		t.Fatalf("imported Device.SiteID=99 must not join local site id 99 by coincidence, got %v", ids)
	}
}
