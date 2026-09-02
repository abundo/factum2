package optical

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestApplyInventoryROADM(t *testing.T) {
	db := testGraphDB(t)
	dev := addDev(db, "roadm-a", "")
	deg1 := addIface(db, dev, "1/0/DEG1")
	deg2 := addIface(db, dev, "1/0/DEG2")
	ad1 := addIface(db, dev, "1/0/AD1")
	_ = addIface(db, dev, "1/0/FAN")

	hz := HzFromTHz(193.1)
	hzExpress := HzFromTHz(193.2)
	res, err := ApplyInventory(db, dev.ID, Inventory{
		Kind: models.OpticalKindROADM,
		Ports: []InventoryPort{
			{Name: "1/0/DEG1", Role: models.PortROADMDegree},
			{Name: "1/0/DEG2", Role: models.PortROADMDegree},
			{Name: "1/0/AD1", Role: models.PortROADMAddDrop, FreqHz: hz},
			{Name: "1/0/MISSING", Role: models.PortROADMDegree},
		},
		XConnects: []InventoryXConnect{
			{Name: "xc-1", Kind: models.XCAddDrop, PortA: "1/0/AD1", PortB: "1/0/DEG1", FreqHz: hz},
			{Name: "express", Kind: models.XCExpress, PortA: "1/0/DEG1", PortB: "1/0/DEG2", FreqHz: hzExpress},
			{Name: "wss-amp", Kind: models.XCPassthrough, PortA: "1/0/DEG1", PortB: "1/0/DEG2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != models.OpticalKindROADM {
		t.Errorf("kind = %q", res.Kind)
	}
	if res.PortsUpserted != 3 {
		t.Errorf("ports upserted = %d, skipped %v", res.PortsUpserted, res.PortsSkipped)
	}
	if len(res.PortsSkipped) != 1 || res.PortsSkipped[0] != "1/0/MISSING: no matching interface" {
		t.Errorf("skipped ports = %v", res.PortsSkipped)
	}
	if res.XConnectsUpserted != 2 {
		t.Errorf("xconnects upserted = %d skipped %v", res.XConnectsUpserted, res.XConnectsSkipped)
	}

	var d models.Device
	if err := db.First(&d, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if d.OpticalKind != models.OpticalKindROADM {
		t.Errorf("stored kind = %q", d.OpticalKind)
	}

	var adPort models.OpticalPort
	if err := db.Where("interface_id = ?", ad1.ID).First(&adPort).Error; err != nil {
		t.Fatal(err)
	}
	if adPort.Role != models.PortROADMAddDrop || adPort.FreqHz != hz {
		t.Errorf("add/drop port = %+v", adPort)
	}

	var xcs []models.OpticalXConnect
	if err := db.Where("device_id = ?", dev.ID).Find(&xcs).Error; err != nil {
		t.Fatal(err)
	}
	if len(xcs) != 2 {
		t.Fatalf("xconnects = %+v", xcs)
	}
	kinds := map[string]models.OpticalXConnect{}
	for _, x := range xcs {
		kinds[x.Kind] = x
		if x.Source != models.XCSourceOpenROADM {
			t.Errorf("source = %q", x.Source)
		}
	}
	ad := kinds[models.XCAddDrop]
	if ad.InterfaceAID != ad1.ID || ad.InterfaceBID != deg1.ID || ad.FreqHz != hz {
		t.Errorf("add/drop xc = %+v want a=%d b=%d", ad, ad1.ID, deg1.ID)
	}
	ex := kinds[models.XCExpress]
	if (ex.InterfaceAID != deg1.ID || ex.InterfaceBID != deg2.ID) &&
		(ex.InterfaceAID != deg2.ID || ex.InterfaceBID != deg1.ID) {
		t.Errorf("express xc = %+v", ex)
	}

	// Re-apply without express: driver-sourced express is deleted; a
	// manual xconnect is not.
	manual := models.OpticalXConnect{
		DeviceID: dev.ID, Kind: models.XCExpress,
		InterfaceAID: deg1.ID, InterfaceBID: deg2.ID,
		FreqHz: HzFromTHz(193.3),
	}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}
	res, err = ApplyInventory(db, dev.ID, Inventory{
		Kind: models.OpticalKindROADM,
		Ports: []InventoryPort{
			{Name: "1/0/DEG1", Role: models.PortROADMDegree},
			{Name: "1/0/DEG2", Role: models.PortROADMDegree},
			{Name: "1/0/AD1", Role: models.PortROADMAddDrop, FreqHz: hz},
		},
		XConnects: []InventoryXConnect{
			{Name: "xc-1", Kind: models.XCAddDrop, PortA: "1/0/DEG1", PortB: "1/0/AD1", FreqHz: hz},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.XConnectsDeleted != 1 {
		t.Errorf("deleted = %d, skipped %v", res.XConnectsDeleted, res.XConnectsSkipped)
	}
	if err := db.Where("device_id = ?", dev.ID).Find(&xcs).Error; err != nil {
		t.Fatal(err)
	}
	if len(xcs) != 2 {
		t.Fatalf("after re-apply want add/drop + manual express, got %+v", xcs)
	}
	var sawManual, sawDriver bool
	for _, x := range xcs {
		if x.ID == manual.ID && x.Source == "" {
			sawManual = true
		}
		if x.Kind == models.XCAddDrop && x.Source == models.XCSourceOpenROADM {
			sawDriver = true
		}
	}
	if !sawManual || !sawDriver {
		t.Errorf("manual=%v driver=%v xcs=%+v", sawManual, sawDriver, xcs)
	}
}

func TestApplyInventoryXPDRTributaryAndSuffixMatch(t *testing.T) {
	db := testGraphDB(t)
	dev := addDev(db, "xpdr-1", "")
	client := addIface(db, dev, "C1")
	line := addIface(db, dev, "L1")
	hz := HzFromTHz(193.1)

	res, err := ApplyInventory(db, dev.ID, Inventory{
		Kind: models.OpticalKindWDMShelf,
		Ports: []InventoryPort{
			{Name: "1/1/C1", Role: models.PortTXPClient},
			{Name: "1/1/L1", Role: models.PortTXPLine, FreqHz: hz},
		},
		XConnects: []InventoryXConnect{
			{Name: "1", Kind: models.XCTributary, PortA: "1/1/L1", PortB: "1/1/C1", FreqHz: hz},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.PortsUpserted != 2 || res.XConnectsUpserted != 1 {
		t.Fatalf("result = %+v", res)
	}
	var xc models.OpticalXConnect
	if err := db.Where("device_id = ?", dev.ID).First(&xc).Error; err != nil {
		t.Fatal(err)
	}
	if xc.Kind != models.XCTributary || xc.InterfaceAID != client.ID || xc.InterfaceBID != line.ID {
		t.Errorf("tributary order = %+v want a=%d (client) b=%d (line)", xc, client.ID, line.ID)
	}
}

func TestApplyInventoryUnknownDevice(t *testing.T) {
	db := testGraphDB(t)
	if _, err := ApplyInventory(db, 999, Inventory{Kind: models.OpticalKindROADM}); err == nil {
		t.Fatal("expected error")
	}
}
