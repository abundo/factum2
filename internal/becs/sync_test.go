package becs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

type testReporter struct {
	lines []string
}

func (r *testReporter) Emit(level jobevent.Level, format string, args ...any) {
	r.lines = append(r.lines, string(level)+": "+fmt.Sprintf(format, args...))
}

func (r *testReporter) EmitErr(err error) {
	if err != nil {
		r.Emit(jobevent.Error, "%s", err)
	}
}

type fakeNetbox struct {
	devices            []*netboxtool.NBDevice
	deviceTypes        map[string]*netboxtool.NetboxDeviceTypeDetail
	site               *netboxtool.NetboxNamedRef
	role               *netboxtool.NetboxNamedRef
	platform           *netboxtool.NetboxNamedRef
	missingCustomField string

	createdNames []string
	deletedIDs   []int
	updates      []map[string]any
	createdIfs   []string
	deletedIfs   []int
	createdAddrs []string
	deletedAddrs []int
	nextID       uint
}

func (f *fakeNetbox) alloc() uint {
	f.nextID++
	if f.nextID < 100 {
		f.nextID = 100
	}
	return f.nextID
}

func (f *fakeNetbox) GetDevices() ([]*netboxtool.NBDevice, error) {
	return f.devices, nil
}

func (f *fakeNetbox) GetDevice(name string, id int) (*netboxtool.NBDevice, error) {
	for _, d := range f.devices {
		if (name != "" && strings.EqualFold(d.Name, name)) || (id > 0 && int(d.NetboxID) == id) {
			return d, nil
		}
	}
	return nil, nil
}

func (f *fakeNetbox) GetDeviceType(manufacturer, model string) (*netboxtool.NetboxDeviceTypeDetail, error) {
	dt, ok := f.deviceTypes[manufacturer+"/"+model]
	if !ok {
		return nil, fmt.Errorf("device type not found: %s/%s", manufacturer, model)
	}
	return dt, nil
}

func (f *fakeNetbox) CreateDevice(name string, extra map[string]any) (*netboxtool.NetboxDeviceREST, error) {
	f.createdNames = append(f.createdNames, name)
	id := f.alloc()
	cf, _ := extra["custom_fields"].(map[string]any)
	d := &netboxtool.NBDevice{
		NetboxID:     id,
		Name:         name,
		CustomFields: cf,
		Enabled:      extra["status"] == "active",
	}
	if dt, ok := extra["device_type"].(uint); ok {
		d.ModelID = dt
	}
	f.devices = append(f.devices, d)
	return &netboxtool.NetboxDeviceREST{ID: id, Name: name}, nil
}

func (f *fakeNetbox) UpdateDevice(deviceID uint, changes map[string]any) error {
	cp := map[string]any{}
	for k, v := range changes {
		cp[k] = v
	}
	f.updates = append(f.updates, cp)
	for _, d := range f.devices {
		if d.NetboxID != deviceID {
			continue
		}
		if name, ok := changes["name"].(string); ok {
			d.Name = name
		}
		if status, ok := changes["status"].(string); ok {
			d.Enabled = status == "active"
		}
		if cf, ok := changes["custom_fields"].(map[string]any); ok {
			if d.CustomFields == nil {
				d.CustomFields = map[string]any{}
			}
			for k, v := range cf {
				d.CustomFields[k] = v
			}
			if p, ok := cf["parents"].(string); ok {
				d.CfParents = p
			}
		}
	}
	return nil
}

func (f *fakeNetbox) DeleteDevice(id int) error {
	f.deletedIDs = append(f.deletedIDs, id)
	keep := f.devices[:0]
	for _, d := range f.devices {
		if int(d.NetboxID) != id {
			keep = append(keep, d)
		}
	}
	f.devices = keep
	return nil
}

func (f *fakeNetbox) CreateInterfaceWithOptions(deviceID uint, name string, extra map[string]any) (*netboxtool.NetboxInterfaceREST, error) {
	f.createdIfs = append(f.createdIfs, name)
	id := f.alloc()
	for _, d := range f.devices {
		if d.NetboxID != deviceID {
			continue
		}
		iface := netboxtool.NBInterface{NetboxID: id, Name: name}
		if cf, ok := extra["custom_fields"].(map[string]any); ok {
			iface.CustomFields = cf
		}
		d.Interfaces = append(d.Interfaces, iface)
	}
	return &netboxtool.NetboxInterfaceREST{ID: id, Name: name}, nil
}

func (f *fakeNetbox) InterfaceUpdate(interfaceID int, changes map[string]any) error {
	for _, d := range f.devices {
		for i := range d.Interfaces {
			if int(d.Interfaces[i].NetboxID) != interfaceID {
				continue
			}
			if name, ok := changes["name"].(string); ok {
				d.Interfaces[i].Name = name
			}
			if cf, ok := changes["custom_fields"].(map[string]any); ok {
				if d.Interfaces[i].CustomFields == nil {
					d.Interfaces[i].CustomFields = map[string]any{}
				}
				for k, v := range cf {
					d.Interfaces[i].CustomFields[k] = v
				}
			}
		}
	}
	return nil
}

func (f *fakeNetbox) InterfaceDelete(interfaceID int) error {
	f.deletedIfs = append(f.deletedIfs, interfaceID)
	for _, d := range f.devices {
		keep := d.Interfaces[:0]
		for _, iface := range d.Interfaces {
			if int(iface.NetboxID) != interfaceID {
				keep = append(keep, iface)
			}
		}
		d.Interfaces = keep
	}
	return nil
}

func (f *fakeNetbox) CreateInterfaceAddress(interfaceID uint, address string, extra map[string]any) (*netboxtool.NBAddress, error) {
	f.createdAddrs = append(f.createdAddrs, address)
	id := f.alloc()
	addr := &netboxtool.NBAddress{NetboxID: id, Address: address, NBInterfaceID: interfaceID}
	for _, d := range f.devices {
		for i := range d.Interfaces {
			if d.Interfaces[i].NetboxID == interfaceID {
				d.Interfaces[i].Addresses = append(d.Interfaces[i].Addresses, *addr)
			}
		}
	}
	return addr, nil
}

func (f *fakeNetbox) AddressDelete(addressID int) error {
	f.deletedAddrs = append(f.deletedAddrs, addressID)
	return nil
}

func (f *fakeNetbox) GetSiteByName(name string) (*netboxtool.NetboxNamedRef, error) {
	return f.site, nil
}
func (f *fakeNetbox) GetDeviceRoleBySlug(slug string) (*netboxtool.NetboxNamedRef, error) {
	return f.role, nil
}
func (f *fakeNetbox) GetPlatformBySlug(slug string) (*netboxtool.NetboxNamedRef, error) {
	return f.platform, nil
}

func (f *fakeNetbox) RequireCustomField(name string, objectTypes ...string) error {
	if f.missingCustomField != "" && f.missingCustomField == name {
		return fmt.Errorf("netbox custom field %q is not defined", name)
	}
	return nil
}

func newTestSyncer(t *testing.T, nb *fakeNetbox) *Syncer {
	t.Helper()
	c := NewClient("http://unused", "u", "p")
	c.Index(loadFixture(t))
	return &Syncer{
		becs:       c,
		nb:         nb,
		settings:   &models.Settings{},
		reporter:   &testReporter{},
		domain:     "example.com",
		types:      map[string]*netboxtool.NetboxDeviceTypeDetail{},
		nbByName:   map[string]*netboxtool.NBDevice{},
		nbByOID:    map[uint]*netboxtool.NBDevice{},
		siteID:     1,
		roleID:     2,
		platformID: 3,
	}
}

func TestSyncDevicesCreate(t *testing.T) {
	nb := &fakeNetbox{
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{
			"Waystream/ASR7348": {ID: 10, Model: "ASR7348"},
		},
		site:     &netboxtool.NetboxNamedRef{ID: 1, Name: "Default"},
		role:     &netboxtool.NetboxNamedRef{ID: 2, Slug: "access-nod"},
		platform: &netboxtool.NetboxNamedRef{ID: 3, Slug: "ibos"},
	}
	s := newTestSyncer(t, nb)
	devs, err := s.becs.Devices(s.domain)
	if err != nil {
		t.Fatal(err)
	}
	becsByOID := map[int]*Device{devs[0].OID: devs[0]}
	becsByName := map[string]*Device{devs[0].ShortName: devs[0]}
	s.syncDevices(becsByOID, becsByName)
	if len(nb.createdNames) != 1 || nb.createdNames[0] != "test-lab2" {
		t.Fatalf("created=%v, want [test-lab2]", nb.createdNames)
	}
	if s.created != 1 {
		t.Errorf("created count=%d", s.created)
	}
	if _, ok := s.nbByOID[uint(devs[0].OID)]; !ok {
		t.Error("created device not indexed by oid")
	}
}

func TestSyncDevicesDeleteMissing(t *testing.T) {
	stale := &netboxtool.NBDevice{
		NetboxID:     7,
		Name:         "gone",
		CustomFields: map[string]any{"becs_oid": 999},
	}
	nb := &fakeNetbox{
		devices:     []*netboxtool.NBDevice{stale},
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{"Waystream/ASR7348": {ID: 10, Model: "ASR7348"}},
		site:        &netboxtool.NetboxNamedRef{ID: 1},
		role:        &netboxtool.NetboxNamedRef{ID: 2},
		platform:    &netboxtool.NetboxNamedRef{ID: 3},
	}
	s := newTestSyncer(t, nb)
	s.nbByName["gone"] = stale
	s.nbByOID[999] = stale
	s.syncDevices(map[int]*Device{}, map[string]*Device{})
	if len(nb.deletedIDs) != 1 || nb.deletedIDs[0] != 7 {
		t.Fatalf("deleted=%v, want [7]", nb.deletedIDs)
	}
}

func TestSyncDevicesDoesNotDeleteWithoutOID(t *testing.T) {
	other := &netboxtool.NBDevice{NetboxID: 3, Name: "core-sw1"}
	nb := &fakeNetbox{devices: []*netboxtool.NBDevice{other}}
	s := newTestSyncer(t, nb)
	s.nbByName["core-sw1"] = other
	s.syncDevices(map[int]*Device{}, map[string]*Device{})
	if len(nb.deletedIDs) != 0 {
		t.Fatalf("deleted unowned device %v", nb.deletedIDs)
	}
}

func TestSyncDevicesSetOID(t *testing.T) {
	existing := &netboxtool.NBDevice{
		NetboxID: 11,
		Name:     "test-lab2",
	}
	nb := &fakeNetbox{
		devices:     []*netboxtool.NBDevice{existing},
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{"Waystream/ASR7348": {ID: 10, Model: "ASR7348"}},
		site:        &netboxtool.NetboxNamedRef{ID: 1},
		role:        &netboxtool.NetboxNamedRef{ID: 2},
		platform:    &netboxtool.NetboxNamedRef{ID: 3},
	}
	s := newTestSyncer(t, nb)
	s.nbByName["test-lab2"] = existing
	devs, err := s.becs.Devices(s.domain)
	if err != nil {
		t.Fatal(err)
	}
	s.syncDevices(map[int]*Device{devs[0].OID: devs[0]}, map[string]*Device{devs[0].ShortName: devs[0]})
	if len(nb.createdNames) != 0 {
		t.Errorf("should not create, created=%v", nb.createdNames)
	}
	if got := deviceBecsOID(existing); got != uint(devs[0].OID) {
		t.Errorf("becs_oid=%d, want %d", got, devs[0].OID)
	}
}

func TestSyncInterfacesCreateAndSkipEthernet0(t *testing.T) {
	dev := &netboxtool.NBDevice{
		NetboxID:     20,
		Name:         "test-lab2",
		CustomFields: map[string]any{"becs_oid": 3641645},
	}
	nb := &fakeNetbox{
		devices:     []*netboxtool.NBDevice{dev},
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{"Waystream/ASR7348": {ID: 10, Model: "ASR7348"}},
	}
	s := newTestSyncer(t, nb)
	s.nbByOID[3641645] = dev
	devs, err := s.becs.Devices(s.domain)
	if err != nil {
		t.Fatal(err)
	}
	s.syncInterfaces(dev, devs[0])
	for _, name := range nb.createdIfs {
		if name == "ethernet0" {
			t.Fatal("ethernet0 should be ignored")
		}
	}
	if len(nb.createdIfs) < 2 {
		t.Fatalf("created ifs=%v, want loopback0 and vlan1", nb.createdIfs)
	}
}

func TestIfaceTypeFallback(t *testing.T) {
	if got := ifaceType(nil, "ethernet1/1"); got != "1000base-t" {
		t.Errorf("ethernet -> %s", got)
	}
	if got := ifaceType(nil, "loopback0"); got != "virtual" {
		t.Errorf("loopback -> %s", got)
	}
	dt := &netboxtool.NetboxDeviceTypeDetail{
		Interfaces: []netboxtool.NetboxInterfaceTemplate{{Name: "ethernet1/1", Type: "10gbase-x-sfpp"}},
	}
	if got := ifaceType(dt, "ethernet1/1"); got != "10gbase-x-sfpp" {
		t.Errorf("template -> %s", got)
	}
}

func TestRunRequiresBecsOIDCustomField(t *testing.T) {
	nb := &fakeNetbox{
		missingCustomField: "becs_oid",
		site:               &netboxtool.NetboxNamedRef{ID: 1},
		role:               &netboxtool.NetboxNamedRef{ID: 2},
		platform:           &netboxtool.NetboxNamedRef{ID: 3},
	}
	s := newTestSyncer(t, nb)
	err := s.run("")
	if err == nil || !strings.Contains(err.Error(), "becs_oid") {
		t.Fatalf("want missing becs_oid error, got %v", err)
	}
}

func TestRunCreatesDeviceInterfacesAndAddress(t *testing.T) {
	nb := &fakeNetbox{
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{
			"Waystream/ASR7348": {
				ID:    10,
				Model: "ASR7348",
				Interfaces: []netboxtool.NetboxInterfaceTemplate{
					{Name: "loopback0", Type: "virtual"},
					{Name: "vlan1", Type: "virtual"},
				},
			},
		},
		site:     &netboxtool.NetboxNamedRef{ID: 1, Name: "Default"},
		role:     &netboxtool.NetboxNamedRef{ID: 2, Slug: "access-nod"},
		platform: &netboxtool.NetboxNamedRef{ID: 3, Slug: "ibos"},
	}
	s := newTestSyncer(t, nb)
	if err := s.run(""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(nb.createdNames) != 1 || nb.createdNames[0] != "test-lab2" {
		t.Fatalf("created devices=%v", nb.createdNames)
	}
	for _, name := range nb.createdIfs {
		if name == "ethernet0" {
			t.Fatal("ethernet0 should be ignored")
		}
	}
	if len(nb.createdIfs) < 2 {
		t.Fatalf("created ifs=%v, want loopback0 and vlan1", nb.createdIfs)
	}
	wantAddr := map[string]bool{"10.15.15.153/32": false, "10.0.0.9/24": false}
	for _, a := range nb.createdAddrs {
		if _, ok := wantAddr[a]; ok {
			wantAddr[a] = true
		}
	}
	for addr, seen := range wantAddr {
		if !seen {
			t.Errorf("missing address %s (got %v)", addr, nb.createdAddrs)
		}
	}
	if !s.changed || s.created != 1 {
		t.Errorf("changed=%v created=%d", s.changed, s.created)
	}
}

func TestCustomFieldUint(t *testing.T) {
	m := map[string]any{"n": float64(12), "s": "34", "bad": "x"}
	if got := customFieldUint(m, "n"); got != 12 {
		t.Errorf("number: %d", got)
	}
	if got := customFieldUint(m, "s"); got != 34 {
		t.Errorf("string: %d", got)
	}
	if got := customFieldUint(m, "bad"); got != 0 {
		t.Errorf("bad: %d", got)
	}
	if got := customFieldUint(nil, "n"); got != 0 {
		t.Errorf("nil: %d", got)
	}
}

func TestJoinParents(t *testing.T) {
	if got := joinParents([]string{"a.example.com", "b.example.com"}); got != "a.example.com,b.example.com" {
		t.Errorf("got %q", got)
	}
	if joinParents(nil) != "" {
		t.Error("empty parents")
	}
}

func reporterText(s *Syncer) string {
	return strings.Join(s.reporter.(*testReporter).lines, "\n")
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}

func assertNoWrites(t *testing.T, nb *fakeNetbox) {
	t.Helper()
	if len(nb.createdNames) != 0 || len(nb.deletedIDs) != 0 || len(nb.updates) != 0 ||
		len(nb.createdIfs) != 0 || len(nb.deletedIfs) != 0 ||
		len(nb.createdAddrs) != 0 || len(nb.deletedAddrs) != 0 {
		t.Fatalf("dry-run wrote to netbox: created=%v deleted=%v updates=%v ifs=%v delifs=%v addrs=%v deladdrs=%v",
			nb.createdNames, nb.deletedIDs, nb.updates, nb.createdIfs, nb.deletedIfs, nb.createdAddrs, nb.deletedAddrs)
	}
}

func testDeviceType() *netboxtool.NetboxDeviceTypeDetail {
	return &netboxtool.NetboxDeviceTypeDetail{
		ID:    10,
		Model: "ASR7348",
		Interfaces: []netboxtool.NetboxInterfaceTemplate{
			{Name: "loopback0", Type: "virtual"},
			{Name: "vlan1", Type: "virtual"},
		},
	}
}

func TestDryRunCreateDoesNotWrite(t *testing.T) {
	nb := &fakeNetbox{
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{
			"Waystream/ASR7348": testDeviceType(),
		},
		site:     &netboxtool.NetboxNamedRef{ID: 1, Name: "Default"},
		role:     &netboxtool.NetboxNamedRef{ID: 2, Slug: "access-nod"},
		platform: &netboxtool.NetboxNamedRef{ID: 3, Slug: "ibos"},
	}
	s := newTestSyncer(t, nb)
	s.dryRun = true
	if err := s.run(""); err != nil {
		t.Fatalf("run: %v", err)
	}
	assertNoWrites(t, nb)

	out := reporterText(s)
	assertContains(t, out, "dry-run: would create Netbox device test-lab2:")
	assertContains(t, out, `"becs_oid":3641645`)
	assertContains(t, out, `"status":"active"`)
	assertContains(t, out, `"parents":"core1.example.com"`)
	assertContains(t, out, "dry-run: would create interface test-lab2/loopback0:")
	assertContains(t, out, `"becs_oid":3641698`)
	assertContains(t, out, "dry-run: would create address test-lab2/loopback0 10.15.15.153/32:")
	assertContains(t, out, "dry-run: would create address test-lab2/vlan1 10.0.0.9/24:")
	assertContains(t, out, "dry-run: would set primary_ip4 on test-lab2 to 10.15.15.153/32")
	if s.created != 1 {
		t.Errorf("created count=%d, want 1", s.created)
	}
	for _, line := range s.reporter.(*testReporter).lines {
		if strings.Contains(line, "create interface") && strings.Contains(line, "ethernet0") {
			t.Fatalf("ethernet0 should be ignored: %s", line)
		}
	}
}

func TestDryRunDeleteDoesNotWrite(t *testing.T) {
	stale := &netboxtool.NBDevice{
		NetboxID:     7,
		Name:         "gone",
		CustomFields: map[string]any{"becs_oid": 999},
	}
	nb := &fakeNetbox{
		devices: []*netboxtool.NBDevice{stale},
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{
			"Waystream/ASR7348": testDeviceType(),
		},
		site:     &netboxtool.NetboxNamedRef{ID: 1, Name: "Default"},
		role:     &netboxtool.NetboxNamedRef{ID: 2, Slug: "access-nod"},
		platform: &netboxtool.NetboxNamedRef{ID: 3, Slug: "ibos"},
	}
	s := newTestSyncer(t, nb)
	s.dryRun = true
	if err := s.run(""); err != nil {
		t.Fatalf("run: %v", err)
	}
	assertNoWrites(t, nb)
	assertContains(t, reporterText(s), "dry-run: would delete Netbox device gone (id=7)")
	if s.deleted != 1 {
		t.Errorf("deleted count=%d, want 1", s.deleted)
	}
}

func TestDryRunUpdateShowsExactPayload(t *testing.T) {
	existing := &netboxtool.NBDevice{
		NetboxID:           20,
		Name:               "old-lab",
		Enabled:            false,
		ModelName:          "ASR7348",
		CustomFields:       map[string]any{"becs_oid": 3641645},
		CfParents:          "core1.example.com",
		CfAlarmDestination: "noc",
		CfAlarmTimeperiod:  "24x7",
		CfConnectionMethod: "ssh",
	}
	nb := &fakeNetbox{
		devices: []*netboxtool.NBDevice{existing},
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{
			"Waystream/ASR7348": testDeviceType(),
		},
		site:     &netboxtool.NetboxNamedRef{ID: 1, Name: "Default"},
		role:     &netboxtool.NetboxNamedRef{ID: 2, Slug: "access-nod"},
		platform: &netboxtool.NetboxNamedRef{ID: 3, Slug: "ibos"},
	}
	s := newTestSyncer(t, nb)
	s.dryRun = true
	if err := s.run(""); err != nil {
		t.Fatalf("run: %v", err)
	}
	assertNoWrites(t, nb)
	out := reporterText(s)
	assertContains(t, out, "dry-run: would update device old-lab:")
	assertContains(t, out, `"name":"test-lab2"`)
	assertContains(t, out, `"status":"active"`)
	if strings.Contains(out, "would create Netbox device") {
		t.Error("should update existing device, not create")
	}
}

func TestDryRunAddressReplaceDoesNotWrite(t *testing.T) {
	existing := &netboxtool.NBDevice{
		NetboxID:           20,
		Name:               "test-lab2",
		Enabled:            true,
		ModelName:          "ASR7348",
		CustomFields:       map[string]any{"becs_oid": 3641645},
		CfParents:          "core1.example.com",
		CfAlarmDestination: "noc",
		CfAlarmTimeperiod:  "24x7",
		CfConnectionMethod: "ssh",
		PrimaryIPv4ID:      50,
		Interfaces: []netboxtool.NBInterface{
			{
				NetboxID:     30,
				Name:         "loopback0",
				Type:         "virtual",
				Enabled:      true,
				CustomFields: map[string]any{"becs_oid": 3641698},
				Addresses:    []netboxtool.NBAddress{{NetboxID: 50, Address: "1.2.3.4/32"}},
			},
			{
				NetboxID:     31,
				Name:         "vlan1",
				Type:         "virtual",
				Enabled:      true,
				CustomFields: map[string]any{"becs_oid": 999004},
				Addresses:    []netboxtool.NBAddress{{NetboxID: 51, Address: "10.0.0.9/24"}},
			},
		},
	}
	nb := &fakeNetbox{
		devices: []*netboxtool.NBDevice{existing},
		deviceTypes: map[string]*netboxtool.NetboxDeviceTypeDetail{
			"Waystream/ASR7348": testDeviceType(),
		},
		site:     &netboxtool.NetboxNamedRef{ID: 1, Name: "Default"},
		role:     &netboxtool.NetboxNamedRef{ID: 2, Slug: "access-nod"},
		platform: &netboxtool.NetboxNamedRef{ID: 3, Slug: "ibos"},
	}
	s := newTestSyncer(t, nb)
	s.dryRun = true
	if err := s.run(""); err != nil {
		t.Fatalf("run: %v", err)
	}
	assertNoWrites(t, nb)
	out := reporterText(s)
	assertContains(t, out, "dry-run: would delete address test-lab2/loopback0 1.2.3.4/32 (id=50)")
	assertContains(t, out, "dry-run: would create address test-lab2/loopback0 10.15.15.153/32:")
}

func TestFormatPayload(t *testing.T) {
	got := formatPayload(map[string]any{"status": "active", "custom_fields": map[string]any{"becs_oid": 12}})
	assertContains(t, got, `"status":"active"`)
	assertContains(t, got, `"becs_oid":12`)
}

var _ jobevent.Reporter = (*testReporter)(nil)
var _ netboxAPI = (*fakeNetbox)(nil)
