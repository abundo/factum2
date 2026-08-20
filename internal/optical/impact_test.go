package optical

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeviceDownImpactELINE(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:impact?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Device{}, &models.Customer{}, &models.Service{}, &models.ServiceHop{}, &models.ServicePath{}); err != nil {
		t.Fatal(err)
	}
	cust := models.Customer{Name: "Acme"}
	db.Create(&cust)
	dev := models.Device{Name: "pe1", Status: "offline"}
	db.Create(&dev)
	svc := models.Service{ServiceID: "CN00001", CustomerID: cust.ID, EndpointADeviceID: dev.ID}
	db.Create(&svc)

	out, err := DeviceDownImpact(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.ServiceCount != 1 || out.CustomerCount != 1 {
		t.Fatalf("got services=%d customers=%d, want 1/1", out.ServiceCount, out.CustomerCount)
	}
	if out.Services[0].Source != "eline" {
		t.Errorf("source=%s, want eline", out.Services[0].Source)
	}
}
