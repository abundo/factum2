package drivers

// Open ROADM MSA driver - see README-DRIVERS.md for how this driver works
// (read-only NETCONF against org-openroadm-device) and how it compares to
// the others.

import (
	"errors"
	"fmt"
	"sync"

	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

// OpenROADMDriver talks NETCONF (RFC 6241 over SSH 830) to an Open ROADM
// MSA device. It is read-only: inventory, interface status, neighbors and
// the YANG tree are fetched with <get>; every write method returns an
// error. The YANG namespace (http://org/openroadm/device) has been stable
// from MSA 1.2 through current device-model revisions; leaf names used
// here exist across that range. Frequency lives on later-model NMC-CTP /
// OCH / OTSi augment containers and is simply absent on 1.2/2.2 replies.
type OpenROADMDriver struct {
	DriverClient
	p DriverParam

	mu     sync.Mutex
	cached *orDevice
	raw    []byte
}

func init() {
	registerDriver("openroadm", func(p DriverParam) (DriverClient, error) {
		return NewOpenROADMDriver(p)
	})
}

func NewOpenROADMDriver(p DriverParam) (*OpenROADMDriver, error) {
	if err := validateDriverParam(p); err != nil {
		return nil, err
	}
	return &OpenROADMDriver{p: p}, nil
}

var (
	_ DriverClient  = (*OpenROADMDriver)(nil)
	_ OpticalClient = (*OpenROADMDriver)(nil)
)

func errOpenROADMReadOnly(op string) error {
	return fmt.Errorf("%s is not supported: Open ROADM driver is read-only", op)
}

// device fetches (once per driver instance) the org-openroadm-device tree
// via NETCONF <get>. Subsequent reads reuse the cached decode so
// GetDeviceConfig + GetNeighbors in one device-sync pass don't dial twice.
func (driver *OpenROADMDriver) device() (*orDevice, error) {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if driver.cached != nil {
		return driver.cached, nil
	}
	data, err := netconfGet(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, orDeviceFilter{})
	if err != nil {
		return nil, err
	}
	dev, err := parseORDevice(data)
	if err != nil {
		return nil, err
	}
	driver.cached = dev
	driver.raw = data
	return dev, nil
}

func (driver *OpenROADMDriver) Exec(cmd string) (*ExecModel, error) {
	return nil, errOpenROADMReadOnly("Exec")
}

func (driver *OpenROADMDriver) Version() (*VersionModel, error) {
	dev, err := driver.device()
	if err != nil {
		return nil, err
	}
	v := versionFromORDevice(dev)
	if v.ModelName == "" && v.SerialNumber == "" && v.Version == "" {
		return nil, errors.New("no org-openroadm-device/info in NETCONF response")
	}
	return v, nil
}

// RunningConfigGet returns the raw org-openroadm-device XML from <get>.
// jsonformat is ignored: there is no CLI-text or CLI-JSON equivalent.
func (driver *OpenROADMDriver) RunningConfigGet(jsonformat bool) (*RunningConfigModel, error) {
	if _, err := driver.device(); err != nil {
		return nil, err
	}
	driver.mu.Lock()
	raw := driver.raw
	driver.mu.Unlock()
	return &RunningConfigModel{ConfigStr: string(raw)}, nil
}

func (driver *OpenROADMDriver) RunningConfigSave() error {
	return errOpenROADMReadOnly("RunningConfigSave")
}

func (driver *OpenROADMDriver) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	dev, err := driver.device()
	if err != nil {
		return nil, err
	}
	return interfacesFromORDevice(dev), nil
}

func (driver *OpenROADMDriver) SetInterfaceDescription(intf *netboxtool.NBInterface) error {
	return setInterfaceDescription(driver.SetInterfaceDescriptions, intf)
}

func (driver *OpenROADMDriver) SetInterfaceDescriptions(name []string, intf []*netboxtool.NBInterface) error {
	return errOpenROADMReadOnly("SetInterfaceDescriptions")
}

func (driver *OpenROADMDriver) SetInterfaceVLANs(name []string, params []*VLANConfig) error {
	return errOpenROADMReadOnly("SetInterfaceVLANs")
}

func (driver *OpenROADMDriver) GetDeviceConfig() (*DeviceConfig, error) {
	dev, err := driver.device()
	if err != nil {
		return nil, err
	}
	return deviceConfigFromORDevice(dev), nil
}

func (driver *OpenROADMDriver) GetNeighbors() ([]*Neighbor, error) {
	dev, err := driver.device()
	if err != nil {
		return nil, err
	}
	return neighborsFromORDevice(dev), nil
}

// GetOpticalInventory maps org-openroadm-device onto Factum optical kinds,
// port roles and xconnects. Persist via optical.ApplyInventory (device-sync
// and `factum2-driver optical-inventory-apply`).
func (driver *OpenROADMDriver) GetOpticalInventory() (*OpticalInventory, error) {
	dev, err := driver.device()
	if err != nil {
		return nil, err
	}
	return inventoryFromORDevice(dev), nil
}

// OpticalClient is implemented by drivers that can read optical inventory
// (ROADM degrees/SRGs, transponder ports, intra-device xconnects).
// Deliberately not part of DriverClient: adding optical support to one
// driver should never force every other driver to grow a stub method.
type OpticalClient interface {
	GetOpticalInventory() (*OpticalInventory, error)
}

// OpticalInventory is one device's Open ROADM inventory, keyed with the
// same kind/role/xconnect strings as models.OpticalKind* / Port* / XC*.
type OpticalInventory struct {
	NodeID           string                `json:"node_id"`
	NodeType         string                `json:"node_type"`
	OpticalKind      string                `json:"optical_kind"`
	Vendor           string                `json:"vendor"`
	Model            string                `json:"model"`
	Serial           string                `json:"serial"`
	SoftwareVersion  string                `json:"software_version"`
	OpenROADMVersion string                `json:"openroadm_version"`
	Ports            []OpticalPortView     `json:"ports"`
	XConnects        []OpticalXConnectView `json:"xconnects"`
}

// OpticalPortView is one physical circuit-pack port with a Factum optical
// role. Name is "circuit-pack-name/port-name".
type OpticalPortView struct {
	Name        string `json:"name"`
	CircuitPack string `json:"circuit_pack"`
	PortName    string `json:"port_name"`
	Role        string `json:"role"`
	Description string `json:"description"`
	AdminStatus string `json:"admin_status"`
	OperStatus  string `json:"oper_status"`
	FreqHz      uint64 `json:"freq_hz"`
	Qual        string `json:"qual"`
}

// OpticalXConnectView is one intra-device optical adjacency. PortA/PortB
// are OpticalPortView.Name values. Kind is models.XC*.
type OpticalXConnectView struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	PortA     string `json:"port_a"`
	PortB     string `json:"port_b"`
	FreqHz    uint64 `json:"freq_hz"`
	CircuitID string `json:"circuit_id"`
}

// ApplyInventory is the Factum optical.Inventory shape of this dump, for
// optical.ApplyInventory / PUT /api/optical/device/:id/inventory.
func (inv *OpticalInventory) ApplyInventory() optical.Inventory {
	if inv == nil {
		return optical.Inventory{}
	}
	out := optical.Inventory{Kind: inv.OpticalKind, Source: models.XCSourceOpenROADM}
	for _, p := range inv.Ports {
		out.Ports = append(out.Ports, optical.InventoryPort{Name: p.Name, Role: p.Role, FreqHz: p.FreqHz})
	}
	for _, x := range inv.XConnects {
		out.XConnects = append(out.XConnects, optical.InventoryXConnect{
			Name: x.Name, Kind: x.Kind, PortA: x.PortA, PortB: x.PortB, FreqHz: x.FreqHz,
		})
	}
	return out
}
