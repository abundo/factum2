package models

import (
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// import "github.com/abundo/netboxtool"

// --------------------------------------------------------------------------
//	Netbox
// --------------------------------------------------------------------------

type Device struct {
	FactumModel
	// Netbox's dcim.Device and virtualization.VirtualMachine tables have
	// independent ID sequences, so NetboxID is only unique per VM value,
	// never globally - see internal/netbox.syncDevice.
	// Local (Factum-created) devices keep NetboxID=0; uniqueness is a
	// partial index WHERE netbox_id <> 0 so many local rows can coexist.
	VM             bool   `json:"vm"`
	NetboxID       uint   `json:"netbox_id"`
	Name           string `json:"name" gorm:"type:varchar(255)"`
	Comments       string `json:"comments" gorm:"type:varchar(255)"`
	Enabled        bool   `json:"enabled"`
	Manufacturer   string `json:"manufacturer" gorm:"type:varchar(255)"`
	ManufacturerID uint   `json:"manufacturer_id"`
	ModelName      string `json:"model_name" gorm:"type:varchar(255)"`
	ModelID        uint   `json:"model_id"`
	// DeviceTypeID is the Factum catalog row (models.DeviceType), not
	// NetBox's device-type id (that is ModelID). 0 if unset.
	DeviceTypeID  uint   `json:"device_type_id" gorm:"index"`
	Platform      string `json:"platform" gorm:"type:varchar(255)"`
	PlatformID    uint   `json:"platform_id"`
	PrimaryIPv4   string `json:"primary_ipv4" gorm:"type:varchar(255)"`
	PrimaryIPv4ID uint   `json:"primary_ipv4_id"`
	PrimaryIPv6   string `json:"primary_ipv6" gorm:"type:varchar(255)"`
	PrimaryIPv6ID uint   `json:"primary_ipv6_id"`
	Role          string `json:"role" gorm:"type:varchar(255)"`
	RoleID        uint   `json:"role_id"`
	Site          string `json:"site" gorm:"type:varchar(255)"`
	SiteID        uint   `json:"site_id"`
	Status        string `json:"status" gorm:"type:varchar(255)"`
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
	DeviceID uint `json:"device_id" gorm:"index"`
	// NetboxID uniqueness is a partial index WHERE netbox_id <> 0 so many
	// Factum-local interfaces (netbox_id=0) can coexist on one device.
	NetboxID    uint   `json:"netbox_id"`
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
	// platforms (EOS, VRP, Cisco SMB), "" if not a switchport or unknown.
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
	// DNSName is Netbox ipam.IPAddress.dns_name, validated at Netbox sync.
	DNSName string `json:"dns_name" gorm:"type:varchar(255)"`
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

const (
	SiteSourceFactum = "factum"
	SiteSourceNetbox = "netbox"

	// SiteNetboxKind* is the NetBox object a synced row came from.
	// Factum-created sites leave this empty. Region/site/location IDs
	// occupy separate NetBox sequences, so uniqueness is (kind, id).
	SiteNetboxKindRegion   = "region"
	SiteNetboxKindSite     = "site"
	SiteNetboxKindLocation = "location"
)

// Site is one node in the Organization sites tree. NetBox regions, sites
// and locations all map onto this table (parented the same way they nest
// in NetBox); operators can also create sites here (source=factum). GPS
// is optional — the network map only plots rows that have coordinates.
type Site struct {
	FactumModel
	ParentID   *uint   `json:"parent_id" gorm:"index"`
	Name       string  `json:"name" gorm:"type:varchar(255);not null"`
	Slug       string  `json:"slug" gorm:"type:varchar(255)"`
	Source     string  `json:"source" gorm:"type:varchar(32)"`
	NetboxKind string  `json:"netbox_kind" gorm:"type:varchar(32)"`
	NetboxID   uint    `json:"netbox_id"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
}

// SiteDTO is the create/update body for /api/sites. Source/NetboxKind/
// NetboxID are sync-managed and excluded so a caller cannot fake a NetBox
// origin or rewrite the import key.
type SiteDTO struct {
	ID        uint    `json:"id"`
	ParentID  *uint   `json:"parent_id"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (s *Site) BeforeCreate(tx *gorm.DB) error {
	if strings.TrimSpace(s.Slug) == "" {
		s.Slug = Slugify(s.Name)
	}
	if s.Source == "" {
		if s.NetboxID != 0 {
			s.Source = SiteSourceNetbox
		} else {
			s.Source = SiteSourceFactum
		}
	}
	if s.NetboxID != 0 && s.NetboxKind == "" {
		s.NetboxKind = SiteNetboxKindSite
	}
	return nil
}

func (s *Site) BeforeUpdate(tx *gorm.DB) error {
	if strings.TrimSpace(s.Slug) == "" {
		s.Slug = Slugify(s.Name)
	}
	return nil
}

func (s Site) HasCoordinates() bool {
	return s.Latitude != 0 || s.Longitude != 0
}

func (s Site) IsLocal() bool {
	return s.Source != SiteSourceNetbox
}

// Manufacturer is the shared DCIM catalog (NetBox dcim.Manufacturer).
// Source is "netbox" when upserted from sync, "factum" when created in the UI.
type Manufacturer struct {
	FactumModel
	Name     string `json:"name" gorm:"type:varchar(255);uniqueIndex;not null"`
	Slug     string `json:"slug" gorm:"type:varchar(255);uniqueIndex;not null"`
	Source   string `json:"source" gorm:"type:varchar(32)"`
	NetboxID uint   `json:"netbox_id"`
}

type ManufacturerDTO struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (m *Manufacturer) BeforeCreate(tx *gorm.DB) error {
	if strings.TrimSpace(m.Slug) == "" {
		m.Slug = Slugify(m.Name)
	}
	if m.Source == "" {
		m.Source = "factum"
	}
	return nil
}

func (m *Manufacturer) BeforeUpdate(tx *gorm.DB) error {
	if strings.TrimSpace(m.Slug) == "" {
		m.Slug = Slugify(m.Name)
	}
	return nil
}

// DeviceType is the shared DCIM catalog (NetBox dcim.DeviceType).
type DeviceType struct {
	FactumModel
	ManufacturerID uint   `json:"manufacturer_id" gorm:"index;not null"`
	Model          string `json:"model" gorm:"type:varchar(255);not null"`
	Slug           string `json:"slug" gorm:"type:varchar(255);not null"`
	Source         string `json:"source" gorm:"type:varchar(32)"`
	NetboxID       uint   `json:"netbox_id"`
}

type DeviceTypeDTO struct {
	ID             uint   `json:"id"`
	ManufacturerID uint   `json:"manufacturer_id"`
	Model          string `json:"model"`
	Slug           string `json:"slug"`
}

func (d *DeviceType) BeforeCreate(tx *gorm.DB) error {
	if strings.TrimSpace(d.Slug) == "" {
		d.Slug = Slugify(d.Model)
	}
	if d.Source == "" {
		d.Source = "factum"
	}
	return nil
}

func (d *DeviceType) BeforeUpdate(tx *gorm.DB) error {
	if strings.TrimSpace(d.Slug) == "" {
		d.Slug = Slugify(d.Model)
	}
	return nil
}

func (d *DeviceType) BeforeDelete(tx *gorm.DB) error {
	return tx.Where("device_type_id = ?", d.ID).Delete(&InterfaceTemplate{}).Error
}

// InterfaceTemplate is a port defined on a DeviceType (NetBox
// dcim.InterfaceTemplate). Copied onto a device when that device is
// created locally, and shown under DCIM → Device types.
type InterfaceTemplate struct {
	FactumModel
	DeviceTypeID uint   `json:"device_type_id" gorm:"uniqueIndex:idx_interface_templates_type_name;not null"`
	Name         string `json:"name" gorm:"uniqueIndex:idx_interface_templates_type_name;type:varchar(255);not null"`
	Type         string `json:"type" gorm:"type:varchar(255)"`
	Label        string `json:"label" gorm:"type:varchar(255)"`
	Description  string `json:"description" gorm:"type:varchar(255)"`
	Source       string `json:"source" gorm:"type:varchar(32)"`
	NetboxID     uint   `json:"netbox_id"`
}

type InterfaceTemplateDTO struct {
	ID           uint   `json:"id"`
	DeviceTypeID uint   `json:"device_type_id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Label        string `json:"label"`
	Description  string `json:"description"`
}

func (t *InterfaceTemplate) BeforeCreate(tx *gorm.DB) error {
	if t.Source == "" {
		t.Source = "factum"
	}
	return nil
}

// InterfaceCreateDTO is the POST/PUT /api/dcim/interfaces body for a
// Factum-local device interface (NetboxID stays 0).
type InterfaceCreateDTO struct {
	ID          uint   `json:"id"`
	DeviceID    uint   `json:"device_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

// Platform is the shared DCIM catalog (NetBox dcim.Platform).
// Slug is what drivers match on (eos, sros, vrp, …).
type Platform struct {
	FactumModel
	Name           string `json:"name" gorm:"type:varchar(255);uniqueIndex;not null"`
	Slug           string `json:"slug" gorm:"type:varchar(255);uniqueIndex;not null"`
	ManufacturerID uint   `json:"manufacturer_id"`
	Source         string `json:"source" gorm:"type:varchar(32)"`
	NetboxID       uint   `json:"netbox_id"`
}

type PlatformDTO struct {
	ID             uint   `json:"id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	ManufacturerID uint   `json:"manufacturer_id"`
}

func (p *Platform) BeforeCreate(tx *gorm.DB) error {
	if strings.TrimSpace(p.Slug) == "" {
		p.Slug = Slugify(p.Name)
	}
	if p.Source == "" {
		p.Source = "factum"
	}
	return nil
}

func (p *Platform) BeforeUpdate(tx *gorm.DB) error {
	if strings.TrimSpace(p.Slug) == "" {
		p.Slug = Slugify(p.Name)
	}
	return nil
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a catalog name into a URL/NetBox-style slug.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// DeviceCreateDTO is the POST/PUT /api/device body for a Factum-local device
// (NetboxID stays 0, CfSource is "factum"). Manufacturer/model/platform
// strings on Device are copied from the catalog rows at create time.
// Pointer bools distinguish omitted (leave existing / default) from false.
type DeviceCreateDTO struct {
	Name              string `json:"name"`
	DeviceTypeID      uint   `json:"device_type_id"`
	PlatformID        uint   `json:"platform_id"`
	Site              string `json:"site"`
	Role              string `json:"role"`
	Status            string `json:"status"`
	PrimaryIPv4       string `json:"primary_ipv4"`
	PrimaryIPv6       string `json:"primary_ipv6"`
	Comments          string `json:"comments"`
	Enabled           *bool  `json:"enabled"`
	CfLocation        string `json:"cf_location"`
	CfMonitorIcinga   *bool  `json:"cf_monitor_icinga"`
	CfMonitorLibrenms *bool  `json:"cf_monitor_librenms"`
	CfMonitorGrafana  *bool  `json:"cf_monitor_grafana"`
	CfBackupOxidized  *bool  `json:"cf_backup_oxidized"`
	CfAlarmInterfaces *bool  `json:"cf_alarm_interfaces"`
	OpticalKind       string `json:"optical_kind"`
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
