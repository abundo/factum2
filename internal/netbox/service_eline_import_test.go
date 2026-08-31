package netbox

import (
	"io"
	"testing"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeL2VPNAPI struct {
	l2vpns []*netboxtool.NBL2VPN
	terms  map[uint][]*netboxtool.NBL2VPNTermination
}

func (f *fakeL2VPNAPI) GetL2VPNs(l2vpnType string) ([]*netboxtool.NBL2VPN, error) {
	var out []*netboxtool.NBL2VPN
	for _, l := range f.l2vpns {
		if l2vpnType == "" || l.Type == l2vpnType {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeL2VPNAPI) GetL2VPNTerminations(l2vpnID uint) ([]*netboxtool.NBL2VPNTermination, error) {
	return f.terms[l2vpnID], nil
}

func newImportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Unique DSN per test so parallel-ish package tests don't share memory.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := util.MigrateDatabase(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func silentReporter() jobevent.Reporter {
	return jobevent.NewConsoleReporter(io.Discard)
}

func seedDeviceWithIfaces(t *testing.T, db *gorm.DB, name string, netboxDeviceID uint, ifaces []models.Interface) (deviceID uint, ifaceIDs map[string]uint) {
	t.Helper()
	d := models.Device{Name: name, NetboxID: netboxDeviceID, CfSource: "netbox"}
	if err := db.Create(&d).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}
	ifaceIDs = make(map[string]uint, len(ifaces))
	for _, iface := range ifaces {
		iface.DeviceID = d.ID
		if err := db.Create(&iface).Error; err != nil {
			t.Fatalf("create iface %s: %v", iface.Name, err)
		}
		ifaceIDs[iface.Name] = iface.ID
	}
	return d.ID, ifaceIDs
}

func TestSplitSubinterfaceName(t *testing.T) {
	cases := []struct {
		name       string
		wantParent string
		wantVLAN   int
		wantOK     bool
	}{
		{"Ethernet3.338", "Ethernet3", 338, true},
		{"Ethernet43", "", 0, false},
		{"Po1.100", "Po1", 100, true},
		{"Ethernet3.0", "", 0, false},
		// Outside classic 802.1Q range - still a valid subinterface unit
		// (e.g. device-sync CN00570-11 -> Ethernet3.5711).
		{"Ethernet3.5711", "Ethernet3", 5711, true},
	}
	for _, tc := range cases {
		parent, vlan, ok := splitSubinterfaceName(tc.name)
		if ok != tc.wantOK || parent != tc.wantParent || vlan != tc.wantVLAN {
			t.Errorf("splitSubinterfaceName(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tc.name, parent, vlan, ok, tc.wantParent, tc.wantVLAN, tc.wantOK)
		}
	}
}

func TestSyncServiceEndpointsFromL2VPNs_LinksByServiceID(t *testing.T) {
	db := newImportTestDB(t)

	devA, ifA := seedDeviceWithIfaces(t, db, "pi1-r4", 10, []models.Interface{
		{NetboxID: 100, Name: "Ethernet3", Type: "1000base-t"},
		{NetboxID: 101, Name: "Ethernet3.338", Type: "virtual", ParentID: 100},
	})
	devB, ifB := seedDeviceWithIfaces(t, db, "pi1-r5", 11, []models.Interface{
		{NetboxID: 200, Name: "Ethernet1", Type: "1000base-t"},
		{NetboxID: 201, Name: "Ethernet1.338", Type: "virtual", ParentID: 200},
	})

	svc := models.Service{
		ServiceID: "CN00078",
		Name:      "from lime",
		Source:    "lime",
	}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("create service: %v", err)
	}

	api := &fakeL2VPNAPI{
		l2vpns: []*netboxtool.NBL2VPN{
			{NetboxID: 50, Name: "CN00078", Type: "evpl", Identifier: 1000078},
		},
		terms: map[uint][]*netboxtool.NBL2VPNTermination{
			50: {
				{NetboxID: 501, L2VPNID: 50, InterfaceID: 101},
				{NetboxID: 502, L2VPNID: 50, InterfaceID: 201},
			},
		},
	}

	if err := syncServiceEndpointsFromL2VPNs(db, api, silentReporter()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var got models.Service
	if err := db.First(&got, svc.ID).Error; err != nil {
		t.Fatalf("reload service: %v", err)
	}
	if got.ServiceType != "ELINE" {
		t.Errorf("ServiceType = %q, want ELINE", got.ServiceType)
	}
	if got.L2VPNNetboxID != 50 {
		t.Errorf("L2VPNNetboxID = %d, want 50", got.L2VPNNetboxID)
	}
	if got.PseudowireID != 1000078 {
		t.Errorf("PseudowireID = %d, want 1000078", got.PseudowireID)
	}

	eps := mustListEndpoints(t, db, got.ID)
	a, b := eps["a"], eps["b"]
	if a.DeviceID != devA || a.InterfaceID != ifA["Ethernet3"] {
		t.Errorf("endpoint A = device %d iface %d, want device %d iface %d",
			a.DeviceID, a.InterfaceID, devA, ifA["Ethernet3"])
	}
	if cfgmgmt.VLANFromFields(a.Fields) != 338 {
		t.Errorf("endpoint A vlan = %d, want 338", cfgmgmt.VLANFromFields(a.Fields))
	}
	subA, termA := cfgmgmt.NetboxIDsFromFields(a.Fields)
	if subA != 101 || termA != 501 {
		t.Errorf("endpoint A sub/term = %d/%d, want 101/501", subA, termA)
	}
	if b.DeviceID != devB || b.InterfaceID != ifB["Ethernet1"] {
		t.Errorf("endpoint B = device %d iface %d, want device %d iface %d",
			b.DeviceID, b.InterfaceID, devB, ifB["Ethernet1"])
	}
	if cfgmgmt.VLANFromFields(b.Fields) != 338 {
		t.Errorf("endpoint B vlan = %d, want 338", cfgmgmt.VLANFromFields(b.Fields))
	}
	subB, termB := cfgmgmt.NetboxIDsFromFields(b.Fields)
	if subB != 201 || termB != 502 {
		t.Errorf("endpoint B sub/term = %d/%d, want 201/502", subB, termB)
	}
}

func mustListEndpoints(t *testing.T, db *gorm.DB, svcID uint) map[string]models.ServiceEndpoint {
	t.Helper()
	rows, err := cfgmgmt.ListEndpoints(db, svcID)
	if err != nil {
		t.Fatalf("list endpoints: %v", err)
	}
	out := map[string]models.ServiceEndpoint{}
	for _, ep := range rows {
		out[ep.Role] = ep
	}
	return out
}

func TestSyncServiceEndpointsFromL2VPNs_Idempotent(t *testing.T) {
	db := newImportTestDB(t)
	_, _ = seedDeviceWithIfaces(t, db, "r1", 1, []models.Interface{
		{NetboxID: 10, Name: "Eth1", Type: "1000base-t"},
		{NetboxID: 11, Name: "Eth1.10", Type: "virtual", ParentID: 10},
	})
	svc := models.Service{ServiceID: "CN00001", Source: "lime"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("create service: %v", err)
	}
	api := &fakeL2VPNAPI{
		l2vpns: []*netboxtool.NBL2VPN{{NetboxID: 9, Name: "CN00001", Type: "evpl", Identifier: 1000001}},
		terms: map[uint][]*netboxtool.NBL2VPNTermination{
			9: {{NetboxID: 90, L2VPNID: 9, InterfaceID: 11}},
		},
	}
	if err := syncServiceEndpointsFromL2VPNs(db, api, silentReporter()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	var afterFirst models.Service
	if err := db.First(&afterFirst, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := syncServiceEndpointsFromL2VPNs(db, api, silentReporter()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	var afterSecond models.Service
	if err := db.First(&afterSecond, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterFirst.UpdatedAt != afterSecond.UpdatedAt {
		// GORM may bump UpdatedAt on no-op Updates depending on version;
		// check field equality instead.
	}
	if afterSecond.L2VPNNetboxID != afterFirst.L2VPNNetboxID {
		t.Errorf("second sync changed l2vpn: %+v vs %+v", afterFirst, afterSecond)
	}
	first := mustListEndpoints(t, db, afterFirst.ID)
	second := mustListEndpoints(t, db, afterSecond.ID)
	if first["a"].InterfaceID != second["a"].InterfaceID {
		t.Errorf("second sync changed endpoints: %+v vs %+v", first, second)
	}
}

func TestSyncServiceEndpointsFromL2VPNs_SkipsNonELINEType(t *testing.T) {
	db := newImportTestDB(t)
	_, _ = seedDeviceWithIfaces(t, db, "r1", 1, []models.Interface{
		{NetboxID: 10, Name: "Eth1", Type: "1000base-t"},
		{NetboxID: 11, Name: "Eth1.10", Type: "virtual", ParentID: 10},
	})
	svc := models.Service{ServiceID: "CN00002", ServiceType: "ELAN", Source: "lime"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("create service: %v", err)
	}
	api := &fakeL2VPNAPI{
		l2vpns: []*netboxtool.NBL2VPN{{NetboxID: 9, Name: "CN00002", Type: "evpl", Identifier: 1000002}},
		terms: map[uint][]*netboxtool.NBL2VPNTermination{
			9: {{NetboxID: 90, L2VPNID: 9, InterfaceID: 11}},
		},
	}
	if err := syncServiceEndpointsFromL2VPNs(db, api, silentReporter()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	var got models.Service
	if err := db.First(&got, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.L2VPNNetboxID != 0 {
		t.Errorf("ELAN service was modified: l2vpn=%d", got.L2VPNNetboxID)
	}
	if len(mustListEndpoints(t, db, got.ID)) != 0 {
		t.Errorf("ELAN service grew endpoints")
	}
	if got.ServiceType != "ELAN" {
		t.Errorf("ServiceType = %q, want ELAN", got.ServiceType)
	}
}

func TestSyncServiceEndpointsFromL2VPNs_MatchesByL2VPNNetboxID(t *testing.T) {
	db := newImportTestDB(t)
	_, ifs := seedDeviceWithIfaces(t, db, "r1", 1, []models.Interface{
		{NetboxID: 10, Name: "Eth1", Type: "1000base-t"},
		{NetboxID: 11, Name: "Eth1.10", Type: "virtual", ParentID: 10},
	})
	// Name no longer matches L2VPN name, but L2VPNNetboxID does (e.g. rename).
	svc := models.Service{
		ServiceID:     "CN-RENAMED",
		L2VPNNetboxID: 77,
		Source:        "lime",
	}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("create service: %v", err)
	}
	api := &fakeL2VPNAPI{
		l2vpns: []*netboxtool.NBL2VPN{{NetboxID: 77, Name: "CN00099", Type: "evpl", Identifier: 1000099}},
		terms: map[uint][]*netboxtool.NBL2VPNTermination{
			77: {{NetboxID: 700, L2VPNID: 77, InterfaceID: 11}},
		},
	}
	if err := syncServiceEndpointsFromL2VPNs(db, api, silentReporter()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	var got models.Service
	if err := db.First(&got, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	eps := mustListEndpoints(t, db, got.ID)
	if eps["a"].InterfaceID != ifs["Eth1"] {
		t.Errorf("endpoint a iface = %d, want %d", eps["a"].InterfaceID, ifs["Eth1"])
	}
	if got.ServiceType != "ELINE" {
		t.Errorf("ServiceType = %q, want ELINE", got.ServiceType)
	}
}

func TestSyncServiceEndpointsFromL2VPNs_NameFallbackWithoutParentID(t *testing.T) {
	db := newImportTestDB(t)
	_, ifs := seedDeviceWithIfaces(t, db, "r1", 1, []models.Interface{
		{NetboxID: 10, Name: "Ethernet3", Type: "1000base-t"},
		// ParentID deliberately 0 - resolve via name parse.
		{NetboxID: 11, Name: "Ethernet3.5711", Type: "virtual", ParentID: 0},
	})
	svc := models.Service{ServiceID: "CN00570-11", Source: "lime"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatalf("create service: %v", err)
	}
	api := &fakeL2VPNAPI{
		l2vpns: []*netboxtool.NBL2VPN{{NetboxID: 5, Name: "CN00570-11", Type: "evpl", Identifier: 1005711}},
		terms: map[uint][]*netboxtool.NBL2VPNTermination{
			5: {{NetboxID: 50, L2VPNID: 5, InterfaceID: 11}},
		},
	}
	if err := syncServiceEndpointsFromL2VPNs(db, api, silentReporter()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	var got models.Service
	if err := db.First(&got, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	eps := mustListEndpoints(t, db, got.ID)
	if eps["a"].InterfaceID != ifs["Ethernet3"] || cfgmgmt.VLANFromFields(eps["a"].Fields) != 5711 {
		t.Errorf("endpoint A iface/vlan = %d/%d, want %d/5711",
			eps["a"].InterfaceID, cfgmgmt.VLANFromFields(eps["a"].Fields), ifs["Ethernet3"])
	}
}

func TestSyncServiceEndpointsFromL2VPNs_NoMatchingService(t *testing.T) {
	db := newImportTestDB(t)
	_, _ = seedDeviceWithIfaces(t, db, "r1", 1, []models.Interface{
		{NetboxID: 10, Name: "Eth1", Type: "1000base-t"},
		{NetboxID: 11, Name: "Eth1.1", Type: "virtual", ParentID: 10},
	})
	api := &fakeL2VPNAPI{
		l2vpns: []*netboxtool.NBL2VPN{{NetboxID: 1, Name: "CN99999", Type: "evpl", Identifier: 1099999}},
		terms: map[uint][]*netboxtool.NBL2VPNTermination{
			1: {{NetboxID: 10, L2VPNID: 1, InterfaceID: 11}},
		},
	}
	if err := syncServiceEndpointsFromL2VPNs(db, api, silentReporter()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	var n int64
	if err := db.Model(&models.Service{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("created %d services, want 0 (import must not invent service rows)", n)
	}
}
