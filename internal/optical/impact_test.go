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
	if err := db.AutoMigrate(&models.Device{}, &models.Customer{}, &models.Service{}, &models.ServiceEndpoint{}, &models.ServiceHop{}, &models.ServicePath{}); err != nil {
		t.Fatal(err)
	}
	cust := models.Customer{Name: "Acme"}
	db.Create(&cust)
	dev := models.Device{Name: "pe1", Status: "offline"}
	db.Create(&dev)
	svc := models.Service{ServiceID: "CN00001", CustomerID: cust.ID, ServiceType: "ELINE"}
	db.Create(&svc)
	db.Create(&models.ServiceEndpoint{ServiceID: svc.ID, Role: "a", DeviceID: dev.ID, InterfaceID: 1})

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

func TestResourceImpactWavelengthAndFiber(t *testing.T) {
	db := impactTestDB(t, "impact-svc")
	cust := models.Customer{Name: "Acme"}
	db.Create(&cust)
	wl := models.Service{ServiceID: "VL00001", CustomerID: cust.ID}
	db.Create(&wl)
	fiber := models.Service{ServiceID: "LF00001", CustomerID: cust.ID}
	db.Create(&fiber)
	cn := models.Service{ServiceID: "CN00001", CustomerID: cust.ID, ServiceType: "ELINE"}
	db.Create(&cn)

	rows, err := ResourceImpact(db, models.MaintResourceWavelength, wl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ServiceRef != "VL00001" || rows[0].Source != "service" {
		t.Fatalf("wavelength impact = %+v", rows)
	}

	rows, err = ResourceImpact(db, models.MaintResourceFiber, fiber.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ServiceRef != "LF00001" {
		t.Fatalf("fiber impact = %+v", rows)
	}

	rows, err = ResourceImpact(db, models.MaintResourceWavelength, cn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("CN service should be excluded, got %+v", rows)
	}
}

func TestResourcesImpactUnionsAndDedupes(t *testing.T) {
	db := impactTestDB(t, "impact-union")
	cust := models.Customer{Name: "Acme"}
	db.Create(&cust)
	dev := models.Device{Name: "roadm1"}
	db.Create(&dev)
	wl := models.Service{ServiceID: "VL00002", CustomerID: cust.ID}
	db.Create(&wl)
	db.Create(&models.ServicePath{ServiceID: wl.ID, Mode: models.TraceModeWDM, Status: models.PathComplete, EndpointAInterfaceID: 1, EndpointZInterfaceID: 2})
	db.Create(&models.ServiceHop{ServiceID: wl.ID, Seq: 1, Kind: models.HopDevice, DeviceID: &dev.ID})

	rows, err := ResourcesImpact(db, []models.MaintenanceResource{
		{ResourceType: models.MaintResourceDevice, ResourceID: dev.ID},
		{ResourceType: models.MaintResourceWavelength, ResourceID: wl.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("union size = %d, want 1 (same service via device hop and wavelength)", len(rows))
	}
	if rows[0].ServiceRef != "VL00002" {
		t.Fatalf("service = %s, want VL00002", rows[0].ServiceRef)
	}
}

func impactTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Device{}, &models.Customer{}, &models.Service{},
		&models.ServiceEndpoint{}, &models.ServiceHop{}, &models.ServicePath{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}
