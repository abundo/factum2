package netbox

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/abundo/factum2/internal/netboxtool"
)

func TestSyncDeviceUpsertsCatalog(t *testing.T) {
	db := newImportTestDB(t)

	if _, err := syncDevice(db, &netboxtool.NBDevice{
		NetboxID:       10,
		Name:           "sw1",
		Manufacturer:   "Arista",
		ManufacturerID: 3,
		ModelName:      "DCS-7050",
		ModelID:        8,
		Platform:       "eos",
		PlatformID:     4,
		Status:         "active",
		CfSource:       "netbox",
	}, nil); err != nil {
		t.Fatal(err)
	}

	var mfr models.Manufacturer
	if err := db.Where("slug = ?", "arista").First(&mfr).Error; err != nil {
		t.Fatalf("manufacturer: %v", err)
	}
	if mfr.Source != "netbox" || mfr.NetboxID != 3 || mfr.Name != "Arista" {
		t.Fatalf("manufacturer = %+v", mfr)
	}
	var dt models.DeviceType
	if err := db.Where("model = ?", "DCS-7050").First(&dt).Error; err != nil {
		t.Fatalf("device type: %v", err)
	}
	if dt.ManufacturerID != mfr.ID || dt.Source != "netbox" || dt.NetboxID != 8 {
		t.Fatalf("device type = %+v", dt)
	}
	var plat models.Platform
	if err := db.Where("slug = ?", "eos").First(&plat).Error; err != nil {
		t.Fatalf("platform: %v", err)
	}
	if plat.Source != "netbox" || plat.NetboxID != 4 {
		t.Fatalf("platform = %+v", plat)
	}

	// Second sync of the same NetBox objects is idempotent.
	if _, err := syncDevice(db, &netboxtool.NBDevice{
		NetboxID:       10,
		Name:           "sw1",
		Manufacturer:   "Arista",
		ManufacturerID: 3,
		ModelName:      "DCS-7050",
		ModelID:        8,
		Platform:       "eos",
		PlatformID:     4,
		Status:         "active",
		CfSource:       "netbox",
	}, nil); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.Manufacturer{}).Count(&n)
	if n != 1 {
		t.Fatalf("manufacturers = %d, want 1", n)
	}
}

func TestUpsertManufacturerAdoptsFactumSlug(t *testing.T) {
	db := newImportTestDB(t)
	local := models.Manufacturer{Name: "Arista", Slug: "arista", Source: "factum"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	got, err := upsertManufacturer(db, "Arista", 99)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != local.ID {
		t.Fatalf("id = %d, want adopted %d", got.ID, local.ID)
	}
	if got.Source != "netbox" || got.NetboxID != 99 {
		t.Fatalf("got = %+v", got)
	}
}

func TestUpsertInterfaceTemplatesAdoptsNameAndKeepsLocal(t *testing.T) {
	db := newImportTestDB(t)
	mfr := models.Manufacturer{Name: "Arista", Slug: "arista", Source: "factum"}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatal(err)
	}
	dt := models.DeviceType{ManufacturerID: mfr.ID, Model: "DCS-7050", Slug: "dcs-7050", Source: "factum"}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatal(err)
	}
	local := models.InterfaceTemplate{DeviceTypeID: dt.ID, Name: "Ethernet1", Type: "1000base-t", Source: "factum"}
	extra := models.InterfaceTemplate{DeviceTypeID: dt.ID, Name: "Loopback0", Type: "virtual", Source: "factum"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}

	if err := upsertInterfaceTemplates(db, dt.ID, []netboxtool.NetboxInterfaceTemplate{
		{ID: 11, Name: "Ethernet1", Type: "10gbase-x-sfpp", Description: "uplink"},
		{ID: 12, Name: "Management1", Type: "1000base-t"},
	}); err != nil {
		t.Fatal(err)
	}

	var rows []models.InterfaceTemplate
	if err := db.Where("device_type_id = ?", dt.ID).Order("name").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("templates = %d, want 3 (adopted + new + local extra): %+v", len(rows), rows)
	}
	byName := map[string]models.InterfaceTemplate{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	eth := byName["Ethernet1"]
	if eth.ID != local.ID || eth.Source != "netbox" || eth.NetboxID != 11 || eth.Type != "10gbase-x-sfpp" {
		t.Fatalf("Ethernet1 = %+v, want adopted local id %d", eth, local.ID)
	}
	if byName["Loopback0"].Source != "factum" {
		t.Fatalf("Loopback0 should stay factum: %+v", byName["Loopback0"])
	}
	if byName["Management1"].Source != "netbox" || byName["Management1"].NetboxID != 12 {
		t.Fatalf("Management1 = %+v", byName["Management1"])
	}

	if err := upsertInterfaceTemplates(db, dt.ID, []netboxtool.NetboxInterfaceTemplate{
		{ID: 11, Name: "Ethernet1", Type: "10gbase-x-sfpp"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("device_type_id = ?", dt.ID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	byName = map[string]models.InterfaceTemplate{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if _, ok := byName["Management1"]; ok {
		t.Fatal("Management1 should be removed after leaving the NetBox payload")
	}
	if byName["Loopback0"].Source != "factum" {
		t.Fatal("local extra template was deleted")
	}
}

type fakeDeviceTypeSrc struct {
	detail *netboxtool.NetboxDeviceTypeDetail
	err    error
}

func (f fakeDeviceTypeSrc) GetDeviceType(string, string) (*netboxtool.NetboxDeviceTypeDetail, error) {
	return f.detail, f.err
}

func TestSyncDeviceTypeTemplates(t *testing.T) {
	db := newImportTestDB(t)
	mfr := models.Manufacturer{Name: "Arista", Slug: "arista", Source: "netbox"}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatal(err)
	}
	dt := models.DeviceType{ManufacturerID: mfr.ID, Model: "DCS-7050", Slug: "dcs-7050", Source: "netbox"}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatal(err)
	}
	src := fakeDeviceTypeSrc{detail: &netboxtool.NetboxDeviceTypeDetail{
		Interfaces: []netboxtool.NetboxInterfaceTemplate{{ID: 3, Name: "Ethernet1", Type: "1000base-t"}},
	}}
	if err := syncDeviceTypeTemplates(db, src, []*netboxtool.NBDevice{
		{Manufacturer: "Arista", ModelName: "DCS-7050"},
		{Manufacturer: "Arista", ModelName: "DCS-7050"},
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.InterfaceTemplate{}).Count(&n)
	if n != 1 {
		t.Fatalf("templates = %d, want 1 (deduped)", n)
	}
}
