package netbox

import (
	"errors"
	"testing"

	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
)

type fakeInterfaceTypes struct {
	choices []netboxtool.InterfaceTypeChoice
	err     error
}

func (f fakeInterfaceTypes) GetInterfaceTypeChoices() ([]netboxtool.InterfaceTypeChoice, error) {
	return f.choices, f.err
}

func TestUpsertInterfaceTypesAdoptsFactumValue(t *testing.T) {
	db := newImportTestDB(t)
	local := models.InterfaceType{Value: "1000base-t", Label: "Gigabit", Source: "factum", SortOrder: 9}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	n, err := upsertInterfaceTypes(db, []netboxtool.InterfaceTypeChoice{
		{Value: "virtual", Label: "Virtual"},
		{Value: "1000base-t", Label: "1000BASE-T (1GE)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	var row models.InterfaceType
	if err := db.Where("value = ?", "1000base-t").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ID != local.ID {
		t.Fatalf("id = %d, want adopted %d", row.ID, local.ID)
	}
	if row.Source != "netbox" || row.Label != "1000BASE-T (1GE)" || row.SortOrder != 1 {
		t.Fatalf("row = %+v", row)
	}
}

func TestUpsertInterfaceTypesKeepsLocalAndUsedNetbox(t *testing.T) {
	db := newImportTestDB(t)
	keepLocal := models.InterfaceType{Value: "custom-foo", Label: "Custom", Source: "factum"}
	goneUnused := models.InterfaceType{Value: "gone", Label: "Gone", Source: "netbox"}
	goneUsed := models.InterfaceType{Value: "still-on-port", Label: "Still", Source: "netbox"}
	if err := db.Create(&keepLocal).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&goneUnused).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&goneUsed).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "sw1"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Interface{DeviceID: dev.ID, Name: "Eth1", Type: "still-on-port"}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := upsertInterfaceTypes(db, []netboxtool.InterfaceTypeChoice{
		{Value: "virtual", Label: "Virtual"},
	}); err != nil {
		t.Fatal(err)
	}

	var n int64
	db.Model(&models.InterfaceType{}).Where("value = ?", "custom-foo").Count(&n)
	if n != 1 {
		t.Fatalf("local custom-foo missing")
	}
	db.Model(&models.InterfaceType{}).Where("value = ?", "gone").Count(&n)
	if n != 0 {
		t.Fatalf("unused netbox type still present")
	}
	db.Model(&models.InterfaceType{}).Where("value = ?", "still-on-port").Count(&n)
	if n != 1 {
		t.Fatalf("in-use netbox type deleted")
	}
}

func TestSyncInterfaceTypesEmptyOrErrorIsNoop(t *testing.T) {
	db := newImportTestDB(t)
	row := models.InterfaceType{Value: "virtual", Label: "Virtual", Source: "netbox"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := syncInterfaceTypes(db, fakeInterfaceTypes{err: errors.New("boom")}, nil); err != nil {
		t.Fatal(err)
	}
	if err := syncInterfaceTypes(db, fakeInterfaceTypes{}, nil); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.InterfaceType{}).Count(&n)
	if n != 1 {
		t.Fatalf("n = %d, want catalog unchanged", n)
	}
}
