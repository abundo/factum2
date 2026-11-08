package web

import (
	"context"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAttachVirtualChassisLabelsControllerAndDegrees(t *testing.T) {
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
	if err := db.AutoMigrate(&models.Device{}, &models.VirtualChassis{}); err != nil {
		t.Fatal(err)
	}

	ctrl := models.Device{Name: "roadm-a-ctrl", VCPosition: 1, VCPriority: 255}
	deg := models.Device{Name: "roadm-a-deg1", VCPosition: 2}
	if err := db.Create(&ctrl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&deg).Error; err != nil {
		t.Fatal(err)
	}
	chassis := models.VirtualChassis{
		NetboxID:       7,
		Name:           "roadm-a",
		Domain:         "wdm",
		MasterNetboxID: 10,
		MasterDeviceID: ctrl.ID,
	}
	if err := db.Create(&chassis).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Device{}).Where("id IN ?", []uint{ctrl.ID, deg.ID}).Update("virtual_chassis_id", chassis.ID).Error; err != nil {
		t.Fatal(err)
	}

	var devices []models.Device
	if err := db.Order("id").Find(&devices).Error; err != nil {
		t.Fatal(err)
	}
	if err := attachVirtualChassis(context.Background(), db, devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d", len(devices))
	}
	byName := map[string]models.Device{}
	for _, device := range devices {
		byName[device.Name] = device
	}
	controller := byName["roadm-a-ctrl"]
	if controller.VirtualChassis == nil || !controller.VirtualChassis.Master || controller.VirtualChassis.Member != models.VCMemberController {
		t.Fatalf("controller ref = %+v", controller.VirtualChassis)
	}
	if controller.VirtualChassis.Name != "roadm-a" {
		t.Fatalf("name = %q", controller.VirtualChassis.Name)
	}
	degree := byName["roadm-a-deg1"]
	if degree.VirtualChassis == nil || degree.VirtualChassis.Master || degree.VirtualChassis.Member != models.VCMemberDegree {
		t.Fatalf("degree ref = %+v", degree.VirtualChassis)
	}
	members := controller.VirtualChassis.Members
	if len(members) != 2 || !members[0].Master || members[0].Name != "roadm-a-ctrl" || members[1].Name != "roadm-a-deg1" || members[1].Position != 2 {
		t.Fatalf("members = %+v", members)
	}
}
