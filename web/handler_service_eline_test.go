package web

import (
	"errors"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

type elinePackStub struct {
	prepared   bool
	applied    bool
	prepareErr error
}

func (s *elinePackStub) PrepareELINEApply(*drivers.ELINEIntent) error {
	s.prepared = true
	return s.prepareErr
}
func (s *elinePackStub) ApplyCLISession(string, []string) error {
	s.applied = true
	return nil
}
func (s *elinePackStub) Exec(string) (*drivers.ExecModel, error) { return nil, nil }
func (s *elinePackStub) RunningConfigGet(bool) (*drivers.RunningConfigModel, error) {
	return nil, nil
}
func (s *elinePackStub) RunningConfigSave() error { return nil }
func (s *elinePackStub) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	return nil, nil
}
func (s *elinePackStub) SetInterfaceDescription(*netboxtool.NBInterface) error { return nil }
func (s *elinePackStub) SetInterfaceDescriptions([]string, []*netboxtool.NBInterface) error {
	return nil
}
func (s *elinePackStub) SetInterfaceVLANs([]string, []*drivers.VLANConfig) error { return nil }
func (s *elinePackStub) Version() (*drivers.VersionModel, error)                 { return nil, nil }
func (s *elinePackStub) GetDeviceConfig() (*drivers.DeviceConfig, error)         { return nil, nil }
func (s *elinePackStub) GetNeighbors() ([]*drivers.Neighbor, error)              { return nil, nil }

func TestPseudowireIDFromServiceID(t *testing.T) {
	cases := []struct {
		serviceID string
		want      int
		wantErr   bool
	}{
		{serviceID: "CN1234", want: 1001234},
		{serviceID: "CN00001", want: 1000001},
		{serviceID: "CI00042", want: 1100042},   // CI gets its own prefix (11), distinct from CN's 10
		{serviceID: "CN123456", want: 10123456}, // more than 5 digits: kept as-is, %05d is a minimum width
		{serviceID: "CN", wantErr: true},
		{serviceID: "", wantErr: true},
	}

	for _, tc := range cases {
		got, err := pseudowireIDFromServiceID(tc.serviceID)
		if tc.wantErr {
			if err == nil {
				t.Errorf("pseudowireIDFromServiceID(%q): expected error, got %d", tc.serviceID, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("pseudowireIDFromServiceID(%q): unexpected error: %v", tc.serviceID, err)
			continue
		}
		if got != tc.want {
			t.Errorf("pseudowireIDFromServiceID(%q) = %d, want %d", tc.serviceID, got, tc.want)
		}
	}
}

func TestElineComputeStale(t *testing.T) {
	deviceX, deviceY := uint(1), uint(2)
	both := map[uint]bool{deviceX: true, deviceY: true}
	onlyY := map[uint]bool{deviceY: true}

	cases := []struct {
		name             string
		applied          elineAppliedState
		desiredDeviceID  uint
		desiredIface     string
		desiredVLAN      int
		currentDeviceIDs map[uint]bool
		want             *elineStale
	}{
		{
			name:             "never applied",
			applied:          elineAppliedState{},
			desiredDeviceID:  deviceX,
			desiredIface:     "Ethernet1",
			desiredVLAN:      100,
			currentDeviceIDs: both,
			want:             nil,
		},
		{
			name:             "unchanged",
			applied:          elineAppliedState{DeviceID: deviceX, Iface: "Ethernet1", VLAN: 100},
			desiredDeviceID:  deviceX,
			desiredIface:     "Ethernet1",
			desiredVLAN:      100,
			currentDeviceIDs: both,
			want:             nil,
		},
		{
			name:             "vlan changed, same device -> merge",
			applied:          elineAppliedState{DeviceID: deviceX, Iface: "Ethernet1", VLAN: 100},
			desiredDeviceID:  deviceX,
			desiredIface:     "Ethernet1",
			desiredVLAN:      200,
			currentDeviceIDs: both,
			want: &elineStale{
				DeviceID:  deviceX,
				Sub:       drivers.ELINEStaleSubinterface{Iface: "Ethernet1", VLAN: 100},
				Abandoned: false,
			},
		},
		{
			name:             "interface changed, same device -> merge",
			applied:          elineAppliedState{DeviceID: deviceX, Iface: "Ethernet1", VLAN: 100},
			desiredDeviceID:  deviceX,
			desiredIface:     "Ethernet2",
			desiredVLAN:      100,
			currentDeviceIDs: both,
			want: &elineStale{
				DeviceID:  deviceX,
				Sub:       drivers.ELINEStaleSubinterface{Iface: "Ethernet1", VLAN: 100},
				Abandoned: false,
			},
		},
		{
			name:             "device changed, old device no longer an endpoint -> abandoned",
			applied:          elineAppliedState{DeviceID: deviceX, Iface: "Ethernet1", VLAN: 100},
			desiredDeviceID:  deviceY,
			desiredIface:     "Ethernet1",
			desiredVLAN:      100,
			currentDeviceIDs: onlyY,
			want: &elineStale{
				DeviceID:  deviceX,
				Sub:       drivers.ELINEStaleSubinterface{Iface: "Ethernet1", VLAN: 100},
				Abandoned: true,
			},
		},
		{
			name:             "device changed, old device still the other endpoint -> merge",
			applied:          elineAppliedState{DeviceID: deviceX, Iface: "Ethernet1", VLAN: 100},
			desiredDeviceID:  deviceY,
			desiredIface:     "Ethernet1",
			desiredVLAN:      100,
			currentDeviceIDs: both,
			want: &elineStale{
				DeviceID:  deviceX,
				Sub:       drivers.ELINEStaleSubinterface{Iface: "Ethernet1", VLAN: 100},
				Abandoned: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := elineComputeStale(tc.applied, tc.desiredDeviceID, tc.desiredIface, tc.desiredVLAN, tc.currentDeviceIDs)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want %+v", tc.want)
			}
			if *got != *tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestIsPhysicalInterfaceType(t *testing.T) {
	cases := map[string]bool{
		"1000base-t":     true,
		"10gbase-x-sfpp": true,
		"virtual":        false,
		"lag":            false,
		"":               false,
	}
	for typ, want := range cases {
		if got := isPhysicalInterfaceType(typ); got != want {
			t.Errorf("isPhysicalInterfaceType(%q) = %v, want %v", typ, got, want)
		}
	}
}

// SR OS's loopback is always named "system", unlike EOS's operator-chosen
// "Loopback0" convention - confirmed not to be a lab-specific naming choice
// (deviceLoopback0Address's doc comment). A device whose platform lookup
// falls through to the wrong interface name would report a misleading
// "device has no Loopback0/system address" instead of finding the real one.
func TestDeviceLoopbackIfaceName(t *testing.T) {
	cases := []struct {
		platform string
		want     string
	}{
		{platform: "EOS", want: "Loopback0"},
		{platform: "sros", want: "system"},
		{platform: "SROS-MD", want: "system"},
		{platform: "ios-xr", want: "Loopback0"},
		{platform: "", want: "Loopback0"},
	}
	for _, tc := range cases {
		device := &models.Device{Platform: tc.platform}
		if got := deviceLoopbackIfaceName(device); got != tc.want {
			t.Errorf("deviceLoopbackIfaceName(platform=%q) = %q, want %q", tc.platform, got, tc.want)
		}
	}
}

func TestDeviceLoopback0Address(t *testing.T) {
	srosDevice := &models.Device{
		Platform: "sros",
		Name:     "sros1",
		Interfaces: []models.Interface{
			{Name: "1/1/1"},
			{Name: "system", Addresses: []models.Address{{Address: "172.27.250.104/32"}}},
		},
	}
	got, err := deviceLoopback0Address(srosDevice)
	if err != nil {
		t.Fatalf("deviceLoopback0Address: %v", err)
	}
	if got != "172.27.250.104" {
		t.Errorf("deviceLoopback0Address(sros) = %q, want 172.27.250.104 (stripped of /32)", got)
	}

	eosDevice := &models.Device{
		Platform: "eos",
		Name:     "eos1",
		Interfaces: []models.Interface{
			{Name: "Loopback0", Addresses: []models.Address{{Address: "172.27.250.127/32"}}},
		},
	}
	got, err = deviceLoopback0Address(eosDevice)
	if err != nil {
		t.Fatalf("deviceLoopback0Address: %v", err)
	}
	if got != "172.27.250.127" {
		t.Errorf("deviceLoopback0Address(eos) = %q, want 172.27.250.127", got)
	}

	// An EOS device with only a "system"-named interface (SR OS's name)
	// must not match - the platform, not the interface name found, decides
	// which name is looked up.
	noMatch := &models.Device{
		Platform:   "eos",
		Name:       "eos2",
		Interfaces: []models.Interface{{Name: "system", Addresses: []models.Address{{Address: "10.0.0.1/32"}}}},
	}
	if _, err := deviceLoopback0Address(noMatch); err == nil {
		t.Error("deviceLoopback0Address: want error for eos device with no Loopback0, got nil")
	}
}

func TestElineInterfaceDescription(t *testing.T) {
	got := elineInterfaceDescription("CN00570", "Acme AB")
	want := "ID=CN00570 Acme AB"
	if got != want {
		t.Errorf("elineInterfaceDescription = %q, want %q", got, want)
	}
}

// TestUpsertFactumSubinterface verifies that "Save & provision in Netbox"
// can mirror a newly created Netbox subinterface into factum's interfaces
// table (with description) without waiting for a full Netbox sync - the
// bug that left the device interfaces list missing the ELINE description.
func TestUpsertFactumSubinterface(t *testing.T) {
	db := newTestDB(t)

	device := models.Device{Name: "pe1", NetboxID: 10}
	if err := db.Create(&device).Error; err != nil {
		t.Fatalf("seed device: %v", err)
	}

	const (
		netboxID       = uint(9001)
		parentNetboxID = uint(42)
		name           = "Ethernet1.100"
		description    = "ID=CN00570 Acme AB"
	)
	if err := upsertFactumSubinterface(db, device.ID, netboxID, parentNetboxID, name, description); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var iface models.Interface
	if err := db.Where("device_id = ? AND netbox_id = ?", device.ID, netboxID).First(&iface).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if iface.Name != name {
		t.Errorf("Name = %q, want %q", iface.Name, name)
	}
	if iface.Description != description {
		t.Errorf("Description = %q, want %q", iface.Description, description)
	}
	if iface.Type != "virtual" {
		t.Errorf("Type = %q, want virtual", iface.Type)
	}
	if iface.ParentID != parentNetboxID {
		t.Errorf("ParentID = %d, want %d", iface.ParentID, parentNetboxID)
	}

	// Re-upsert with a new description (re-provision) must update in place
	// rather than create a second row for the same (device_id, netbox_id).
	newDesc := "ID=CN00570 Renamed AB"
	if err := upsertFactumSubinterface(db, device.ID, netboxID, parentNetboxID, name, newDesc); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	var count int64
	if err := db.Model(&models.Interface{}).Where("device_id = ? AND netbox_id = ?", device.ID, netboxID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1 after re-upsert", count)
	}
	if err := db.Where("device_id = ? AND netbox_id = ?", device.ID, netboxID).First(&iface).Error; err != nil {
		t.Fatalf("reload after re-upsert: %v", err)
	}
	if iface.Description != newDesc {
		t.Errorf("Description after re-upsert = %q, want %q", iface.Description, newDesc)
	}
}

func TestDeleteFactumInterfaceByNetboxID(t *testing.T) {
	db := newTestDB(t)

	device := models.Device{Name: "pe1", NetboxID: 10}
	if err := db.Create(&device).Error; err != nil {
		t.Fatalf("seed device: %v", err)
	}
	iface := models.Interface{
		DeviceID:    device.ID,
		NetboxID:    9001,
		Name:        "Ethernet1.100",
		Description: "ID=CN00570 Acme AB",
		Type:        "virtual",
	}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatalf("seed interface: %v", err)
	}

	deleteFactumInterfaceByNetboxID(db, 9001, "A")

	var count int64
	if err := db.Model(&models.Interface{}).Where("netbox_id = ?", uint(9001)).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("row count after delete = %d, want 0", count)
	}

	// Missing netbox_id must not panic / error - re-provision after
	// already-cleaned Netbox objects is the normal path.
	deleteFactumInterfaceByNetboxID(db, 9001, "A")
	deleteFactumInterfaceByNetboxID(db, 0, "A")
}

func TestApplyELINECmdsPackPathRunsPrepare(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	stub := &elinePackStub{prepareErr: errors.New("sdp 127 already exists with far-end 1.2.3.4")}
	intent := &drivers.ELINEIntent{
		Name: "CN00001",
		Remote: &drivers.ELINERemotePeer{
			NeighborIP:   "10.0.0.127",
			PseudowireID: 1000001,
		},
	}
	err := ctrl.applyELINECmds(stub, &models.Device{Name: "sros1", Platform: "sros"}, intent)
	if err == nil || !strings.Contains(err.Error(), "sdp 127 already exists") {
		t.Fatalf("err = %v, want SDP conflict", err)
	}
	if !stub.prepared {
		t.Fatal("PrepareELINEApply was not called on the pack path")
	}
	if stub.applied {
		t.Fatal("ApplyCLISession ran after PrepareELINEApply failed")
	}
}
