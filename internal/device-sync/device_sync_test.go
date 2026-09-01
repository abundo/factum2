package devicesync

import (
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

// ----- test doubles -----

// testReporter records every emitted line instead of printing it, so tests
// stay quiet and (if ever needed) can assert on warnings/errors. Locked
// since forEachPair now runs per-pair work (including reporter.Emit calls)
// across goroutines.
type testReporter struct {
	mu    sync.Mutex
	lines []string
}

func (r *testReporter) Emit(level jobevent.Level, format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, string(level)+": "+fmt.Sprintf(format, args...))
}

func (r *testReporter) EmitErr(err error) {
	if err != nil {
		r.Emit(jobevent.Error, "%s", err)
	}
}

// fakeNetboxAPI is a minimal in-memory NetboxAPI, recording every write call
// so tests can assert on what device-sync decided to write to Netbox
// without a real Netbox. Locked since forEachPair now dispatches per-pair
// work to goroutines that all share one fakeNetboxAPI - a real NetboxAPI's
// HTTP client is safe for concurrent use, and the fake needs to match that
// to test forEachPair's concurrency honestly rather than just tripping over
// its own bookkeeping.
type fakeNetboxAPI struct {
	mu sync.Mutex

	nextID uint

	createdInterfaces      []string
	createdInterfaceExtras map[string]map[string]any // name -> extra passed to CreateInterfaceWithOptions
	deletedInterfaceIDs    []uint
	updatedInterfaces      []map[string]any

	createdAddresses  []string
	deletedAddressIDs []uint
	updatedAddresses  []map[string]any

	cablesByID         map[uint]*netboxtool.NBCable
	createdCables      [][2]uint
	deletedCableIDs    []uint
	terminationUpdates []cableTerminationUpdate

	prefixesByCIDR  map[string]*netboxtool.NBPrefix
	createdPrefixes []string

	vlanGroupsByName   map[string]*netboxtool.NBVlanGroup
	createdVlanGroups  []string
	vlansByGroupAndVID map[uint]map[int]*netboxtool.NBVlan
	createdVlans       []*netboxtool.NBVlan
	updatedVlans       map[uint]map[string]any

	l2vpnsByName        map[string]*netboxtool.NBL2VPN
	l2vpnsByIdentifier  map[int]*netboxtool.NBL2VPN
	l2vpnsByID          map[uint]*netboxtool.NBL2VPN
	createdL2VPNs       []*netboxtool.NBL2VPN
	updatedL2VPNs       map[uint]map[string]any
	terminationsByL2VPN map[uint][]*netboxtool.NBL2VPNTermination
	createdTerminations [][2]uint // [l2vpnID, interfaceID]

	deviceTypes map[string]*netboxtool.NetboxDeviceTypeDetail // key: "manufacturer/model"
}

type cableTerminationUpdate struct {
	cableID uint
	side    int
	ifaceID uint
}

func newFakeNetboxAPI() *fakeNetboxAPI {
	return &fakeNetboxAPI{
		cablesByID:          map[uint]*netboxtool.NBCable{},
		prefixesByCIDR:      map[string]*netboxtool.NBPrefix{},
		vlanGroupsByName:    map[string]*netboxtool.NBVlanGroup{},
		vlansByGroupAndVID:  map[uint]map[int]*netboxtool.NBVlan{},
		updatedVlans:        map[uint]map[string]any{},
		l2vpnsByName:        map[string]*netboxtool.NBL2VPN{},
		l2vpnsByIdentifier:  map[int]*netboxtool.NBL2VPN{},
		l2vpnsByID:          map[uint]*netboxtool.NBL2VPN{},
		updatedL2VPNs:       map[uint]map[string]any{},
		terminationsByL2VPN: map[uint][]*netboxtool.NBL2VPNTermination{},
		deviceTypes:         map[string]*netboxtool.NetboxDeviceTypeDetail{},
	}
}

var _ NetboxAPI = (*fakeNetboxAPI)(nil)

func (f *fakeNetboxAPI) GetDeviceType(manufacturer, model string) (*netboxtool.NetboxDeviceTypeDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if dt, ok := f.deviceTypes[manufacturer+"/"+model]; ok {
		return dt, nil
	}
	return nil, fmt.Errorf("netbox device type not found: manufacturer=%q model=%q", manufacturer, model)
}

// GetDevices/GetDevice are never called by DeviceSync any more (device
// reads come from factum, see fakeFactumAPI) - they only exist here because
// netboxtool.NewCache(api) requires a CacheSource with these methods to
// construct the device-type template cache TemplateInterfaceTypes still
// uses. Trivial stubs.
func (f *fakeNetboxAPI) GetDevices() ([]*netboxtool.NBDevice, error) {
	return nil, nil
}

func (f *fakeNetboxAPI) GetDevice(name string, id int) (*netboxtool.NBDevice, error) {
	return nil, nil
}

func (f *fakeNetboxAPI) CreateInterfaceWithOptions(deviceID uint, name string, extra map[string]any) (*netboxtool.NetboxInterfaceREST, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.createdInterfaces = append(f.createdInterfaces, name)
	if f.createdInterfaceExtras == nil {
		f.createdInterfaceExtras = map[string]map[string]any{}
	}
	f.createdInterfaceExtras[name] = extra
	return &netboxtool.NetboxInterfaceREST{ID: f.nextID, Name: name}, nil
}

func (f *fakeNetboxAPI) InterfaceUpdate(interfaceID int, changes map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedInterfaces = append(f.updatedInterfaces, changes)
	return nil
}

func (f *fakeNetboxAPI) InterfaceDelete(interfaceID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedInterfaceIDs = append(f.deletedInterfaceIDs, uint(interfaceID))
	return nil
}

func (f *fakeNetboxAPI) CreateInterfaceAddress(interfaceID uint, address string, extra map[string]any) (*netboxtool.NBAddress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.createdAddresses = append(f.createdAddresses, address)
	return &netboxtool.NBAddress{NetboxID: f.nextID, Address: address}, nil
}

func (f *fakeNetboxAPI) AddressUpdate(addressID int, changes map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedAddresses = append(f.updatedAddresses, changes)
	return nil
}

func (f *fakeNetboxAPI) AddressDelete(addressID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedAddressIDs = append(f.deletedAddressIDs, uint(addressID))
	return nil
}

func (f *fakeNetboxAPI) GetCable(cableID uint) (*netboxtool.NBCable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cablesByID[cableID], nil
}

func (f *fakeNetboxAPI) CreateCableWithOptions(aInterfaceID, bInterfaceID uint, extra map[string]any) (*netboxtool.NBCable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdCables = append(f.createdCables, [2]uint{aInterfaceID, bInterfaceID})
	f.nextID++
	label := ""
	if extra != nil {
		label, _ = extra["label"].(string)
	}
	cable := &netboxtool.NBCable{NetboxID: f.nextID, AInterface: aInterfaceID, BInterface: bInterfaceID, Label: label}
	f.cablesByID[cable.NetboxID] = cable
	return cable, nil
}

func (f *fakeNetboxAPI) DeleteCable(cableID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedCableIDs = append(f.deletedCableIDs, cableID)
	delete(f.cablesByID, cableID)
	return nil
}

func (f *fakeNetboxAPI) UpdateCableTermination(cableID uint, side int, interfaceID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminationUpdates = append(f.terminationUpdates, cableTerminationUpdate{cableID, side, interfaceID})
	if cable, ok := f.cablesByID[cableID]; ok {
		if side == 1 {
			cable.AInterface = interfaceID
		} else {
			cable.BInterface = interfaceID
		}
	}
	return nil
}

func (f *fakeNetboxAPI) GetPrefix(prefix string) (*netboxtool.NBPrefix, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prefixesByCIDR[prefix], nil
}

func (f *fakeNetboxAPI) CreatePrefix(prefix string) (*netboxtool.NBPrefix, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdPrefixes = append(f.createdPrefixes, prefix)
	p := &netboxtool.NBPrefix{Prefix: prefix, Status: "active"}
	f.prefixesByCIDR[prefix] = p
	return p, nil
}

func (f *fakeNetboxAPI) GetVlanGroup(name string) (*netboxtool.NBVlanGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vlanGroupsByName[name], nil
}

func (f *fakeNetboxAPI) CreateVlanGroup(name, slug string) (*netboxtool.NBVlanGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.createdVlanGroups = append(f.createdVlanGroups, name)
	g := &netboxtool.NBVlanGroup{NetboxID: f.nextID, Name: name, Slug: slug}
	f.vlanGroupsByName[name] = g
	return g, nil
}

func (f *fakeNetboxAPI) GetVlan(vid int, groupID uint) (*netboxtool.NBVlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vlansByGroupAndVID[groupID][vid], nil
}

func (f *fakeNetboxAPI) CreateVlan(vid int, name string, groupID uint) (*netboxtool.NBVlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	v := &netboxtool.NBVlan{NetboxID: f.nextID, VID: vid, Name: name, GroupID: groupID}
	f.createdVlans = append(f.createdVlans, v)
	if f.vlansByGroupAndVID[groupID] == nil {
		f.vlansByGroupAndVID[groupID] = map[int]*netboxtool.NBVlan{}
	}
	f.vlansByGroupAndVID[groupID][vid] = v
	return v, nil
}

func (f *fakeNetboxAPI) UpdateVlan(id uint, changes map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedVlans[id] = changes
	for _, byVID := range f.vlansByGroupAndVID {
		for _, v := range byVID {
			if v.NetboxID == id {
				if name, ok := changes["name"].(string); ok {
					v.Name = name
				}
			}
		}
	}
	return nil
}

func (f *fakeNetboxAPI) GetL2VPNByName(name string) (*netboxtool.NBL2VPN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.l2vpnsByName[name], nil
}

func (f *fakeNetboxAPI) GetL2VPNByIdentifier(identifier int) (*netboxtool.NBL2VPN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if identifier == 0 {
		return nil, nil
	}
	return f.l2vpnsByIdentifier[identifier], nil
}

func (f *fakeNetboxAPI) CreateL2VPN(name, slug, l2vpnType string, identifier int) (*netboxtool.NBL2VPN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	l := &netboxtool.NBL2VPN{
		NetboxID:   f.nextID,
		Name:       name,
		Slug:       slug,
		Type:       l2vpnType,
		Identifier: identifier,
	}
	f.createdL2VPNs = append(f.createdL2VPNs, l)
	f.l2vpnsByName[name] = l
	f.l2vpnsByID[l.NetboxID] = l
	if identifier > 0 {
		f.l2vpnsByIdentifier[identifier] = l
	}
	return l, nil
}

func (f *fakeNetboxAPI) UpdateL2VPN(l2vpnID uint, changes map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedL2VPNs[l2vpnID] = changes
	l, ok := f.l2vpnsByID[l2vpnID]
	if !ok {
		return nil
	}
	if id, ok := changes["identifier"].(int); ok {
		if l.Identifier > 0 {
			delete(f.l2vpnsByIdentifier, l.Identifier)
		}
		l.Identifier = id
		if id > 0 {
			f.l2vpnsByIdentifier[id] = l
		}
	}
	return nil
}

func (f *fakeNetboxAPI) GetL2VPNTerminations(l2vpnID uint) ([]*netboxtool.NBL2VPNTermination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.terminationsByL2VPN[l2vpnID]
	out := make([]*netboxtool.NBL2VPNTermination, len(src))
	copy(out, src)
	return out, nil
}

func (f *fakeNetboxAPI) CreateL2VPNTermination(l2vpnID, interfaceID uint) (*netboxtool.NBL2VPNTermination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	t := &netboxtool.NBL2VPNTermination{
		NetboxID:    f.nextID,
		L2VPNID:     l2vpnID,
		InterfaceID: interfaceID,
	}
	f.createdTerminations = append(f.createdTerminations, [2]uint{l2vpnID, interfaceID})
	f.terminationsByL2VPN[l2vpnID] = append(f.terminationsByL2VPN[l2vpnID], t)
	return t, nil
}

// fakeFactumAPI is a minimal in-memory FactumAPI, recording every call so
// tests can assert on what device-sync read from factum without a real
// factum2-web. Locked for the same reason as fakeNetboxAPI.
type fakeFactumAPI struct {
	mu sync.Mutex

	devices map[string]*models.Device

	getDevicesCalls      int
	getDeviceByNameCalls []string
}

func newFakeFactumAPI() *fakeFactumAPI {
	return &fakeFactumAPI{devices: map[string]*models.Device{}}
}

var _ FactumAPI = (*fakeFactumAPI)(nil)

func (f *fakeFactumAPI) GetDevices() ([]*models.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getDevicesCalls++
	out := make([]*models.Device, 0, len(f.devices))
	for _, d := range f.devices {
		out = append(out, d)
	}
	return out, nil
}

// GetDeviceByName errors on an unknown name, matching
// internal/factum.FactumClient.GetDeviceByName.
func (f *fakeFactumAPI) GetDeviceByName(name string) (*models.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getDeviceByNameCalls = append(f.getDeviceByNameCalls, name)
	d, ok := f.devices[name]
	if !ok {
		return nil, fmt.Errorf("unknown device %q", name)
	}
	return d, nil
}

// fakeDriver only implements GetNeighbors - the rest is promoted (and would
// panic if called) from the nil embedded drivers.DriverClient, same
// zero-cost-embedding pattern the real drivers (AristaDriver etc) use.
type fakeDriver struct {
	drivers.DriverClient
	neighbors []*drivers.Neighbor
}

func (f *fakeDriver) GetNeighbors() ([]*drivers.Neighbor, error) {
	return f.neighbors, nil
}

func newTestDeviceSync(fakeNB *fakeNetboxAPI, fakeFactum *fakeFactumAPI, cfg *util.ConfigDeviceSync) (*DeviceSync, *testReporter) {
	if cfg == nil {
		cfg = &util.ConfigDeviceSync{}
	}
	reporter := &testReporter{}
	return &DeviceSync{
		nb:          NewNetboxMgr(fakeNB, reporter),
		factum:      fakeFactum,
		cfg:         cfg,
		reporter:    reporter,
		opts:        SyncOptions{Unattended: true},
		deviceCache: map[string]*models.Device{},
	}, reporter
}

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", s, err)
	}
	return p
}

// ----- device selection -----

// TestDevicesToSyncFetchesOnlyNamedDevice covers the --name case: run()
// used to always call GetDevices() (every device in factum) and filter
// client-side, even when only one device's name was requested. devicesToSync
// instead does a single server-side-filtered GetDeviceByName(name) lookup -
// this pins down that GetDevices() is never called when opts.Name is set.
func TestDevicesToSyncFetchesOnlyNamedDevice(t *testing.T) {
	fakeFactum := newFakeFactumAPI()
	fakeFactum.devices["sw1"] = &models.Device{NetboxID: 1, Name: "sw1"}
	fakeFactum.devices["sw2"] = &models.Device{NetboxID: 2, Name: "sw2"}

	ds, _ := newTestDeviceSync(newFakeNetboxAPI(), fakeFactum, nil)
	ds.opts.Name = "sw1"

	devices, err := ds.devicesToSync()
	if err != nil {
		t.Fatalf("devicesToSync: %v", err)
	}
	if len(devices) != 1 || devices[0].Name != "sw1" {
		t.Fatalf("devices = %v, want just [sw1]", devices)
	}
	if fakeFactum.getDevicesCalls != 0 {
		t.Errorf("GetDevices() called %d times, want 0 - a --name run shouldn't fetch every device", fakeFactum.getDevicesCalls)
	}
	if len(fakeFactum.getDeviceByNameCalls) != 1 || fakeFactum.getDeviceByNameCalls[0] != "sw1" {
		t.Errorf("GetDeviceByName calls = %v, want [sw1]", fakeFactum.getDeviceByNameCalls)
	}
}

func TestDevicesToSyncFetchesAllWhenNameEmpty(t *testing.T) {
	fakeFactum := newFakeFactumAPI()
	fakeFactum.devices["sw1"] = &models.Device{NetboxID: 1, Name: "sw1"}
	ds, _ := newTestDeviceSync(newFakeNetboxAPI(), fakeFactum, nil)

	devices, err := ds.devicesToSync()
	if err != nil {
		t.Fatalf("devicesToSync: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %v, want 1", devices)
	}
	if fakeFactum.getDevicesCalls != 1 {
		t.Errorf("GetDevices() called %d times, want 1", fakeFactum.getDevicesCalls)
	}
}

func TestDevicesToSyncUnknownNameReportsError(t *testing.T) {
	ds, reporter := newTestDeviceSync(newFakeNetboxAPI(), newFakeFactumAPI(), nil)
	ds.opts.Name = "does-not-exist"

	devices, err := ds.devicesToSync()
	if err != nil {
		t.Fatalf("devicesToSync: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("devices = %v, want none", devices)
	}
	found := false
	for _, line := range reporter.lines {
		if strings.Contains(line, "does-not-exist") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error mentioning the unknown device name; lines: %v", reporter.lines)
	}
}

// ----- interfaces -----

func TestInterfacesCreateAndDelete(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "Ethernet1"},
		{NetboxID: 11, Name: "Ethernet2"}, // stale, not in device config -> delete
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", Type: "virtual"})
	dc.AddInterface(&drivers.Interface{Name: "Ethernet3", Type: "virtual"}) // new -> create

	pair := &devicePair{nbDevice: nbDevice, config: dc}
	ds.interfacesDelete(pair)
	ds.interfacesCreate(pair)

	if len(fake.deletedInterfaceIDs) != 1 || fake.deletedInterfaceIDs[0] != 11 {
		t.Errorf("deletedInterfaceIDs = %v, want [11]", fake.deletedInterfaceIDs)
	}
	if len(fake.createdInterfaces) != 1 || fake.createdInterfaces[0] != "Ethernet3" {
		t.Errorf("createdInterfaces = %v, want [Ethernet3]", fake.createdInterfaces)
	}
}

// TestInterfacesCreateAppendsInMemoryForAddressSync covers the gap
// RefreshDevice used to paper over: since reads now come from factum
// (cron-synced, not live), interfacesCreate must record the newly created
// interface directly on pair.nbDevice so addressesCreate (next phase, same
// run) can find it.
func TestInterfacesCreateAppendsInMemoryForAddressSync(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1"}
	nbDevice.ID = 1
	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", Type: "virtual", VRF: "MGMT", IPAddresses: []drivers.InterfaceAddress{
		{Address: mustPrefix(t, "10.0.0.2/24")},
	}})

	pair := &devicePair{nbDevice: nbDevice, config: dc}
	ds.interfacesCreate(pair)
	if len(pair.nbDevice.Interfaces) != 1 || pair.nbDevice.Interfaces[0].Name != "Ethernet1" {
		t.Fatalf("pair.nbDevice.Interfaces = %+v, want the just-created Ethernet1 recorded in-memory", pair.nbDevice.Interfaces)
	}

	ds.addressesCreate(pair)
	if len(fake.createdAddresses) != 1 || fake.createdAddresses[0] != "10.0.0.2/24" {
		t.Fatalf("createdAddresses = %v, want [10.0.0.2/24] - addressesCreate should have found the in-memory interface", fake.createdAddresses)
	}
}

// TestInterfacesCreateResolvesParentAndLabel covers SR OS SAP interfaces
// (see srosAddSapInterface): Parent must resolve to the already-existing
// physical port's Netbox ID and Label must be sent through as-is.
func TestInterfacesCreateResolvesParentAndLabel(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "1/1/1"},
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "1/1/1:3704.*", Type: "virtual", Parent: "1/1/1", Label: "SAP"})

	ds.interfacesCreate(&devicePair{nbDevice: nbDevice, config: dc})

	extra := fake.createdInterfaceExtras["1/1/1:3704.*"]
	if extra == nil {
		t.Fatalf("no CreateInterfaceWithOptions call recorded for 1/1/1:3704.*")
	}
	if extra["label"] != "SAP" {
		t.Errorf("label = %v, want SAP", extra["label"])
	}
	if extra["parent"] != uint(10) {
		t.Errorf("parent = %v, want the netbox id of 1/1/1 (10)", extra["parent"])
	}
}

// TestInterfacesCreateDropsUnresolvedParent covers a Parent that doesn't
// exist in Netbox (e.g. the port itself hasn't been synced yet): "parent"
// must be left out of the payload rather than sent as a zero id.
func TestInterfacesCreateDropsUnresolvedParent(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, reporter := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1"}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "1/1/1:3704.*", Type: "virtual", Parent: "1/1/1", Label: "SAP"})

	ds.interfacesCreate(&devicePair{nbDevice: nbDevice, config: dc})

	extra := fake.createdInterfaceExtras["1/1/1:3704.*"]
	if extra == nil {
		t.Fatalf("no CreateInterfaceWithOptions call recorded for 1/1/1:3704.*")
	}
	if _, ok := extra["parent"]; ok {
		t.Errorf("parent = %v, want unset since 1/1/1 doesn't exist in netbox", extra["parent"])
	}
	if len(reporter.lines) == 0 {
		t.Errorf("expected a warning reported for the unresolved parent")
	}
}

// TestInterfacesCreateUsesDeviceTemplateType covers the requested behavior:
// when the interface being created is defined by the device type's
// template, its admin/vendor-curated type (e.g. "1000base-t") is used
// instead of the driver's guessed type (e.g. "other").
func TestInterfacesCreateUsesDeviceTemplateType(t *testing.T) {
	fake := newFakeNetboxAPI()
	fake.deviceTypes["Cisco/NCS-5501"] = &netboxtool.NetboxDeviceTypeDetail{
		Interfaces: []netboxtool.NetboxInterfaceTemplate{
			{Name: "GigabitEthernet0/0/0/0", Type: "1000base-t"},
		},
	}
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "rtr1", Manufacturer: "Cisco", ModelName: "NCS-5501"}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "GigabitEthernet0/0/0/0", Type: "other"}) // driver's guess

	ds.interfacesCreate(&devicePair{nbDevice: nbDevice, config: dc})

	extra := fake.createdInterfaceExtras["GigabitEthernet0/0/0/0"]
	if extra == nil {
		t.Fatalf("no CreateInterfaceWithOptions call recorded for GigabitEthernet0/0/0/0")
	}
	if extra["type"] != "1000base-t" {
		t.Errorf("type = %v, want template's 1000base-t (not the driver's guessed %q)", extra["type"], "other")
	}
}

func TestInterfacesDeleteSkipsDeviceTemplateInterfaces(t *testing.T) {
	fake := newFakeNetboxAPI()
	fake.deviceTypes["Cisco/NCS-5501"] = &netboxtool.NetboxDeviceTypeDetail{
		Interfaces: []netboxtool.NetboxInterfaceTemplate{
			{Name: "MgmtEth0/RP0/CPU0/0"}, // template-defined -> must survive
		},
	}
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{
		NetboxID: 1, Name: "rtr1", Manufacturer: "Cisco", ModelName: "NCS-5501",
		Interfaces: []models.Interface{
			{NetboxID: 10, Name: "MgmtEth0/RP0/CPU0/0"},    // in template -> keep
			{NetboxID: 11, Name: "GigabitEthernet0/0/0/5"}, // not in template -> delete
		},
	}

	ds.interfacesDelete(&devicePair{nbDevice: nbDevice, config: drivers.NewDeviceConfig()})

	if len(fake.deletedInterfaceIDs) != 1 || fake.deletedInterfaceIDs[0] != 11 {
		t.Errorf("deletedInterfaceIDs = %v, want [11] (MgmtEth0/RP0/CPU0/0 is template-defined and must survive)", fake.deletedInterfaceIDs)
	}
}

func TestInterfacesDeleteProceedsWhenTemplateLookupFails(t *testing.T) {
	fake := newFakeNetboxAPI() // no device type registered -> GetDeviceType errors
	ds, reporter := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{
		NetboxID: 1, Name: "rtr1", Manufacturer: "Cisco", ModelName: "Unknown",
		Interfaces: []models.Interface{
			{NetboxID: 10, Name: "GigabitEthernet0/0/0/5"},
		},
	}

	ds.interfacesDelete(&devicePair{nbDevice: nbDevice, config: drivers.NewDeviceConfig()})

	if len(fake.deletedInterfaceIDs) != 1 || fake.deletedInterfaceIDs[0] != 10 {
		t.Errorf("deletedInterfaceIDs = %v, want [10] (should proceed unfiltered when the template lookup fails)", fake.deletedInterfaceIDs)
	}
	if len(reporter.lines) == 0 {
		t.Error("expected a warning to be reported when the template lookup fails")
	}
}

func TestSyncInterfaceValues(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "Ethernet1", Description: "old descr", VRF: "OLD"},
		{NetboxID: 11, Name: "Ethernet2", Description: "unchanged"}, // no VRF on netbox side -> never set from empty
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", Description: "new descr", VRF: "NEW"})
	dc.AddInterface(&drivers.Interface{Name: "Ethernet2", Description: "unchanged", VRF: "SHOULD-NOT-APPLY"})

	pair := &devicePair{nbDevice: nbDevice, config: dc}
	ds.syncInterfaceValues(pair)

	if len(fake.updatedInterfaces) != 1 {
		t.Fatalf("got %d interface updates, want 1 (only Ethernet1 changed): %+v", len(fake.updatedInterfaces), fake.updatedInterfaces)
	}
	changes := fake.updatedInterfaces[0]
	if changes["description"] != "new descr" {
		t.Errorf("description change = %v", changes["description"])
	}
	if vrf, ok := changes["vrf"].(map[string]string); !ok || vrf["name"] != "NEW" {
		t.Errorf("vrf change = %v", changes["vrf"])
	}
}

// TestSyncInterfaceValuesCorrectsTypeFromTemplate covers the requested
// behavior: when an existing Netbox interface's type differs from the
// device type template's type for that interface name, it gets corrected
// to the template's value.
func TestSyncInterfaceValuesCorrectsTypeFromTemplate(t *testing.T) {
	fake := newFakeNetboxAPI()
	fake.deviceTypes["Cisco/NCS-5501"] = &netboxtool.NetboxDeviceTypeDetail{
		Interfaces: []netboxtool.NetboxInterfaceTemplate{
			{Name: "GigabitEthernet0/0/0/0", Type: "1000base-t"},
		},
	}
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{
		NetboxID: 1, Name: "rtr1", Manufacturer: "Cisco", ModelName: "NCS-5501",
		Interfaces: []models.Interface{
			// Wrong type in Netbox (e.g. from before the type-guessing fix).
			{NetboxID: 10, Name: "GigabitEthernet0/0/0/0", Type: "virtual"},
		},
	}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "GigabitEthernet0/0/0/0"})

	ds.syncInterfaceValues(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 1 {
		t.Fatalf("got %d interface updates, want 1: %+v", len(fake.updatedInterfaces), fake.updatedInterfaces)
	}
	if got := fake.updatedInterfaces[0]["type"]; got != "1000base-t" {
		t.Errorf("type change = %v, want 1000base-t", got)
	}
}

// TestSyncInterfaceValuesCorrectsLabelAndParent covers SR OS SAP interfaces
// (see srosAddSapInterface): an existing Netbox interface missing/wrong on
// label or parent gets corrected, while an interface the driver has no
// opinion about (Label/Parent both "") is left alone even if Netbox already
// has a label set on it.
func TestSyncInterfaceValuesCorrectsLabelAndParent(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "1/1/1"},                                             // the SAP's parent port, already in netbox
		{NetboxID: 20, Name: "1/1/1:3704.*", Label: "", ParentID: 0},              // SAP, missing label/parent -> corrected
		{NetboxID: 30, Name: "1/1/5:*", Label: "SAP", ParentID: 10},               // already correct -> no update
		{NetboxID: 40, Name: "Ethernet1", Label: "admin-set label", ParentID: 99}, // no driver opinion -> left alone
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "1/1/1", Type: "other"})
	dc.AddInterface(&drivers.Interface{Name: "1/1/1:3704.*", Type: "virtual", Parent: "1/1/1", Label: "SAP"})
	dc.AddInterface(&drivers.Interface{Name: "1/1/5:*", Type: "virtual", Parent: "1/1/1", Label: "SAP"})
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", Type: "virtual"})

	ds.syncInterfaceValues(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 1 {
		t.Fatalf("got %d interface updates, want 1 (only 1/1/1:3704.*): %+v", len(fake.updatedInterfaces), fake.updatedInterfaces)
	}
	changes := fake.updatedInterfaces[0]
	if changes["label"] != "SAP" {
		t.Errorf("label change = %v, want SAP", changes["label"])
	}
	if changes["parent"] != uint(10) {
		t.Errorf("parent change = %v, want 10 (1/1/1's netbox id)", changes["parent"])
	}
}

// TestSyncInterfaceValuesWarnsOnUnresolvedParent covers a driver-asserted
// Parent that doesn't exist in Netbox yet - the update must still go
// through with whatever other fields changed, minus "parent", with a
// warning reported rather than silently sending a zero id.
func TestSyncInterfaceValuesWarnsOnUnresolvedParent(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, reporter := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 20, Name: "1/1/1:3704.*"},
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "1/1/1:3704.*", Type: "virtual", Parent: "1/1/1", Label: "SAP"})

	ds.syncInterfaceValues(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 1 {
		t.Fatalf("got %d interface updates, want 1: %+v", len(fake.updatedInterfaces), fake.updatedInterfaces)
	}
	if _, ok := fake.updatedInterfaces[0]["parent"]; ok {
		t.Errorf("parent change = %v, want unset since 1/1/1 isn't in netbox", fake.updatedInterfaces[0]["parent"])
	}
	if len(reporter.lines) == 0 {
		t.Error("expected a warning reported for the unresolved parent")
	}
}

// ----- addresses -----

func TestAddressesDelete(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "Ethernet1", Addresses: []models.Address{
			{NetboxID: 100, Address: "10.0.0.1/24"},  // still on device -> kept
			{NetboxID: 101, Address: "10.0.0.99/24"}, // gone from device -> deleted
		}},
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", IPAddresses: []drivers.InterfaceAddress{
		{Address: mustPrefix(t, "10.0.0.1/24")},
	}})

	pair := &devicePair{nbDevice: nbDevice, config: dc}
	ds.addressesDelete(pair)

	if len(fake.deletedAddressIDs) != 1 || fake.deletedAddressIDs[0] != 101 {
		t.Errorf("deletedAddressIDs = %v, want [101]", fake.deletedAddressIDs)
	}
	if got := pair.nbDevice.Interfaces[0].Addresses; len(got) != 1 || got[0].NetboxID != 100 {
		t.Errorf("pair.nbDevice addresses after delete = %+v, want just netbox_id=100 kept in-memory", got)
	}
}

// TestAddressesDeleteReportsVRFMismatch covers the case that prompted
// reportAddressDeleteReason: an address that's actually present on the
// device, but under a different VRF than Netbox has it in - addressMatch
// correctly treats that as "not found" (it's not safe to assume they're the
// same address), but the reported reason must say so plainly (both sides'
// VRFs) rather than just "deleting", so a real VRF-string mismatch is
// diagnosable from the log alone instead of looking like the address was
// simply removed from the device.
func TestAddressesDeleteReportsVRFMismatch(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, reporter := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "alk-ce1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "Port-Channel2.820", Addresses: []models.Address{
			{NetboxID: 100, Address: "82.99.65.49/30", VRF: "AL-DCBD-1"},
		}},
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Port-Channel2.820", VRF: "AL-DCBD-1", IPAddresses: []drivers.InterfaceAddress{
		// Same address, but parsed with no VRF - simulates the device-side
		// VRF not matching Netbox's, rather than the address being gone.
		{Address: mustPrefix(t, "82.99.65.49/30")},
	}})

	ds.addressesDelete(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.deletedAddressIDs) != 1 || fake.deletedAddressIDs[0] != 100 {
		t.Fatalf("deletedAddressIDs = %v, want [100]", fake.deletedAddressIDs)
	}

	var reason string
	for _, line := range reporter.lines {
		if strings.Contains(line, "82.99.65.49/30") {
			reason = line
			break
		}
	}
	if reason == "" {
		t.Fatalf("no reported reason mentioning the address; lines: %v", reporter.lines)
	}
	for _, want := range []string{"vrf=AL-DCBD-1", "vrf=-"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reported reason %q missing %q - both sides' VRF should be visible", reason, want)
		}
	}
}

func TestAddressesCreate(t *testing.T) {
	fake := newFakeNetboxAPI()
	cfg := &util.ConfigDeviceSync{VRFInGlobal: []string{"MGMT"}}
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), cfg)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "Ethernet1", Description: "anycast VIP"},
	}}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", VRF: "MGMT", IPAddresses: []drivers.InterfaceAddress{
		{Address: mustPrefix(t, "10.0.0.2/24"), VRF: "MGMT"}, // MGMT is in vrf_in_global -> no vrf sent
	}})

	ds.addressesCreate(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.createdAddresses) != 1 || fake.createdAddresses[0] != "10.0.0.2/24" {
		t.Fatalf("createdAddresses = %v, want [10.0.0.2/24]", fake.createdAddresses)
	}
}

func TestAddressesUpdateTogglesAnycastRole(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	nbDevice := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 10, Name: "Loopback0", Description: "ANYCAST vip", Addresses: []models.Address{
			{NetboxID: 100, Address: "10.0.0.1/32"}, // no role yet, should become anycast
		}},
		{NetboxID: 11, Name: "Ethernet1", Description: "regular link", Addresses: []models.Address{
			{NetboxID: 101, Address: "10.0.0.2/24", Role: "anycast"}, // stale role, should be cleared
		}},
	}}

	ds.addressesUpdate(&devicePair{nbDevice: nbDevice, config: drivers.NewDeviceConfig()})

	if len(fake.updatedAddresses) != 2 {
		t.Fatalf("got %d address updates, want 2: %+v", len(fake.updatedAddresses), fake.updatedAddresses)
	}
	if fake.updatedAddresses[0]["role"] != "anycast" {
		t.Errorf("first update = %v, want role=anycast", fake.updatedAddresses[0])
	}
	if fake.updatedAddresses[1]["role"] != nil {
		t.Errorf("second update = %v, want role=nil (cleared)", fake.updatedAddresses[1])
	}
	// Role changes are mutated in-memory (no re-fetch) - see addressesUpdate.
	if nbDevice.Interfaces[0].Addresses[0].Role != "anycast" {
		t.Errorf("in-memory role = %q, want anycast", nbDevice.Interfaces[0].Addresses[0].Role)
	}
	if nbDevice.Interfaces[1].Addresses[0].Role != "" {
		t.Errorf("in-memory role = %q, want cleared", nbDevice.Interfaces[1].Addresses[0].Role)
	}
}

// ----- addressMatch / normalizeVRF -----

func TestAddressMatchVRFInGlobal(t *testing.T) {
	vrfInGlobal := []string{"MGMT"}
	a := mustPrefix(t, "10.0.0.1/24")
	b := mustPrefix(t, "10.0.0.1/24")

	if !addressMatch(a, "MGMT", b, "", vrfInGlobal) {
		t.Error("MGMT (global) should match no-VRF for the same address")
	}
	if addressMatch(a, "CUSTOMER1", b, "CUSTOMER2", vrfInGlobal) {
		t.Error("two different non-global VRFs should not match")
	}
	c := mustPrefix(t, "10.0.0.2/24")
	if addressMatch(a, "", c, "", vrfInGlobal) {
		t.Error("different addresses should never match")
	}
}

// ----- connections -----

func TestSyncConnectionCreatesCableWhenNoneExists(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	device := &models.Device{NetboxID: 1, Name: "sw1"}
	remote := &models.Device{NetboxID: 2, Name: "sw2"}
	localIf := &models.Interface{NetboxID: 100, Name: "Ethernet1"}
	remoteIf := &models.Interface{NetboxID: 200, Name: "Ethernet2"}

	ds.syncConnection(device, remote, localIf, remoteIf)

	if len(fake.createdCables) != 1 || fake.createdCables[0] != [2]uint{100, 200} {
		t.Fatalf("createdCables = %v, want [[100 200]]", fake.createdCables)
	}
	var created *netboxtool.NBCable
	for _, c := range fake.cablesByID {
		created = c
	}
	if created == nil {
		t.Fatal("expected a created cable in cablesByID")
	}
	if created.Label != netbox.CableLabelLLDP {
		t.Fatalf("created cable label = %q, want %q", created.Label, netbox.CableLabelLLDP)
	}
}

func TestSyncConnectionLeavesCorrectCableAlone(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	localIf := &models.Interface{NetboxID: 100, Name: "Ethernet1", CableID: 5}
	remoteIf := &models.Interface{NetboxID: 200, Name: "Ethernet2"}
	fake.cablesByID[5] = &netboxtool.NBCable{NetboxID: 5, AInterface: 100, BInterface: 200, Label: netbox.CableLabelLLDP}

	device := &models.Device{NetboxID: 1, Name: "sw1"}
	remote := &models.Device{NetboxID: 2, Name: "sw2"}
	ds.syncConnection(device, remote, localIf, remoteIf)

	if len(fake.createdCables) != 0 || len(fake.terminationUpdates) != 0 || len(fake.deletedCableIDs) != 0 {
		t.Errorf("expected no changes for an already-correct cable, got created=%v updates=%v deleted=%v",
			fake.createdCables, fake.terminationUpdates, fake.deletedCableIDs)
	}
}

func TestSyncConnectionRepairsWrongTermination(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	// Cable exists, A side is correct (localIf), but B side points at some
	// other (stale) interface instead of remoteIf.
	localIf := &models.Interface{NetboxID: 100, Name: "Ethernet1", CableID: 5}
	remoteIf := &models.Interface{NetboxID: 200, Name: "Ethernet2"}
	fake.cablesByID[5] = &netboxtool.NBCable{NetboxID: 5, AInterface: 100, BInterface: 999, Label: netbox.CableLabelLLDP}

	device := &models.Device{NetboxID: 1, Name: "sw1"}
	remote := &models.Device{NetboxID: 2, Name: "sw2"}
	ds.syncConnection(device, remote, localIf, remoteIf)

	if len(fake.terminationUpdates) != 1 {
		t.Fatalf("got %d termination updates, want 1: %+v", len(fake.terminationUpdates), fake.terminationUpdates)
	}
	got := fake.terminationUpdates[0]
	if got.cableID != 5 || got.side != 2 || got.ifaceID != 200 {
		t.Errorf("termination update = %+v, want {cableID:5 side:2 ifaceID:200}", got)
	}
}

func TestSyncConnectionLeavesManualCableAlone(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	localIf := &models.Interface{NetboxID: 100, Name: "Ethernet1", CableID: 5}
	remoteIf := &models.Interface{NetboxID: 200, Name: "Ethernet2"}
	fake.cablesByID[5] = &netboxtool.NBCable{NetboxID: 5, AInterface: 100, BInterface: 999} // unlabeled = manual

	device := &models.Device{NetboxID: 1, Name: "sw1"}
	remote := &models.Device{NetboxID: 2, Name: "sw2"}
	ds.syncConnection(device, remote, localIf, remoteIf)

	if len(fake.terminationUpdates) != 0 || len(fake.deletedCableIDs) != 0 || len(fake.createdCables) != 0 {
		t.Errorf("manual cable must not be mutated, got updates=%v deleted=%v created=%v",
			fake.terminationUpdates, fake.deletedCableIDs, fake.createdCables)
	}
}

func TestSyncConnectionSkipsOpticalDevice(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	device := &models.Device{NetboxID: 1, Name: "pe1"}
	remote := &models.Device{NetboxID: 2, Name: "dcp2", OpticalKind: models.OpticalKindWDMShelf}
	localIf := &models.Interface{NetboxID: 100, Name: "Ethernet1"}
	remoteIf := &models.Interface{NetboxID: 200, Name: "C1"}
	ds.syncConnection(device, remote, localIf, remoteIf)
	if len(fake.createdCables) != 0 {
		t.Errorf("createdCables = %v, want none (optical far end)", fake.createdCables)
	}
}

func TestSyncConnectionsEndToEnd(t *testing.T) {
	fake := newFakeNetboxAPI()
	fakeFactum := newFakeFactumAPI()
	ds, _ := newTestDeviceSync(fake, fakeFactum, nil)

	device := &models.Device{NetboxID: 1, Name: "sw1", Interfaces: []models.Interface{
		{NetboxID: 100, Name: "Ethernet1"},
	}}
	remote := &models.Device{NetboxID: 2, Name: "sw2.example.com", Interfaces: []models.Interface{
		{NetboxID: 200, Name: "Ethernet2"},
	}}
	fakeFactum.devices["sw2"] = remote // LLDP reports the FQDN; lookup is by hostname

	pair := &devicePair{
		nbDevice: device,
		config:   drivers.NewDeviceConfig(),
		driver: &fakeDriver{neighbors: []*drivers.Neighbor{
			{LocalInterface: "Ethernet1", RemoteName: "SW2.EXAMPLE.COM", RemoteInterface: "Ethernet2"},
		}},
	}
	ds.syncConnections(pair)

	if len(fake.createdCables) != 1 || fake.createdCables[0] != [2]uint{100, 200} {
		t.Fatalf("createdCables = %v, want [[100 200]]", fake.createdCables)
	}
}

// ----- prefixes -----

func TestSyncPrefixesCreatesMissingSkipsHostRoutes(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", IPAddresses: []drivers.InterfaceAddress{
		{Address: mustPrefix(t, "10.0.0.5/24")},  // -> 10.0.0.0/24, should be created
		{Address: mustPrefix(t, "10.0.0.6/24")},  // same network, should be de-duped
		{Address: mustPrefix(t, "192.0.2.1/32")}, // host route, skipped
	}})
	fake.prefixesByCIDR["10.0.0.0/24"] = nil // not actually present, just documents intent

	ds.syncPrefixes(&devicePair{nbDevice: &models.Device{Name: "sw1"}, config: dc})

	if len(fake.createdPrefixes) != 1 || fake.createdPrefixes[0] != "10.0.0.0/24" {
		t.Fatalf("createdPrefixes = %v, want [10.0.0.0/24]", fake.createdPrefixes)
	}
}

func TestSyncPrefixesSkipsExisting(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)
	fake.prefixesByCIDR["10.0.0.0/24"] = &netboxtool.NBPrefix{Prefix: "10.0.0.0/24"}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", IPAddresses: []drivers.InterfaceAddress{
		{Address: mustPrefix(t, "10.0.0.5/24")},
	}})

	ds.syncPrefixes(&devicePair{nbDevice: &models.Device{Name: "sw1"}, config: dc})

	if len(fake.createdPrefixes) != 0 {
		t.Errorf("createdPrefixes = %v, want none (already exists)", fake.createdPrefixes)
	}
}

// ----- Vlans -----

func TestSyncVlansCreatesAndRenamesGroup(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), &util.ConfigDeviceSync{VlanGroupName: "Global VLANs"})

	if _, err := ds.nb.EnsureVlanGroup(ds.cfg.VlanGroupName); err != nil {
		t.Fatalf("EnsureVlanGroup: %v", err)
	}
	if len(fake.createdVlanGroups) != 1 || fake.createdVlanGroups[0] != "Global VLANs" {
		t.Fatalf("createdVlanGroups = %v, want [Global VLANs]", fake.createdVlanGroups)
	}

	dc := drivers.NewDeviceConfig()
	dc.GlobalVLANs[10] = &drivers.VLAN{ID: 10, Name: "servers"}
	ds.syncVlans(&devicePair{nbDevice: &models.Device{Name: "sw1"}, config: dc})

	if len(fake.createdVlans) != 1 || fake.createdVlans[0].VID != 10 || fake.createdVlans[0].Name != "servers" {
		t.Fatalf("createdVlans = %+v, want one vlan 10 (servers)", fake.createdVlans)
	}

	// A second device reporting the same VID with a changed name should
	// rename the existing vlan rather than create a duplicate.
	dc2 := drivers.NewDeviceConfig()
	dc2.GlobalVLANs[10] = &drivers.VLAN{ID: 10, Name: "renamed"}
	ds.syncVlans(&devicePair{nbDevice: &models.Device{Name: "sw2"}, config: dc2})

	if len(fake.createdVlans) != 1 {
		t.Fatalf("createdVlans = %+v, want still just one (renamed, not duplicated)", fake.createdVlans)
	}
	if len(fake.updatedVlans) != 1 {
		t.Fatalf("updatedVlans = %v, want one name update", fake.updatedVlans)
	}
}

// TestSyncVlansCoversUndeclaredTaggedVid covers a real failure seen against
// a VRP device: a trunk's "port trunk allow-pass vlan" list can reference a
// VID that was never declared via its own "vlan" block (so it never shows
// up in GlobalVLANs) - syncVlans must still create a Netbox vlan for it
// (with a generated fallback name) so syncInterfaceVlans can later resolve
// it, instead of leaving it permanently unresolvable.
func TestSyncVlansCoversUndeclaredTaggedVid(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), &util.ConfigDeviceSync{VlanGroupName: "Global VLANs"})
	if _, err := ds.nb.EnsureVlanGroup(ds.cfg.VlanGroupName); err != nil {
		t.Fatalf("EnsureVlanGroup: %v", err)
	}

	dc := drivers.NewDeviceConfig()
	dc.GlobalVLANs[10] = &drivers.VLAN{ID: 10, Name: "servers"}
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", SwitchportMode: "trunk", TaggedVLANs: []int{10, 2069}})
	ds.syncVlans(&devicePair{nbDevice: &models.Device{Name: "sw1"}, config: dc})

	if _, ok := ds.nb.VlanNetboxID(2069); !ok {
		t.Fatalf("vlan 2069 (undeclared, only referenced by a trunk) was not synced to netbox")
	}
	if len(fake.createdVlans) != 2 {
		t.Fatalf("createdVlans = %+v, want 2 (declared vlan 10 + undeclared vlan 2069)", fake.createdVlans)
	}
}

// TestSyncVlansFallsBackToGeneratedNameWhenBlank covers a real failure seen
// against a VRP device: "vlan batch <id-list>" declares a vlan by ID alone,
// with no name (vrpParseGlobalVlans), and Netbox rejects a blank vlan name
// with 400 "This field may not be blank."
func TestSyncVlansFallsBackToGeneratedNameWhenBlank(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), &util.ConfigDeviceSync{VlanGroupName: "Global VLANs"})
	if _, err := ds.nb.EnsureVlanGroup(ds.cfg.VlanGroupName); err != nil {
		t.Fatalf("EnsureVlanGroup: %v", err)
	}

	dc := drivers.NewDeviceConfig()
	dc.GlobalVLANs[994] = &drivers.VLAN{ID: 994} // no Name, as vlan-batch produces
	ds.syncVlans(&devicePair{nbDevice: &models.Device{Name: "sw1"}, config: dc})

	if len(fake.createdVlans) != 1 || fake.createdVlans[0].Name == "" {
		t.Fatalf("createdVlans = %+v, want one vlan with a non-blank fallback name", fake.createdVlans)
	}
}

func TestSyncVlansNoopWhenGroupNameUnset(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	dc := drivers.NewDeviceConfig()
	dc.GlobalVLANs[10] = &drivers.VLAN{ID: 10, Name: "servers"}
	ds.syncVlans(&devicePair{nbDevice: &models.Device{Name: "sw1"}, config: dc})

	if len(fake.createdVlans) != 0 {
		t.Errorf("createdVlans = %v, want none (VlanGroupName unset)", fake.createdVlans)
	}
}

func TestSyncInterfaceVlansPushesAssignment(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), &util.ConfigDeviceSync{VlanGroupName: "Global VLANs"})
	if _, err := ds.nb.EnsureVlanGroup(ds.cfg.VlanGroupName); err != nil {
		t.Fatalf("EnsureVlanGroup: %v", err)
	}
	if _, err := ds.nb.EnsureVlan(10, "servers"); err != nil {
		t.Fatalf("EnsureVlan(10): %v", err)
	}
	if _, err := ds.nb.EnsureVlan(20, "voice"); err != nil {
		t.Fatalf("EnsureVlan(20): %v", err)
	}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", SwitchportMode: "trunk", UntaggedVLAN: 10, TaggedVLANs: []int{20}})

	nbDevice := &models.Device{
		Name:       "sw1",
		Interfaces: []models.Interface{{NetboxID: 100, Name: "Ethernet1"}},
	}
	ds.syncInterfaceVlans(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 1 {
		t.Fatalf("updatedInterfaces = %v, want one update", fake.updatedInterfaces)
	}
	changes := fake.updatedInterfaces[0]
	untaggedID, _ := ds.nb.VlanNetboxID(10)
	taggedID, _ := ds.nb.VlanNetboxID(20)
	if changes["untagged_vlan"] != untaggedID {
		t.Errorf("changes[untagged_vlan] = %v, want %v", changes["untagged_vlan"], untaggedID)
	}
	if tagged, ok := changes["tagged_vlans"].([]uint); !ok || len(tagged) != 1 || tagged[0] != taggedID {
		t.Errorf("changes[tagged_vlans] = %v, want [%v]", changes["tagged_vlans"], taggedID)
	}
	// Netbox rejects untagged_vlan/tagged_vlans unless the interface's own
	// "mode" supports them ("Interface mode does not support untagged
	// vlan") - must be sent in the same PATCH, mapped from SwitchportMode.
	if changes["mode"] != "tagged" {
		t.Errorf(`changes["mode"] = %v, want "tagged" (SwitchportMode "trunk")`, changes["mode"])
	}
	if nbDevice.Interfaces[0].UntaggedVLAN != 10 {
		t.Errorf("nbDevice.Interfaces[0].UntaggedVLAN = %d, want 10 (updated in-memory)", nbDevice.Interfaces[0].UntaggedVLAN)
	}

	// Re-running with the same state should be a no-op.
	ds.syncInterfaceVlans(&devicePair{nbDevice: nbDevice, config: dc})
	if len(fake.updatedInterfaces) != 1 {
		t.Errorf("updatedInterfaces after re-run = %v, want still one (no change)", fake.updatedInterfaces)
	}
}

// TestSyncInterfaceVlansModeClearedWhenNoLongerSwitchport covers Netbox's
// "mode" field being reset to null (not left as a stale "access"/"tagged")
// once an interface's vlan assignment is fully cleared, e.g. reconfigured
// from a switchport to a routed port.
// TestSyncInterfaceVlansQinQUsesSVlanField covers Netbox's q-in-q mode: the
// outer/S-VLAN tag goes in qinq_svlan, not untagged_vlan (the two are
// mutually exclusive in Netbox) - SwitchportMode "dot1q-tunnel" is what
// VRP's "port link-type dot1q-tunnel"/EOS's "switchport mode dot1q-tunnel"
// parse to (the same generic regex that already captures "access"/"trunk").
func TestSyncInterfaceVlansQinQUsesSVlanField(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), &util.ConfigDeviceSync{VlanGroupName: "Global VLANs"})
	if _, err := ds.nb.EnsureVlanGroup(ds.cfg.VlanGroupName); err != nil {
		t.Fatalf("EnsureVlanGroup: %v", err)
	}
	if _, err := ds.nb.EnsureVlan(100, "customer-a"); err != nil {
		t.Fatalf("EnsureVlan(100): %v", err)
	}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", SwitchportMode: "dot1q-tunnel", UntaggedVLAN: 100})

	nbDevice := &models.Device{
		Name:       "sw1",
		Interfaces: []models.Interface{{NetboxID: 100, Name: "Ethernet1"}},
	}
	ds.syncInterfaceVlans(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 1 {
		t.Fatalf("updatedInterfaces = %v, want one update", fake.updatedInterfaces)
	}
	changes := fake.updatedInterfaces[0]
	svlanID, _ := ds.nb.VlanNetboxID(100)
	if changes["qinq_svlan"] != svlanID {
		t.Errorf("changes[qinq_svlan] = %v, want %v", changes["qinq_svlan"], svlanID)
	}
	if _, ok := changes["untagged_vlan"]; ok {
		t.Errorf("changes[untagged_vlan] = %v, want field absent (q-in-q uses qinq_svlan instead)", changes["untagged_vlan"])
	}
	if changes["mode"] != "q-in-q" {
		t.Errorf(`changes["mode"] = %v, want "q-in-q"`, changes["mode"])
	}
}

func TestSyncInterfaceVlansModeClearedWhenNoLongerSwitchport(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), &util.ConfigDeviceSync{VlanGroupName: "Global VLANs"})

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1"}) // no longer a switchport

	nbDevice := &models.Device{
		Name:       "sw1",
		Interfaces: []models.Interface{{NetboxID: 100, Name: "Ethernet1", UntaggedVLAN: 10}},
	}
	ds.syncInterfaceVlans(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 1 {
		t.Fatalf("updatedInterfaces = %v, want one update (clearing untagged_vlan)", fake.updatedInterfaces)
	}
	changes := fake.updatedInterfaces[0]
	if changes["untagged_vlan"] != nil {
		t.Errorf(`changes["untagged_vlan"] = %v, want nil`, changes["untagged_vlan"])
	}
	if changes["mode"] != nil {
		t.Errorf(`changes["mode"] = %v, want nil (no longer a switchport)`, changes["mode"])
	}
}

// TestSyncInterfaceVlansSkipsPartiallyResolvedTaggedVlans covers a real bug:
// if one tagged VID in the list fails to resolve to a Netbox ID (e.g. its
// EnsureVlan call errored earlier in syncVlans), the old code still sent
// Netbox a tagged_vlans PATCH built from just the VIDs that did resolve -
// silently dropping the others from the interface, and then recording the
// full (unsent) list in-memory, which made every later run believe Netbox
// already matched and never retry.
func TestSyncInterfaceVlansSkipsPartiallyResolvedTaggedVlans(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), &util.ConfigDeviceSync{VlanGroupName: "Global VLANs"})
	if _, err := ds.nb.EnsureVlanGroup(ds.cfg.VlanGroupName); err != nil {
		t.Fatalf("EnsureVlanGroup: %v", err)
	}
	// Only vlan 10 is registered - vlan 20 is deliberately never
	// EnsureVlan'd, simulating a vlan whose own sync failed.
	if _, err := ds.nb.EnsureVlan(10, "servers"); err != nil {
		t.Fatalf("EnsureVlan(10): %v", err)
	}

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", TaggedVLANs: []int{10, 20}})
	nbDevice := &models.Device{
		Name:       "sw1",
		Interfaces: []models.Interface{{NetboxID: 100, Name: "Ethernet1"}},
	}
	ds.syncInterfaceVlans(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 0 {
		t.Fatalf("updatedInterfaces = %v, want none (vlan 20 unresolved, must not push a partial list)", fake.updatedInterfaces)
	}
	if nbDevice.Interfaces[0].TaggedVLANs != nil {
		t.Errorf("nbDevice.Interfaces[0].TaggedVLANs = %v, want unchanged (nothing was actually sent)", nbDevice.Interfaces[0].TaggedVLANs)
	}
}

func TestSyncInterfaceVlansNoopWhenGroupNameUnset(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	dc := drivers.NewDeviceConfig()
	dc.AddInterface(&drivers.Interface{Name: "Ethernet1", UntaggedVLAN: 10})
	nbDevice := &models.Device{
		Name:       "sw1",
		Interfaces: []models.Interface{{NetboxID: 100, Name: "Ethernet1"}},
	}
	ds.syncInterfaceVlans(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.updatedInterfaces) != 0 {
		t.Errorf("updatedInterfaces = %v, want none (VlanGroupName unset)", fake.updatedInterfaces)
	}
}

// ----- ELINE → L2VPN EVPL -----

func TestElineLocalSideOfCrossDevice(t *testing.T) {
	iface := &drivers.Interface{Name: "Ethernet1.100"}
	pw := &drivers.Pseudowire{Name: "CN00570", PWID: 1000570, Neighbor: "10.0.0.1"}
	side := elineLocalSideOf(&drivers.ELINE{Name: "CN00570", Conn1: iface, Conn2: pw})
	if len(side.ifaceNames) != 1 || side.ifaceNames[0] != "Ethernet1.100" {
		t.Errorf("ifaceNames = %v, want [Ethernet1.100]", side.ifaceNames)
	}
	if side.pwid != 1000570 {
		t.Errorf("pwid = %d, want 1000570", side.pwid)
	}
}

func TestElineLocalSideOfSameDevice(t *testing.T) {
	a := &drivers.Interface{Name: "Ethernet1.10"}
	b := &drivers.Interface{Name: "Ethernet2.20"}
	side := elineLocalSideOf(&drivers.ELINE{Name: "CN00600", Conn1: a, Conn2: b})
	if len(side.ifaceNames) != 2 {
		t.Fatalf("ifaceNames = %v, want 2", side.ifaceNames)
	}
	if side.pwid != 0 {
		t.Errorf("pwid = %d, want 0 (same-device has no pseudowire)", side.pwid)
	}
}

func TestElineLocalSideOfSROSStringSAP(t *testing.T) {
	// SR OS parsers leave Conn1 as the sap-id string (and register a matching
	// Interface separately) - see srosParseEline.
	pw := &drivers.Pseudowire{Name: "CN00570", PWID: 1005704}
	side := elineLocalSideOf(&drivers.ELINE{
		Name:  "CN00570-4 GC-KNET",
		Conn1: "1/1/1:3704.*",
		Conn2: pw,
	})
	if len(side.ifaceNames) != 1 || side.ifaceNames[0] != "1/1/1:3704.*" {
		t.Errorf("ifaceNames = %v, want [1/1/1:3704.*]", side.ifaceNames)
	}
	if side.pwid != 1005704 {
		t.Errorf("pwid = %d, want 1005704", side.pwid)
	}
}

func TestSyncELINEsCreatesEVPLAndTermination(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	dc := drivers.NewDeviceConfig()
	ac := &drivers.Interface{Name: "Ethernet1.100", Type: "virtual"}
	dc.AddInterface(ac)
	dc.ELINEs["CN00570"] = &drivers.ELINE{
		Name:  "CN00570",
		Conn1: ac,
		Conn2: &drivers.Pseudowire{Name: "CN00570", PWID: 1000570, Neighbor: "10.0.0.2"},
	}
	nbDevice := &models.Device{
		Name: "r1",
		Interfaces: []models.Interface{
			{NetboxID: 42, Name: "Ethernet1.100", Type: "virtual"},
		},
	}

	ds.syncELINEs(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.createdL2VPNs) != 1 {
		t.Fatalf("createdL2VPNs = %d, want 1", len(fake.createdL2VPNs))
	}
	l := fake.createdL2VPNs[0]
	if l.Name != "CN00570" || l.Type != "evpl" || l.Identifier != 1000570 {
		t.Errorf("l2vpn = %+v, want name=CN00570 type=evpl identifier=1000570", l)
	}
	if l.Slug != "cn00570" {
		t.Errorf("slug = %q, want cn00570", l.Slug)
	}
	if len(fake.createdTerminations) != 1 {
		t.Fatalf("createdTerminations = %v, want one on interface 42", fake.createdTerminations)
	}
	if fake.createdTerminations[0] != [2]uint{l.NetboxID, 42} {
		t.Errorf("termination = %v, want [%d, 42]", fake.createdTerminations[0], l.NetboxID)
	}
}

func TestSyncELINEsSameDeviceCreatesTwoTerminations(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	dc := drivers.NewDeviceConfig()
	a := &drivers.Interface{Name: "Ethernet1.10", Type: "virtual"}
	b := &drivers.Interface{Name: "Ethernet2.20", Type: "virtual"}
	dc.AddInterface(a)
	dc.AddInterface(b)
	dc.ELINEs["CN00600"] = &drivers.ELINE{Name: "CN00600", Conn1: a, Conn2: b}
	nbDevice := &models.Device{
		Name: "r1",
		Interfaces: []models.Interface{
			{NetboxID: 10, Name: "Ethernet1.10"},
			{NetboxID: 20, Name: "Ethernet2.20"},
		},
	}

	ds.syncELINEs(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.createdL2VPNs) != 1 {
		t.Fatalf("createdL2VPNs = %d, want 1", len(fake.createdL2VPNs))
	}
	if fake.createdL2VPNs[0].Identifier != 0 {
		t.Errorf("identifier = %d, want 0 for same-device", fake.createdL2VPNs[0].Identifier)
	}
	if len(fake.createdTerminations) != 2 {
		t.Fatalf("createdTerminations = %v, want 2", fake.createdTerminations)
	}
}

func TestSyncELINEsReusesExistingByName(t *testing.T) {
	fake := newFakeNetboxAPI()
	existing := &netboxtool.NBL2VPN{NetboxID: 7, Name: "CN00570", Type: "evpl", Identifier: 1000570}
	fake.l2vpnsByName["CN00570"] = existing
	fake.l2vpnsByID[7] = existing
	fake.l2vpnsByIdentifier[1000570] = existing

	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)
	dc := drivers.NewDeviceConfig()
	ac := &drivers.Interface{Name: "Ethernet1.100"}
	dc.ELINEs["CN00570"] = &drivers.ELINE{
		Name:  "CN00570",
		Conn1: ac,
		Conn2: &drivers.Pseudowire{PWID: 1000570},
	}
	nbDevice := &models.Device{
		Name:       "r1",
		Interfaces: []models.Interface{{NetboxID: 42, Name: "Ethernet1.100"}},
	}

	ds.syncELINEs(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.createdL2VPNs) != 0 {
		t.Errorf("createdL2VPNs = %d, want 0 (reuse existing)", len(fake.createdL2VPNs))
	}
	if len(fake.createdTerminations) != 1 || fake.createdTerminations[0][0] != 7 {
		t.Errorf("createdTerminations = %v, want termination on existing l2vpn 7", fake.createdTerminations)
	}
}

func TestSyncELINEsReusesExistingByIdentifier(t *testing.T) {
	// Service-provisioned L2VPN is named after the ServiceID; device may
	// still share the same pseudowire ID under a matching identifier even
	// if we look up by a different name first and miss.
	fake := newFakeNetboxAPI()
	existing := &netboxtool.NBL2VPN{NetboxID: 9, Name: "CN00570", Type: "evpl", Identifier: 1000570}
	fake.l2vpnsByID[9] = existing
	fake.l2vpnsByIdentifier[1000570] = existing
	// Deliberately NOT in l2vpnsByName under the device's eline name.

	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)
	dc := drivers.NewDeviceConfig()
	ac := &drivers.Interface{Name: "Ethernet1.100"}
	dc.ELINEs["CN00570-legacy"] = &drivers.ELINE{
		Name:  "CN00570-legacy",
		Conn1: ac,
		Conn2: &drivers.Pseudowire{PWID: 1000570},
	}
	nbDevice := &models.Device{
		Name:       "r1",
		Interfaces: []models.Interface{{NetboxID: 42, Name: "Ethernet1.100"}},
	}

	ds.syncELINEs(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.createdL2VPNs) != 0 {
		t.Errorf("createdL2VPNs = %d, want 0 (matched by identifier)", len(fake.createdL2VPNs))
	}
	if len(fake.createdTerminations) != 1 || fake.createdTerminations[0][0] != 9 {
		t.Errorf("createdTerminations = %v, want on l2vpn 9", fake.createdTerminations)
	}
}

func TestSyncELINEsIdempotentTerminations(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	dc := drivers.NewDeviceConfig()
	ac := &drivers.Interface{Name: "Ethernet1.100"}
	dc.ELINEs["CN00570"] = &drivers.ELINE{
		Name:  "CN00570",
		Conn1: ac,
		Conn2: &drivers.Pseudowire{PWID: 1000570},
	}
	nbDevice := &models.Device{
		Name:       "r1",
		Interfaces: []models.Interface{{NetboxID: 42, Name: "Ethernet1.100"}},
	}
	pair := &devicePair{nbDevice: nbDevice, config: dc}

	ds.syncELINEs(pair)
	ds.syncELINEs(pair)

	if len(fake.createdL2VPNs) != 1 {
		t.Errorf("createdL2VPNs = %d, want 1 after two runs", len(fake.createdL2VPNs))
	}
	if len(fake.createdTerminations) != 1 {
		t.Errorf("createdTerminations = %d, want 1 after two runs (idempotent)", len(fake.createdTerminations))
	}
}

func TestSyncELINEsSkipsMissingInterface(t *testing.T) {
	fake := newFakeNetboxAPI()
	ds, reporter := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	dc := drivers.NewDeviceConfig()
	ac := &drivers.Interface{Name: "Ethernet1.100"}
	dc.ELINEs["CN00570"] = &drivers.ELINE{
		Name:  "CN00570",
		Conn1: ac,
		Conn2: &drivers.Pseudowire{PWID: 1000570},
	}
	// Netbox snapshot has no matching interface (interfacesCreate hasn't
	// created it, or it failed) - L2VPN is still created, termination is not.
	nbDevice := &models.Device{Name: "r1"}

	ds.syncELINEs(&devicePair{nbDevice: nbDevice, config: dc})

	if len(fake.createdL2VPNs) != 1 {
		t.Fatalf("createdL2VPNs = %d, want 1", len(fake.createdL2VPNs))
	}
	if len(fake.createdTerminations) != 0 {
		t.Errorf("createdTerminations = %v, want none when interface missing", fake.createdTerminations)
	}
	found := false
	for _, line := range reporter.lines {
		if strings.Contains(line, "not in netbox yet") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about missing interface; lines = %v", reporter.lines)
	}
}

func TestL2vpnSlug(t *testing.T) {
	cases := map[string]string{
		"CN00570":           "cn00570",
		"CN00570-4 GC-KNET": "cn00570-4-gc-knet",
		"  Foo/Bar  ":       "foo-bar",
	}
	for in, want := range cases {
		if got := l2vpnSlug(in); got != want {
			t.Errorf("l2vpnSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// ----- forEachPair concurrency -----

// TestForEachPairTemplateCacheIsRaceSafe runs interfacesCreate over many
// pairs that all share one manufacturer/model, so every worker goroutine
// hits NetboxMgr.TemplateInterfaceTypes (backed by netboxtool.Cache, which
// is documented as unsafe for concurrent use) for the same cache key at
// once. Run with `go test -race` - it doesn't assert anything -race
// wouldn't already catch, but nothing in the existing suite gave forEachPair
// more than one pair to fan out over, so without this test the sharedMu
// locking added to NetboxMgr had no coverage at all.
func TestForEachPairTemplateCacheIsRaceSafe(t *testing.T) {
	fake := newFakeNetboxAPI()
	fake.deviceTypes["Cisco/ISR"] = &netboxtool.NetboxDeviceTypeDetail{
		Interfaces: []netboxtool.NetboxInterfaceTemplate{{Name: "Ethernet1", Type: "1000base-t"}},
	}
	ds, _ := newTestDeviceSync(fake, newFakeFactumAPI(), nil)

	const n = 3 * syncWorkers
	for i := 0; i < n; i++ {
		dc := drivers.NewDeviceConfig()
		dc.AddInterface(&drivers.Interface{Name: "Ethernet1", Type: "other"})
		ds.pairs = append(ds.pairs, devicePair{
			nbDevice: &models.Device{
				NetboxID:     uint(i + 1),
				Name:         fmt.Sprintf("sw%d", i),
				Manufacturer: "Cisco",
				ModelName:    "ISR",
			},
			config: dc,
		})
	}

	ds.forEachPair(ds.interfacesCreate)

	if len(fake.createdInterfaces) != n {
		t.Fatalf("createdInterfaces = %d, want %d", len(fake.createdInterfaces), n)
	}
	for _, extra := range fake.createdInterfaceExtras {
		if extra["type"] != "1000base-t" {
			t.Errorf("created interface type = %v, want template type 1000base-t (cache corrupted by concurrent access?)", extra["type"])
		}
	}
}
