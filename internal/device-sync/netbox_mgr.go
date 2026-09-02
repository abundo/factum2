package devicesync

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

// NetboxAPI narrows *netboxtool.NetboxClient to what NetboxMgr needs, so
// tests can substitute a fake - same shape as drivers.DriverClient.
type NetboxAPI interface {
	GetDevices() ([]*netboxtool.NBDevice, error)
	GetDevice(name string, id int) (*netboxtool.NBDevice, error)
	GetDeviceType(manufacturer, model string) (*netboxtool.NetboxDeviceTypeDetail, error)

	CreateInterfaceWithOptions(deviceID uint, name string, extra map[string]any) (*netboxtool.NetboxInterfaceREST, error)
	InterfaceUpdate(interfaceID int, changes map[string]any) error
	InterfaceDelete(interfaceID int) error

	CreateInterfaceAddress(interfaceID uint, address string, extra map[string]any) (*netboxtool.NBAddress, error)
	AddressUpdate(addressID int, changes map[string]any) error
	AddressDelete(addressID int) error

	GetCable(cableID uint) (*netboxtool.NBCable, error)
	CreateCableWithOptions(aInterfaceID, bInterfaceID uint, extra map[string]any) (*netboxtool.NBCable, error)
	DeleteCable(cableID uint) error
	UpdateCableTermination(cableID uint, side int, interfaceID uint) error

	GetPrefix(prefix string) (*netboxtool.NBPrefix, error)
	CreatePrefix(prefix string) (*netboxtool.NBPrefix, error)

	GetVlanGroup(name string) (*netboxtool.NBVlanGroup, error)
	CreateVlanGroup(name, slug string) (*netboxtool.NBVlanGroup, error)
	GetVlan(vid int, groupID uint) (*netboxtool.NBVlan, error)
	CreateVlan(vid int, name string, groupID uint) (*netboxtool.NBVlan, error)
	UpdateVlan(id uint, changes map[string]any) error

	GetL2VPNByName(name string) (*netboxtool.NBL2VPN, error)
	GetL2VPNByIdentifier(identifier int) (*netboxtool.NBL2VPN, error)
	CreateL2VPN(name, slug, l2vpnType string, identifier int) (*netboxtool.NBL2VPN, error)
	UpdateL2VPN(l2vpnID uint, changes map[string]any) error
	GetL2VPNTerminations(l2vpnID uint) ([]*netboxtool.NBL2VPNTermination, error)
	CreateL2VPNTermination(l2vpnID, interfaceID uint) (*netboxtool.NBL2VPNTermination, error)

	GetVRFByName(name string) (*netboxtool.NBVRF, error)
	CreateVRF(name, rd, description string) (*netboxtool.NBVRF, error)

	UpdateDevice(deviceID uint, changes map[string]any) error
}

// NetboxMgr wraps NetboxAPI with netboxtool's optional Cache (so repeated
// device-type lookups within one sync run don't re-fetch) and emits a
// jobevent.Reporter line for every change made. Device/interface/address
// reads no longer go through NetboxMgr at all - see DeviceSync.factum -
// this is now purely the Netbox write path plus device-type template
// lookups.
//
// sharedMu guards every access to cache and the EnsurePrefix check-then-create
// below: netboxtool.Cache is documented as not safe for concurrent use, and
// EnsurePrefix's exists-check + create is its own tiny race if two devices'
// pairs are synced concurrently (device-sync.go's forEachPairParallel) and
// share a prefix. The rest of NetboxMgr's methods are plain one-shot API
// calls with no shared mutable state, so they're left unlocked - serializing
// them too would defeat the point of syncing pairs in parallel.
type NetboxMgr struct {
	api      NetboxAPI
	reporter jobevent.Reporter
	cache    *netboxtool.Cache
	sharedMu sync.Mutex

	// vlanGroupID is the resolved Netbox ID of the single global VLAN group
	// (Settings.DeviceSyncVlanGroupName), set once by EnsureVlanGroup at the
	// start of a run. vlanIDsByVID maps a VID to that VLAN's Netbox ID
	// within the group, populated by EnsureVlan and read by
	// DeviceSync.syncInterfaceVlans to build untagged_vlan/tagged_vlans
	// PATCH payloads - both guarded by sharedMu like EnsurePrefix's
	// check-then-create below.
	vlanGroupID  uint
	vlanIDsByVID map[int]uint
}

func NewNetboxMgr(api NetboxAPI, reporter jobevent.Reporter) *NetboxMgr {
	return &NetboxMgr{
		api:      api,
		reporter: reporter,
		// netboxtool.NewCache still requires a CacheSource with
		// GetDevices/GetDevice/GetDeviceType (interface-to-interface
		// assignability), even though only TemplateInterfaceTypes (backed by
		// GetDeviceType) is used below - device/interface/address reads come
		// from factum now, see DeviceSync.factum.
		cache:        netboxtool.NewCache(api),
		vlanIDsByVID: map[int]uint{},
	}
}

// TemplateInterfaceTypes returns the Netbox interface type (e.g.
// "1000base-t") of every interface defined by a device type's template
// (manufacturer+model), keyed by interface name - the template's own type
// is admin/vendor-curated per-port data, more accurate than anything a
// parsed running-config can tell us, so device-sync.go prefers it over its
// own guessed type (see interfacesCreate/syncInterfaceValues) wherever an
// interface is template-defined. Presence in the map (regardless of value)
// is also what interfacesDelete uses to avoid deleting a Netbox interface
// that only exists because of the template (Netbox, or an admin
// re-applying the template, would just recreate it) - port of the
// `nb_ifname in nb_device._device_type.interfaces_name` guard in
// netbox_mgr.py's interfaces_delete. Results are cached per
// manufacturer/model for the life of the NetboxMgr - see netboxtool.Cache.
func (m *NetboxMgr) TemplateInterfaceTypes(manufacturer, model string) (map[string]string, error) {
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()
	return m.cache.TemplateInterfaceTypes(manufacturer, model)
}

// ----- Interface -----

// CreateInterface creates a Netbox interface from a parsed device
// interface. Every driver's parser always sets a concrete Type (physical
// interfaces get "other", not "virtual" - see e.g. eosInterfaceType -
// Netbox rejects terminating a cable on a "virtual" interface), but Type is
// only sent here when non-empty as a defensive fallback rather than sending
// Netbox an empty value for that required field. iface.Parent, if set (see
// e.g. srosAddSapInterface), is resolved by name against device.Interfaces -
// which by this point already includes any earlier-in-this-phase creation
// (interfacesCreate appends in-memory, see its own comment) - so a parent
// created earlier in the same run is found without a re-fetch; a Parent
// that doesn't resolve is dropped with a warning rather than sent as a
// missing/zero Netbox ID.
func (m *NetboxMgr) CreateInterface(device *models.Device, iface *drivers.Interface) (*netboxtool.NetboxInterfaceREST, error) {
	extra := map[string]any{"description": iface.Description}
	if iface.Type != "" {
		extra["type"] = iface.Type
	}
	if iface.VRF != "" {
		extra["vrf"] = map[string]string{"name": iface.VRF}
	}
	if iface.Label != "" {
		extra["label"] = iface.Label
	}
	if iface.Parent != "" {
		if parent := findInterfaceByName(device.Interfaces, iface.Parent); parent != nil {
			extra["parent"] = parent.NetboxID
		} else {
			m.reporter.Emit(jobevent.Warning, "%s: create interface %s: parent %s not found in netbox, leaving unset", device.Name, iface.Name, iface.Parent)
		}
	}
	created, err := m.api.CreateInterfaceWithOptions(device.NetboxID, iface.Name, extra)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "%s: create interface %s: %v", device.Name, iface.Name, err)
		return nil, err
	}
	m.reporter.Emit(jobevent.Info, "%s: created interface %s (description=%q vrf=%q)", device.Name, iface.Name, iface.Description, iface.VRF)
	return created, nil
}

// UpdateInterface applies changes (field -> new value) to an existing
// interface.
func (m *NetboxMgr) UpdateInterface(device *models.Device, iface *models.Interface, changes map[string]any) error {
	if err := m.api.InterfaceUpdate(int(iface.NetboxID), changes); err != nil {
		m.reporter.Emit(jobevent.Error, "%s: update interface %s: %v", device.Name, iface.Name, err)
		return err
	}
	for field, value := range changes {
		m.reporter.Emit(jobevent.Info, "%s: interface %s: %s -> %v", device.Name, iface.Name, field, value)
	}
	return nil
}

func (m *NetboxMgr) UpdateDevice(device *models.Device, changes map[string]any) error {
	if err := m.api.UpdateDevice(device.NetboxID, changes); err != nil {
		m.reporter.Emit(jobevent.Error, "%s: update device: %v", device.Name, err)
		return err
	}
	for field, value := range changes {
		m.reporter.Emit(jobevent.Info, "%s: %s -> %v", device.Name, field, value)
	}
	return nil
}

func (m *NetboxMgr) DeleteInterface(device *models.Device, iface *models.Interface) error {
	if err := m.api.InterfaceDelete(int(iface.NetboxID)); err != nil {
		m.reporter.Emit(jobevent.Error, "%s: delete interface %s: %v", device.Name, iface.Name, err)
		return err
	}
	m.reporter.Emit(jobevent.Info, "%s: deleted interface %s", device.Name, iface.Name)
	return nil
}

// ----- Address -----

// CreateAddress creates a Netbox address on iface. extra carries whatever
// REST fields the caller has already decided on (role/vrf) - see
// device-sync.go's addressesCreate, the Go equivalent of main.py's
// address_create.
func (m *NetboxMgr) CreateAddress(device *models.Device, iface *models.Interface, address string, extra map[string]any) (*netboxtool.NBAddress, error) {
	created, err := m.api.CreateInterfaceAddress(iface.NetboxID, address, extra)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "%s: interface %s: create address %s: %v", device.Name, iface.Name, address, err)
		return nil, err
	}
	m.reporter.Emit(jobevent.Info, "%s: interface %s: created address %s", device.Name, iface.Name, address)
	return created, nil
}

func (m *NetboxMgr) UpdateAddress(device *models.Device, iface *models.Interface, addr *models.Address, changes map[string]any) error {
	if err := m.api.AddressUpdate(int(addr.NetboxID), changes); err != nil {
		m.reporter.Emit(jobevent.Error, "%s: interface %s: update address %s: %v", device.Name, iface.Name, addr.Address, err)
		return err
	}
	for field, value := range changes {
		m.reporter.Emit(jobevent.Info, "%s: interface %s: address %s: %s -> %v", device.Name, iface.Name, addr.Address, field, value)
	}
	return nil
}

func (m *NetboxMgr) DeleteAddress(device *models.Device, iface *models.Interface, addr *models.Address) error {
	if err := m.api.AddressDelete(int(addr.NetboxID)); err != nil {
		m.reporter.Emit(jobevent.Error, "%s: interface %s: delete address %s: %v", device.Name, iface.Name, addr.Address, err)
		return err
	}
	m.reporter.Emit(jobevent.Info, "%s: interface %s: deleted address %s", device.Name, iface.Name, addr.Address)
	return nil
}

// ----- Cable -----

func (m *NetboxMgr) CreateCable(device, remoteDevice *models.Device, localIface, remoteIface *models.Interface) error {
	if _, err := netbox.CreateLLDPCable(m.api, localIface.NetboxID, remoteIface.NetboxID); err != nil {
		m.reporter.Emit(jobevent.Error, "%s: interface %s: create connection to %s %s: %v", device.Name, localIface.Name, remoteDevice.Name, remoteIface.Name, err)
		return err
	}
	m.reporter.Emit(jobevent.Info, "%s: interface %s: connected to %s %s", device.Name, localIface.Name, remoteDevice.Name, remoteIface.Name)
	return nil
}

func (m *NetboxMgr) DeleteCable(cable *netboxtool.NBCable) error {
	if err := m.api.DeleteCable(cable.NetboxID); err != nil {
		m.reporter.Emit(jobevent.Error, "delete connection %d: %v", cable.NetboxID, err)
		return err
	}
	m.reporter.Emit(jobevent.Info, "deleted connection %d", cable.NetboxID)
	return nil
}

// SetConnectionTermination replaces one side (1=A, 2=B) of an existing
// cable with iface.
func (m *NetboxMgr) SetConnectionTermination(device *models.Device, cable *netboxtool.NBCable, side int, iface *models.Interface) error {
	if err := m.api.UpdateCableTermination(cable.NetboxID, side, iface.NetboxID); err != nil {
		m.reporter.Emit(jobevent.Error, "%s: update connection %d: %v", device.Name, cable.NetboxID, err)
		return err
	}
	m.reporter.Emit(jobevent.Info, "%s: updated connection %d side %d to interface %s", device.Name, cable.NetboxID, side, iface.Name)
	return nil
}

// ----- Prefix -----

// EnsurePrefix creates prefix (a CIDR network literal) if it doesn't
// already exist. Locked because it's a check-then-create: two devices whose
// addresses fall in the same prefix, synced concurrently, would otherwise
// both see it missing and race to create it.
func (m *NetboxMgr) EnsurePrefix(prefix string) error {
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()
	existing, err := m.api.GetPrefix(prefix)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	if _, err := m.api.CreatePrefix(prefix); err != nil {
		m.reporter.Emit(jobevent.Error, "create prefix %s: %v", prefix, err)
		return err
	}
	m.reporter.Emit(jobevent.Info, "created prefix %s", prefix)
	return nil
}

// ----- VLAN -----

// vlanSlugInvalidChars matches runs of characters not valid in a Netbox
// slug - mirrors internal/netbox/factum2-netbox.go's slugify, duplicated
// rather than shared since device-sync doesn't otherwise depend on that
// package and this is the only place here that needs one.
var vlanSlugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)

func vlanGroupSlug(name string) string {
	s := vlanSlugInvalidChars.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if len(s) > 100 {
		s = strings.Trim(s[:100], "-")
	}
	if s == "" {
		s = "vlan-group"
	}
	return s
}

// EnsureVlanGroup resolves name to a Netbox VLAN group ID, creating a new
// global (unscoped) group if none exists, and memoizes the result on
// m.vlanGroupID - safe to call more than once per run, but DeviceSync.run
// only ever does so once, before any per-device phase.
func (m *NetboxMgr) EnsureVlanGroup(name string) (uint, error) {
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()

	existing, err := m.api.GetVlanGroup(name)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		m.vlanGroupID = existing.NetboxID
		return m.vlanGroupID, nil
	}
	created, err := m.api.CreateVlanGroup(name, vlanGroupSlug(name))
	if err != nil {
		m.reporter.Emit(jobevent.Error, "create netbox vlan group %s: %v", name, err)
		return 0, err
	}
	m.reporter.Emit(jobevent.Info, "created netbox vlan group %s", name)
	m.vlanGroupID = created.NetboxID
	return m.vlanGroupID, nil
}

// EnsureVlan creates vid in the resolved VLAN group if it doesn't already
// exist there, or updates its name if it does but the name differs.
// Records the VLAN's Netbox ID in m.vlanIDsByVID either way, for
// DeviceSync.syncInterfaceVlans to resolve later. Locked because it's a
// check-then-create/update, same race as EnsurePrefix: two devices sharing
// a VID, synced concurrently, would otherwise both see it missing and race
// to create it.
func (m *NetboxMgr) EnsureVlan(vid int, name string) (*netboxtool.NBVlan, error) {
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()

	existing, err := m.api.GetVlan(vid, m.vlanGroupID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Name != name {
			if err := m.api.UpdateVlan(existing.NetboxID, map[string]any{"name": name}); err != nil {
				m.reporter.Emit(jobevent.Error, "update vlan %d name: %v", vid, err)
				return nil, err
			}
			m.reporter.Emit(jobevent.Info, "vlan %d: name -> %q", vid, name)
			existing.Name = name
		}
		m.vlanIDsByVID[vid] = existing.NetboxID
		return existing, nil
	}
	created, err := m.api.CreateVlan(vid, name, m.vlanGroupID)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "create vlan %d (%s): %v", vid, name, err)
		return nil, err
	}
	m.reporter.Emit(jobevent.Info, "created vlan %d (%s)", vid, name)
	m.vlanIDsByVID[vid] = created.NetboxID
	return created, nil
}

// VlanNetboxID returns the Netbox ID of the VLAN with this VID in the
// resolved group (populated by EnsureVlan), and whether it's known.
func (m *NetboxMgr) VlanNetboxID(vid int) (uint, bool) {
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()
	id, ok := m.vlanIDsByVID[vid]
	return id, ok
}

// ----- VRF (L3VPN) -----

// EnsureVRF finds or creates a Netbox VRF by name. RD is set on create
// only; an existing VRF is left alone (device-sync does not rename or
// rewrite operator-owned VRFs). Locked because two devices in the same
// run can both see a new L3VPN and race to create it.
func (m *NetboxMgr) EnsureVRF(deviceName, name, rd, description string) (*netboxtool.NBVRF, error) {
	if name == "" {
		return nil, fmt.Errorf("vrf name is empty")
	}
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()

	existing, err := m.api.GetVRFByName(name)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "%s: lookup vrf %q: %v", deviceName, name, err)
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	created, err := m.api.CreateVRF(name, rd, description)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "%s: create vrf %q: %v", deviceName, name, err)
		return nil, err
	}
	m.reporter.Emit(jobevent.Info, "%s: created vrf %q rd=%q", deviceName, name, rd)
	return created, nil
}

// ----- L2VPN (ELINE → EVPL, ELAN → VPLS) -----

// l2vpnSlug turns an on-device ELINE name into a Netbox-safe slug. Same
// rules as vlanGroupSlug - Netbox accepts [a-z0-9-] only, max 100 chars.
func l2vpnSlug(name string) string {
	s := vlanSlugInvalidChars.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if len(s) > 100 {
		s = strings.Trim(s[:100], "-")
	}
	if s == "" {
		s = "l2vpn"
	}
	return s
}

// EnsureL2VPN finds or creates a Netbox L2VPN of l2vpnType (evpl, vpls,
// …) for an on-device service. Lookup order: exact name, then (when
// identifier > 0) identifier - so a service-provisioned L2VPN named after
// the ServiceID is reused when the device still carries that same
// identifier under a matching or near-matching name. When found by
// identifier alone the existing name is left alone (device-sync does not
// rename service-owned L2VPNs). When creating, identifier is only sent if
// > 0 (same-device patches often have no PWID). Locked because the
// name/identifier check-then-create races when both ends of a
// cross-device service are synced concurrently in the same run.
func (m *NetboxMgr) EnsureL2VPN(deviceName, name, l2vpnType string, identifier int) (*netboxtool.NBL2VPN, error) {
	if l2vpnType == "" {
		l2vpnType = models.NetboxTypeEVPL
	}
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()

	existing, err := m.api.GetL2VPNByName(name)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "%s: lookup l2vpn %q: %v", deviceName, name, err)
		return nil, err
	}
	if existing == nil && identifier > 0 {
		existing, err = m.api.GetL2VPNByIdentifier(identifier)
		if err != nil {
			m.reporter.Emit(jobevent.Error, "%s: lookup l2vpn identifier %d: %v", deviceName, identifier, err)
			return nil, err
		}
	}
	if existing != nil {
		if identifier > 0 && existing.Identifier != identifier {
			if err := m.api.UpdateL2VPN(existing.NetboxID, map[string]any{"identifier": identifier}); err != nil {
				m.reporter.Emit(jobevent.Error, "%s: update l2vpn %q identifier: %v", deviceName, existing.Name, err)
				return nil, err
			}
			m.reporter.Emit(jobevent.Info, "%s: l2vpn %q: identifier -> %d", deviceName, existing.Name, identifier)
			existing.Identifier = identifier
		}
		return existing, nil
	}

	created, err := m.api.CreateL2VPN(name, l2vpnSlug(name), l2vpnType, identifier)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "%s: create l2vpn %q (%s): %v", deviceName, name, l2vpnType, err)
		return nil, err
	}
	m.reporter.Emit(jobevent.Info, "%s: created l2vpn %q type=%s identifier=%d", deviceName, name, l2vpnType, identifier)
	return created, nil
}

// GetL2VPNTerminations returns every termination currently on l2vpnID.
func (m *NetboxMgr) GetL2VPNTerminations(l2vpnID uint) ([]*netboxtool.NBL2VPNTermination, error) {
	return m.api.GetL2VPNTerminations(l2vpnID)
}

// EnsureL2VPNTermination creates a termination of l2vpn on interfaceID
// unless one already exists. terms is the current termination list for
// that L2VPN (caller-fetched); on success the new termination is appended
// in-memory so a second call in the same phase sees it. Locked with
// sharedMu so two concurrent ends of the same L2VPN don't race to create
// the same termination.
func (m *NetboxMgr) EnsureL2VPNTermination(deviceName, l2vpnName string, l2vpnID, interfaceID uint, ifaceName string, terms *[]*netboxtool.NBL2VPNTermination) error {
	m.sharedMu.Lock()
	defer m.sharedMu.Unlock()

	for _, t := range *terms {
		if t.InterfaceID == interfaceID {
			return nil
		}
	}
	created, err := m.api.CreateL2VPNTermination(l2vpnID, interfaceID)
	if err != nil {
		m.reporter.Emit(jobevent.Error, "%s: l2vpn %q: create termination on %s: %v", deviceName, l2vpnName, ifaceName, err)
		return err
	}
	m.reporter.Emit(jobevent.Info, "%s: l2vpn %q: terminated on interface %s", deviceName, l2vpnName, ifaceName)
	*terms = append(*terms, created)
	return nil
}

// BuildInterfaceVlanChanges builds a Netbox interface PATCH payload
// (untagged_vlan/qinq_svlan, tagged_vlans, mode) for an explicit "set this
// interface's switchport state" edit - the web-triggered counterpart of
// DeviceSync.syncInterfaceVlans's diff-against-last-sync PATCH building
// (device-sync.go), which this deliberately doesn't share: that path is a
// best-effort background reconciliation that skips whatever VID didn't
// resolve and sends only the fields that changed, while this one is a
// single explicit user edit that should fail loudly - and atomically, all
// fields together - if any referenced VID wasn't ensured first via
// EnsureVlan. Every non-zero VID in untaggedVID/taggedVIDs must already be
// known to VlanNetboxID.
func (m *NetboxMgr) BuildInterfaceVlanChanges(mode string, untaggedVID int, taggedVIDs []int) (map[string]any, error) {
	changes := map[string]any{"mode": models.SwitchportModeToNetboxMode(mode)}

	vlanField := "untagged_vlan"
	if mode == "dot1q-tunnel" {
		vlanField = "qinq_svlan"
	}
	if untaggedVID == 0 {
		changes[vlanField] = nil
	} else if id, ok := m.VlanNetboxID(untaggedVID); ok {
		changes[vlanField] = id
	} else {
		return nil, fmt.Errorf("vlan %d not synced to netbox", untaggedVID)
	}

	tagged := make([]uint, 0, len(taggedVIDs))
	for _, vid := range taggedVIDs {
		id, ok := m.VlanNetboxID(vid)
		if !ok {
			return nil, fmt.Errorf("vlan %d not synced to netbox", vid)
		}
		tagged = append(tagged, id)
	}
	changes["tagged_vlans"] = tagged

	return changes, nil
}
