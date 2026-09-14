package netbox

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
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
