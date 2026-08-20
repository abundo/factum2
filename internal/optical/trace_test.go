package optical

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testGraphDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:trace-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Device{}, &models.Interface{}, &models.Connection{},
		&models.OpticalPort{}, &models.OpticalXConnect{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

var testNetboxID uint = 1

func addDev(db *gorm.DB, name, kind string) models.Device {
	testNetboxID++
	d := models.Device{Name: name, OpticalKind: kind, NetboxID: testNetboxID}
	if err := db.Create(&d).Error; err != nil {
		panic(err)
	}
	return d
}

func addIface(db *gorm.DB, dev models.Device, name string) models.Interface {
	testNetboxID++
	i := models.Interface{DeviceID: dev.ID, Name: name, NetboxID: testNetboxID}
	if err := db.Create(&i).Error; err != nil {
		panic(err)
	}
	return i
}

func addPort(db *gorm.DB, iface models.Interface, role string, hz uint64) {
	db.Create(&models.OpticalPort{InterfaceID: iface.ID, Role: role, FreqHz: hz})
}

func addConn(db *gorm.DB, a, b models.Interface) {
	testNetboxID++
	if err := db.Create(&models.Connection{
		NetboxID:     testNetboxID,
		DeviceAID:    a.DeviceID,
		InterfaceAID: a.ID,
		DeviceBID:    b.DeviceID,
		InterfaceBID: b.ID,
	}).Error; err != nil {
		panic(err)
	}
}

func addXC(db *gorm.DB, dev models.Device, kind string, a, b models.Interface, hz uint64) {
	xc := models.OpticalXConnect{
		DeviceID: dev.ID, Kind: kind,
		InterfaceAID: a.ID, InterfaceBID: b.ID, FreqHz: hz,
	}
	if err := db.Create(&xc).Error; err != nil {
		panic(err)
	}
}

func TestWalkCustomerPortDoesNotReverse(t *testing.T) {
	db := testGraphDB(t)
	pe := addDev(db, "pe-a", "")
	txp := addDev(db, "txp-1", models.OpticalKindWDMShelf)
	peI := addIface(db, pe, "Eth1")
	cli := addIface(db, txp, "C1")
	line := addIface(db, txp, "L1")
	addPort(db, cli, models.PortTXPClient, 0)
	addPort(db, line, models.PortTXPLine, 193100000000000)
	addConn(db, peI, cli)
	addXC(db, txp, models.XCTributary, cli, line, 0)

	g, err := LoadGraph(db)
	if err != nil {
		t.Fatal(err)
	}
	res := Walk(g, peI.ID, 0, models.TraceModeWDM)
	if res.Status == models.PathConflict {
		t.Fatalf("conflict: %s", res.Error)
	}
	// must visit the line, not bounce back to pe
	sawLine := false
	for _, h := range res.Hops {
		if h.InterfaceID != nil && *h.InterfaceID == line.ID {
			sawLine = true
		}
		if h.InterfaceID != nil && *h.InterfaceID == peI.ID && h.Kind == models.HopInterface {
			// start is fine; a second visit would be a reverse
		}
	}
	if !sawLine {
		t.Fatalf("walk did not reach txp line, hops=%+v err=%s status=%s", res.Hops, res.Error, res.Status)
	}
}

func TestWalkTXPClientIgnoresCustomerPatch(t *testing.T) {
	db := testGraphDB(t)
	pe := addDev(db, "pe-a", "")
	txp := addDev(db, "txp-1", models.OpticalKindWDMShelf)
	peI := addIface(db, pe, "Eth1")
	cli := addIface(db, txp, "C1")
	line := addIface(db, txp, "L1")
	addPort(db, cli, models.PortTXPClient, 0)
	addPort(db, line, models.PortTXPLine, 193100000000000)
	addConn(db, peI, cli)
	addXC(db, txp, models.XCTributary, cli, line, 0)

	g, _ := LoadGraph(db)
	res := Walk(g, cli.ID, 0, models.TraceModeWDM)
	sawLine := false
	for _, h := range res.Hops {
		if h.InterfaceID != nil && *h.InterfaceID == line.ID {
			sawLine = true
		}
	}
	if !sawLine {
		t.Fatalf("txp_client start should take xconnect to line, hops=%+v", res.Hops)
	}
}

func TestWalkDegreeTakesXConnectNotReverse(t *testing.T) {
	db := testGraphDB(t)
	r1 := addDev(db, "roadm-1", models.OpticalKindROADM)
	r2 := addDev(db, "roadm-2", models.OpticalKindROADM)
	ad := addIface(db, r1, "AD-34")
	degW := addIface(db, r1, "DEG-W")
	degE := addIface(db, r2, "DEG-E")
	addPort(db, ad, models.PortROADMAddDrop, 193100000000000)
	addPort(db, degW, models.PortROADMDegree, 0)
	addPort(db, degE, models.PortROADMDegree, 0)
	addXC(db, r1, models.XCAddDrop, ad, degW, 193100000000000)
	addConn(db, degW, degE)

	g, _ := LoadGraph(db)
	res := Walk(g, ad.ID, 0, models.TraceModeWDM)
	sawFar := false
	for _, h := range res.Hops {
		if h.InterfaceID != nil && *h.InterfaceID == degE.ID {
			sawFar = true
		}
	}
	if !sawFar {
		t.Fatalf("should cross trunk to far degree, hops=%+v status=%s err=%s", res.Hops, res.Status, res.Error)
	}
}

func TestWalkPENotImplicitPassThrough(t *testing.T) {
	db := testGraphDB(t)
	pe := addDev(db, "pe", "")
	a := addIface(db, pe, "Eth1")
	b := addIface(db, pe, "Eth2")
	left := addDev(db, "left", "")
	right := addDev(db, "right", "")
	la := addIface(db, left, "X")
	rb := addIface(db, right, "Y")
	addConn(db, la, a)
	addConn(db, b, rb)

	g, _ := LoadGraph(db)
	res := Walk(g, la.ID, 0, models.TraceModeFiber)
	for _, h := range res.Hops {
		if h.InterfaceID != nil && *h.InterfaceID == rb.ID {
			t.Fatalf("fiber walk must not pass through unclassified PE, hops=%+v", res.Hops)
		}
	}
}

func TestWalkPassiveImplicitPassThrough(t *testing.T) {
	db := testGraphDB(t)
	panel := addDev(db, "odf", models.OpticalKindPassive)
	w := addIface(db, panel, "W")
	e := addIface(db, panel, "E")
	left := addDev(db, "left", "")
	right := addDev(db, "right", "")
	la := addIface(db, left, "X")
	rb := addIface(db, right, "Y")
	addConn(db, la, w)
	addConn(db, e, rb)

	g, _ := LoadGraph(db)
	res := Walk(g, la.ID, rb.ID, models.TraceModeFiber)
	if res.Status != models.PathComplete {
		t.Fatalf("status=%s err=%s hops=%+v", res.Status, res.Error, res.Hops)
	}
}
