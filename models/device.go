package models

// import "github.com/abundo/netboxtool"

// --------------------------------------------------------------------------
//	Netbox
// --------------------------------------------------------------------------

type Device struct {
	FactumModel
	// Netbox's dcim.Device and virtualization.VirtualMachine tables have
	// independent ID sequences, so NetboxID is only unique per VM value,
	// never globally - see internal/netbox.syncDevice.
	VM             bool   `json:"vm" gorm:"uniqueIndex:idx_devices_netbox_id_vm"`
	NetboxID       uint   `json:"netbox_id" gorm:"uniqueIndex:idx_devices_netbox_id_vm"`
	Name           string `json:"name" gorm:"type:varchar(255)"`
	Comments       string `json:"comments" gorm:"type:varchar(255)"`
	Enabled        bool   `json:"enabled"`
	Manufacturer   string `json:"manufacturer" gorm:"type:varchar(255)"`
	ManufacturerID uint   `json:"manufacturer_id"`
	ModelName      string `json:"model_name" gorm:"type:varchar(255)"`
	ModelID        uint   `json:"model_id"`
	Platform       string `json:"platform" gorm:"type:varchar(255)"`
	PlatformID     uint   `json:"platform_id"`
	PrimaryIPv4    string `json:"primary_ipv4" gorm:"type:varchar(255)"`
	PrimaryIPv4ID  uint   `json:"primary_ipv4_id"`
	PrimaryIPv6    string `json:"primary_ipv6" gorm:"type:varchar(255)"`
	PrimaryIPv6ID  uint   `json:"primary_ipv6_id"`
	Role           string `json:"role" gorm:"type:varchar(255)"`
	RoleID         uint   `json:"role_id"`
	Site           string `json:"site" gorm:"type:varchar(255)"`
	SiteID         uint   `json:"site_id"`
	Status         string `json:"status" gorm:"type:varchar(255)"`
	// Latitude/Longitude are the device's own GPS coordinates if Netbox has
	// them, else inherited from its site - nil if neither is set. See
	// internal/netbox.syncDevice.
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`

	// OpticalKind is computed on every NetBox sync (CF then role map).
	// Always assigned on the upsert struct so UpdateAll cannot zero it.
	OpticalKind string `json:"optical_kind" gorm:"type:varchar(32);index"`
	// OpticalKindCF is the normalized NetBox custom-field value from the
	// last sync ("" if missing/invalid). Persisted so mapping CRUD can
	// re-resolve without calling NetBox.
	OpticalKindCF string `json:"optical_kind_cf" gorm:"type:varchar(32)"`

	LibrenmsID uint `json:"librenms_id"`

	// Custom fields
	CfAlarmTimeperiod  string      `json:"cf_alarm_timeperiod" gorm:"type:varchar(255)"`
	CfAlarmDestination string      `json:"cf_alarm_destination" gorm:"type:varchar(255)"`
	CfAlarmInterfaces  bool        `json:"cf_alarm_interfaces"`
	CfBackupOxidized   bool        `json:"cf_backup_oxidized"`
	CfConnectionMethod string      `json:"cf_connection_method" gorm:"type:varchar(255)"`
	CfLocation         string      `json:"cf_location" gorm:"type:varchar(255)"`
	CfMonitorGrafana   bool        `json:"cf_monitor_grafana"`
	CfMonitorIcinga    bool        `json:"cf_monitor_icinga"`
	CfMonitorLibrenms  bool        `json:"cf_monitor_librenms"`
	CfSource           string      `json:"cf_source" gorm:"type:varchar(255)"`
	CfSourceID         uint        `json:"cf_source_id"`
	Interfaces         []Interface `json:"interfaces"` // gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
	Tags               []Tag       `json:"tags"`
}

type Interface struct {
	FactumModel
	DeviceID    uint   `json:"device_id" gorm:"uniqueIndex:idx_interfaces_device_id_netbox_id"`
	NetboxID    uint   `json:"netbox_id" gorm:"uniqueIndex:idx_interfaces_device_id_netbox_id"`
	Name        string `json:"name" gorm:"type:varchar(255)"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	Enabled     bool   `json:"enabled"`
	VRF         string `json:"vrf" gorm:"type:varchar(255)"`
	CfRole      string `json:"cf_role" gorm:"type:varchar(255)"`
	// Type is Netbox's interface type (e.g. "1000base-t") - mirrors
	// netboxtool.NBInterface.Type.
	Type string `json:"type" gorm:"type:varchar(255)"`
	// CableID is the Netbox ID of the cable terminated on this interface,
	// 0 if none - mirrors netboxtool.NBInterface.CableID.
	CableID uint `json:"cable_id"`
	// Label is Netbox's free-text interface label, distinct from Name -
	// mirrors netboxtool.NBInterface.Label.
	Label string `json:"label" gorm:"type:varchar(255)"`
	// ParentID is the Netbox ID of this interface's parent interface, 0 if
	// none - mirrors netboxtool.NBInterface.ParentID.
	ParentID uint `json:"parent_id"`
	// UntaggedVLAN/TaggedVLANs are VIDs (not Netbox IDs), mirroring
	// netboxtool.NBInterface.UntaggedVLAN/TaggedVLANs. UntaggedVLAN is 0 if
	// unset.
	UntaggedVLAN int   `json:"untagged_vlan"`
	TaggedVLANs  []int `json:"tagged_vlans" gorm:"serializer:json"`
	// VLANNames maps VID -> Netbox VLAN name for this interface's untagged
	// and tagged VLANs, copied from netboxtool.NBInterface.VLANNames on
	// sync. Used by the VLAN matrix to label columns.
	VLANNames map[int]string `json:"vlan_names,omitempty" gorm:"serializer:json"`
	// SwitchportMode is "access", "trunk" or "dot1q-tunnel" (Q-in-Q), mirroring
	// drivers.Interface.SwitchportMode - only meaningful on global-VLAN
	// platforms (EOS, VRP), "" if not a switchport or unknown.
	SwitchportMode string `json:"switchport_mode" gorm:"type:varchar(255)"`

	LibrenmsID uint `json:"librenms_id"`

	// runtime data
	LineProtocolStatus string `gorm:"-"`
	InterfaceStatus    string `gorm:"-"`

	Addresses []Address `json:"addresses"` // gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
	Tags      []Tag     `json:"tags"`

	// Services lists the factum services terminating on this interface,
	// assembled by fetchDevices (web/handle_dcim.go) - not a DB relation
	// on Interface itself. Terminations come from service_endpoints; when
	// a per-VLAN subinterface exists, the service is attached to that
	// subinterface row instead of the physical port.
	Services []InterfaceServiceRef `json:"services,omitempty" gorm:"-"`
	// Optical is assembled by fetchDevices — not a DB relation on Interface.
	Optical *OpticalPort `json:"optical,omitempty" gorm:"-"`
}

// SwitchportModeToNetboxMode maps a driver/factum SwitchportMode value
// ("access", "trunk", "dot1q-tunnel") to Netbox's interface "mode" field
// ("access", "tagged", "q-in-q") - used when pushing a device-read
// switchport config to Netbox (internal/device-sync, and the interfaces/
// vlans web endpoint). "" (not a switchport) clears Netbox's mode.
func SwitchportModeToNetboxMode(mode string) any {
	switch mode {
	case "access":
		return "access"
	case "trunk":
		return "tagged"
	case "dot1q-tunnel":
		return "q-in-q"
	default:
		return nil
	}
}

// NetboxModeToSwitchportMode is the inverse of SwitchportModeToNetboxMode,
// used when syncing Netbox's interface "mode" into factum's own
// Interface.SwitchportMode (internal/netbox's syncInterfaces). Netbox's
// "tagged-all" (an untagged VLAN plus every VLAN in the assigned group,
// tagged) has no equivalent in this codebase's switchport vocabulary, so it
// maps to "trunk" like "tagged" does - both write "switchport trunk allowed
// vlan ..." in this codebase's driver-write direction.
func NetboxModeToSwitchportMode(mode string) string {
	switch mode {
	case "access":
		return "access"
	case "tagged", "tagged-all":
		return "trunk"
	case "q-in-q":
		return "dot1q-tunnel"
	default:
		return ""
	}
}

// InterfaceServiceRef is the minimal service info needed to link to and
// label a service from an interface listing, without pulling in the whole
// Service record.
type InterfaceServiceRef struct {
	ID        uint   `json:"id"`
	ServiceID string `json:"service_id"`
}

type Address struct {
	FactumModel
	AddressID   uint   `json:"address_id"`
	InterfaceID uint   `json:"interface_id" gorm:"index"`
	NetboxID    uint   `json:"netbox_id"`
	Address     string `json:"address" gorm:"type:varchar(80)"`
	// VRF is the name of the VRF this address belongs to, "" for
	// global/default VRF - mirrors netboxtool.NBAddress.VRF.
	VRF string `json:"vrf" gorm:"type:varchar(255)"`
	// Role is Netbox's native ipam.IPAddress.role (e.g. "anycast"), not a
	// custom field - hence no Cf prefix, unlike Interface.CfRole.
	Role string `json:"role" gorm:"type:varchar(255)"`
}

// Connection is a Netbox cable directly connecting two device interfaces,
// synced read-only from Netbox (internal/netbox.syncCables) - covers every
// interface-to-interface cable Netbox knows about, not just the
// LLDP-discovered ones internal/device-sync creates there.
type Connection struct {
	FactumModel
	NetboxID     uint   `json:"netbox_id" gorm:"uniqueIndex"`
	DeviceAID    uint   `json:"device_a_id" gorm:"index"`
	InterfaceAID uint   `json:"interface_a_id" gorm:"index"`
	DeviceBID    uint   `json:"device_b_id" gorm:"index"`
	InterfaceBID uint   `json:"interface_b_id" gorm:"index"`
	Label        string `json:"label" gorm:"type:varchar(255)"`
}

// Site is a Netbox dcim.Site, synced independently of Device (see
// internal/netbox.syncSites) so a site with GPS coordinates but no devices
// still has somewhere to be recorded - Device.Site/SiteID alone only ever
// reference sites that have at least one device.
type Site struct {
	FactumModel
	NetboxID  uint    `json:"netbox_id" gorm:"uniqueIndex"`
	Name      string  `json:"name" gorm:"type:varchar(255)"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Tag is shared by device-tags and interface-tags: exactly one of
// DeviceID/InterfaceID is set, the other is nil (SQL NULL). They are
// pointers rather than plain uint so the unused side is stored as NULL,
// not 0 -- the fk_devices_tags/fk_interfaces_tags constraints reject a
// literal 0 since no device/interface has id 0.
type Tag struct {
	FactumModel
	DeviceID    *uint  `json:"device_id"`
	InterfaceID *uint  `json:"interface_id"`
	NetboxID    uint   `json:"netbox_id"`
	Name        string `json:"name" gorm:"type:varchar(255)"`
}

// IsTag reports whether the device has a tag with the given name.
func (d *Device) IsTag(name string) bool {
	return hasTag(d.Tags, name)
}

// IsTag reports whether the interface has a tag with the given name.
func (i *Interface) IsTag(name string) bool {
	return hasTag(i.Tags, name)
}

func hasTag(tags []Tag, name string) bool {
	for _, t := range tags {
		if t.Name == name {
			return true
		}
	}
	return false
}
