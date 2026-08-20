package drivers

// Data structures for all devices
// modeled loosely after netbox

import "net/netip"

type DeviceModel struct {
	Name     string
	Platform string
}

type ExecModel struct {
	Result string
}

type InterfaceModel struct {
	Name               string
	Description        string
	LineProtocolStatus string
	InterfaceStatus    string
}

type VersionModel struct {
	ModelName        string
	InternalVersion  string
	SystemMacAddress string
	SerialNumber     string
	MemTotal         float64
	BootupTimestap   float64
	MemFree          float64
	Version          string
	Architecture     string
	InternalBuildId  string
	HardwareRevision string
}

type RunningConfigModel struct {
	ConfigStr string
}

type RunningConfigStructured struct {
	ConfigStruct any
}

// ----------------------------------------------------------------------
// Parsed device configuration - interfaces, VRFs, pseudowires, ELINE/ELAN
// services and LLDP neighbors extracted from a device's running config by
// GetDeviceConfig()/GetNeighbors(), used by internal/device-sync to compare
// a device's live config against NetBox and sync the difference.
// ----------------------------------------------------------------------

// InterfaceAddress is one IP address configured on an interface, together
// with the VRF it belongs to (Netbox models VRF on the address; this
// application follows the device and keeps VRF on the interface instead -
// see Interface.VRF - but a device can technically configure a different
// VRF per address family, hence it's carried here too).
type InterfaceAddress struct {
	Address netip.Prefix
	VRF     string
}

// Interface is one parsed interface (physical, subinterface or LAG).
type Interface struct {
	Name        string
	Description string
	Type        string // netbox interface type bucket: "virtual", "lag", ...
	VRF         string
	LagID       string
	LagParent   string
	VlanID      string
	IPAddresses []InterfaceAddress

	// Parent is another parsed Interface's Name that this one nests under in
	// Netbox (Netbox's own "parent" interface relationship) - "" if this
	// interface has no parent. Currently only set for SR OS SAP interfaces
	// (see srosAddSapInterface), whose Parent is the physical port the SAP
	// is bound to.
	Parent string
	// Label is Netbox's free-text interface label, distinct from Name -
	// currently only set for SR OS SAP interfaces, to "SAP" (see
	// srosAddSapInterface), since SR OS has no real subinterfaces and the
	// Netbox interface standing in for one would otherwise look like it is.
	Label string

	// L2 switchport config, for global-VLAN platforms (EOS, VRP) where an
	// access/trunk port references DeviceConfig.GlobalVLANs by ID - bridge-
	// domain platforms (IOS-XR, SR OS) don't use these, since their VLANs
	// are scoped per bridge-domain/ELAN instead, not global to the device.
	SwitchportMode string // "access", "trunk" or "dot1q-tunnel" (Q-in-Q); "" if not an L2 switchport
	UntaggedVLAN   int    // access VLAN (access mode), native/PVID VLAN (trunk mode), or outer/S-VLAN tag (dot1q-tunnel mode); 0 if none
	TaggedVLANs    []int  // VLANs carried tagged on a trunk port

	// L2Transport is true if this subinterface is configured as pure L2
	// transport rather than L3/IP routing - IOS-XR's "interface X
	// l2transport" and VRP's "interface X mode l2", both trailing tokens on
	// the interface line itself rather than a child line. Distinct from
	// SwitchportMode above, which is the global-VLAN platforms' (EOS, VRP
	// physical ports) equivalent concept - a bridge-domain platform's L2
	// subinterface has no global VLAN membership to express that way. Not
	// consumed anywhere yet; captured so a caller like internal/device-sync
	// can later tell "no IPAddresses because this is an L2 leg" apart from
	// "no IPAddresses because config is incomplete".
	L2Transport bool
}

// VLAN is a global (device-wide) VLAN definition.
type VLAN struct {
	ID   int
	Name string
}

// VLANConfig is the desired switchport/VLAN state for one interface, passed
// to DriverClient.SetInterfaceVLANs - the write-side counterpart of
// Interface's SwitchportMode/UntaggedVLAN/TaggedVLANs fields above. Only
// meaningful on global-VLAN platforms (EOS, VRP) - see SetInterfaceVLANs.
type VLANConfig struct {
	SwitchportMode string // "access", "trunk" or "dot1q-tunnel" (Q-in-Q); "" to remove switchport config
	UntaggedVLAN   int    // access VLAN, native/PVID VLAN, or outer/S-VLAN tag; 0 if none
	TaggedVLANs    []int  // VLANs carried tagged on a trunk port
}

// Pseudowire is one MPLS/SDP pseudowire, used as one end of an ELINE.
type Pseudowire struct {
	Name        string
	Neighbor    string
	Type        string
	PWID        int
	MTU         int
	ControlWord bool
}

// ELINE is a point-to-point service (patch panel / epipe / xconnect); each
// of Conn1/Conn2 is either a *Interface or a *Pseudowire - callers
// type-switch on it.
type ELINE struct {
	Name        string
	Description string
	Conn1       any
	Conn2       any
}

// ELAN is a multipoint L2 service (BGP VLAN / VPLS / bridge-domain).
// Interfaces holds interface/SAP names rather than resolved *Interface
// pointers, even though every name here also has a matching entry in
// DeviceConfig.InterfacesByName (SR OS's SAP ids included - see
// srosAddSapInterface) - callers that need the full Interface look it up
// there by name rather than this field carrying pointers itself.
type ELAN struct {
	Name        string
	Description string
	RD          string
	RTImport    string
	RTExport    string
	Interfaces  []string
}

// VRF is a routing/forwarding instance. Interfaces holds the names of
// parsed interfaces that reference this VRF.
type VRF struct {
	Name        string
	Description string
	RD          string
	RTImport    string
	RTExport    string
	Interfaces  []string
}

// L3VPN is a VRF-attached BGP/MPLS L3 service (router bgp vrf / vprn).
type L3VPN struct {
	Name        string
	Description string
	VRF         *VRF
}

// Neighbor is one LLDP-discovered neighbor.
type Neighbor struct {
	LocalInterface  string
	SystemID        string
	RemoteName      string
	RemoteInterface string
}

// DeviceConfig is a device's fully parsed running config, returned by
// DriverClient.GetDeviceConfig().
type DeviceConfig struct {
	Interfaces       []*Interface
	InterfacesByName map[string]*Interface
	VRFs             map[string]*VRF
	ELINEs           map[string]*ELINE
	ELANs            map[string]*ELAN
	L3VPNs           map[string]*L3VPN
	GlobalVLANs      map[int]*VLAN
}

// NewDeviceConfig returns an empty, initialized DeviceConfig - callers
// should build through this rather than a zero value so the maps are never
// nil.
func NewDeviceConfig() *DeviceConfig {
	return &DeviceConfig{
		InterfacesByName: make(map[string]*Interface),
		VRFs:             make(map[string]*VRF),
		ELINEs:           make(map[string]*ELINE),
		ELANs:            make(map[string]*ELAN),
		L3VPNs:           make(map[string]*L3VPN),
		GlobalVLANs:      make(map[int]*VLAN),
	}
}

// AddInterface appends interface to both Interfaces and InterfacesByName.
func (dc *DeviceConfig) AddInterface(interfaceObj *Interface) {
	dc.Interfaces = append(dc.Interfaces, interfaceObj)
	dc.InterfacesByName[interfaceObj.Name] = interfaceObj
}
