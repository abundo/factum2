package drivers

// Waystream iBOS driver. See README-DRIVERS.md. BECS owns the element, so
// this driver only reads LLDP neighbors for device-sync cables.

import (
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/netboxtool"
)

// IbosDriver reads LLDP-MIB over SNMPv2c. It does not open SSH.
type IbosDriver struct {
	DriverClient
	p DriverParam
}

func init() {
	registerDriver("ibos", func(p DriverParam) (DriverClient, error) {
		return NewIbosDriver(p)
	})
	registerInterfaceNames("ibos", ibosExpandInterfaceName)
}

func NewIbosDriver(p DriverParam) (*IbosDriver, error) {
	return &IbosDriver{p: p}, nil
}

var (
	_ DriverClient      = (*IbosDriver)(nil)
	_ ExternalInventory = (*IbosDriver)(nil)
)

func (driver *IbosDriver) ExternalInventory() {}

func errIbosExternal(op string) error {
	return fmt.Errorf("%s is not supported: iBOS devices are managed by another system", op)
}

func (driver *IbosDriver) Exec(cmd string) (*ExecModel, error) {
	return nil, errIbosExternal("Exec")
}

func (driver *IbosDriver) Version() (*VersionModel, error) {
	return nil, errIbosExternal("Version")
}

func (driver *IbosDriver) RunningConfigGet(jsonformat bool) (*RunningConfigModel, error) {
	return nil, errIbosExternal("RunningConfigGet")
}

func (driver *IbosDriver) RunningConfigSave() error {
	return errIbosExternal("RunningConfigSave")
}

func (driver *IbosDriver) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	return nil, errIbosExternal("GetInterfacesStatus")
}

func (driver *IbosDriver) SetInterfaceDescription(intf *netboxtool.NBInterface) error {
	return setInterfaceDescription(driver.SetInterfaceDescriptions, intf)
}

func (driver *IbosDriver) SetInterfaceDescriptions(name []string, intf []*netboxtool.NBInterface) error {
	return errIbosExternal("SetInterfaceDescriptions")
}

func (driver *IbosDriver) SetInterfaceVLANs(name []string, params []*VLANConfig) error {
	return errIbosExternal("SetInterfaceVLANs")
}

// GetDeviceConfig returns an empty config. BECS already wrote the
// interfaces and addresses to NetBox. device-sync sees ExternalInventory
// and does not compare this result with that inventory.
func (driver *IbosDriver) GetDeviceConfig() (*DeviceConfig, error) {
	return NewDeviceConfig(), nil
}

// GetNeighbors reads IEEE LLDP-MIB (LLDP-V2-MIB if the original is absent)
// over SNMPv2c. The community is DriverParam.SNMPCommunities.
func (driver *IbosDriver) GetNeighbors() ([]*Neighbor, error) {
	return lldpNeighbors(driver.p.Name, driver.p.SNMPCommunities)
}

// ibosIfPrefixes are the show-command abbreviations iBOS uses. Longer
// names come first. The long form is the name BECS stores in NetBox
// (gigabitethernet3). A subinterface stays as BECS wrote it (gi3/vid511).
var ibosIfPrefixes = []struct{ short, long string }{
	{"hundredgigabitethernet", "hundredgigabitethernet"},
	{"fortygigabitethernet", "fortygigabitethernet"},
	{"twentyfivegigabitethernet", "twentyfivegigabitethernet"},
	{"tengigabitethernet", "tengigabitethernet"},
	{"fivegigabitethernet", "fivegigabitethernet"},
	{"twogigabitethernet", "twogigabitethernet"},
	{"gigabitethernet", "gigabitethernet"},
	{"fastethernet", "fastethernet"},
	{"ethernet", "ethernet"},
	{"hu", "hundredgigabitethernet"},
	{"fo", "fortygigabitethernet"},
	{"tf", "twentyfivegigabitethernet"},
	{"te", "tengigabitethernet"},
	{"fi", "fivegigabitethernet"},
	{"tw", "twogigabitethernet"},
	{"gi", "gigabitethernet"},
	{"fe", "fastethernet"},
	{"et", "ethernet"},
}

// ibosExpandInterfaceName turns an LLDP port id into the BECS NetBox name.
// gi3 and "gigabitethernet 3" both become gigabitethernet3. gi3/vid511 is
// left unchanged: that is the stored subinterface name, and LLDP does not
// cable those.
func ibosExpandInterfaceName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	folded := strings.ToLower(strings.ReplaceAll(name, " ", ""))
	for _, p := range ibosIfPrefixes {
		if !strings.HasPrefix(folded, p.short) {
			continue
		}
		rest := folded[len(p.short):]
		if !ibosPhysicalRest(rest) {
			continue
		}
		if p.short == p.long {
			return folded
		}
		return p.long + rest
	}
	return name
}

// ibosPhysicalRest is a port number such as "3" or "1/0/2". A subinterface
// suffix (3/vid511) is not a physical port.
func ibosPhysicalRest(rest string) bool {
	if rest == "" || rest[0] < '0' || rest[0] > '9' {
		return false
	}
	for _, r := range rest {
		if (r < '0' || r > '9') && r != '/' {
			return false
		}
	}
	return true
}
