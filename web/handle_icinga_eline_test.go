package web

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestListIcingaElines(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&models.Platform{}, &models.Device{}, &models.ServiceType{}, &models.Service{}, &models.ServiceEndpoint{}); err != nil {
		t.Fatal(err)
	}
	plats := []models.Platform{
		{Name: "Cisco IOS XR", Slug: "ios-xr"},
		{Name: "Arista EOS", Slug: "eos"},
	}
	if err := db.Create(&plats).Error; err != nil {
		t.Fatal(err)
	}
	devs := []models.Device{
		{Name: "xr1", Enabled: true, PrimaryIPv4: "10.0.0.1/32", Platform: "Cisco IOS XR", PlatformID: plats[0].ID, CfMonitorIcinga: true},
		{Name: "xr2", Enabled: true, PrimaryIPv4: "10.0.0.2/32", Platform: "Cisco IOS XR", PlatformID: plats[0].ID, CfMonitorIcinga: true},
		{Name: "eos1", Enabled: true, PrimaryIPv4: "10.0.0.3/32", Platform: "Arista EOS", PlatformID: plats[1].ID, CfMonitorIcinga: false},
		{Name: "down", Enabled: false, PrimaryIPv4: "10.0.0.4/32", Platform: "ios-xr", CfMonitorIcinga: true},
	}
	if err := db.Create(&devs).Error; err != nil {
		t.Fatal(err)
	}
	types := []models.ServiceType{
		{Name: "ELINE", SyncSource: models.SyncSourceELINE, NetboxType: models.NetboxTypeEVPL},
		{Name: "ELAN", SyncSource: models.SyncSourceELAN, NetboxType: models.NetboxTypeVPLS},
	}
	if err := db.Create(&types).Error; err != nil {
		t.Fatal(err)
	}
	elines := []models.Service{
		{Name: "remote", ServiceID: "CN00570", ServiceType: "ELINE", PseudowireID: 1000570},
		{Name: "local", ServiceID: "CN00571", ServiceType: "ELINE", PseudowireID: 1000571},
		{Name: "vpls", ServiceID: "CN00572", ServiceType: "ELAN", PseudowireID: 1000572},
		{Name: "unpushed", ServiceID: "CN00573", ServiceType: "ELINE", PseudowireID: 1000573},
	}
	if err := db.Create(&elines).Error; err != nil {
		t.Fatal(err)
	}
	eps := []models.ServiceEndpoint{
		{ServiceID: elines[0].ID, Role: "interface", DeviceID: devs[0].ID, InterfaceID: 1, AppliedDeviceID: devs[0].ID},
		{ServiceID: elines[0].ID, Role: "interface", DeviceID: devs[1].ID, InterfaceID: 2, AppliedDeviceID: devs[1].ID},
		{ServiceID: elines[1].ID, Role: "interface", DeviceID: devs[2].ID, InterfaceID: 3, AppliedDeviceID: devs[2].ID},
		{ServiceID: elines[1].ID, Role: "interface", DeviceID: devs[2].ID, InterfaceID: 4, AppliedDeviceID: devs[2].ID},
		{ServiceID: elines[2].ID, Role: "interface", DeviceID: devs[0].ID, InterfaceID: 5, AppliedDeviceID: devs[0].ID},
		{ServiceID: elines[3].ID, Role: "interface", DeviceID: devs[0].ID, InterfaceID: 6},
		{ServiceID: elines[0].ID, Role: "interface", DeviceID: devs[3].ID, InterfaceID: 7, AppliedDeviceID: devs[3].ID},
	}
	if err := db.Create(&eps).Error; err != nil {
		t.Fatal(err)
	}

	got, err := ListIcingaElines(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rows %#v", got)
	}
	if got[0].Device != "eos1" || !got[0].Local || got[0].Monitor || got[0].Platform != "eos" || got[0].PWID != 1000571 {
		t.Fatalf("local %#v", got[0])
	}
	if got[1].Device != "xr1" || got[1].Local || got[1].Platform != "ios-xr" || got[1].ServiceID != "CN00570" || !got[1].Monitor {
		t.Fatalf("xr1 %#v", got[1])
	}
	if got[2].Device != "xr2" || got[2].ServiceID != "CN00570" || got[2].Local {
		t.Fatalf("xr2 %#v", got[2])
	}
}
