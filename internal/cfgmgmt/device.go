package cfgmgmt

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// DCIMDevice is the read-only inventory fragment templates may use.
type DCIMDevice struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	Site         string `json:"site"`
	Role         string `json:"role"`
	ModelName    string `json:"model_name"`
	Manufacturer string `json:"manufacturer"`
	Status       string `json:"status"`
	PrimaryIPv4  string `json:"primary_ipv4"`
	PrimaryIPv6  string `json:"primary_ipv6"`
}

// DCIMInterface is the read-only interface fragment templates may use.
type DCIMInterface struct {
	ID           uint     `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Enabled      bool     `json:"enabled"`
	Type         string   `json:"type"`
	UntaggedVLAN int      `json:"untagged_vlan"`
	TaggedVLANs  []int    `json:"tagged_vlans"`
	Addresses    []string `json:"addresses"`
}

func DCIMFromDevice(d *models.Device) DCIMDevice {
	if d == nil {
		return DCIMDevice{}
	}
	return DCIMDevice{
		ID: d.ID, Name: d.Name, Platform: d.Platform, Site: d.Site,
		Role: d.Role, ModelName: d.ModelName, Manufacturer: d.Manufacturer,
		Status: d.Status, PrimaryIPv4: d.PrimaryIPv4, PrimaryIPv6: d.PrimaryIPv6,
	}
}

func DCIMFromInterface(iface *models.Interface) DCIMInterface {
	if iface == nil {
		return DCIMInterface{}
	}
	addrs := make([]string, 0, len(iface.Addresses))
	for _, a := range iface.Addresses {
		addrs = append(addrs, a.Address)
	}
	return DCIMInterface{
		ID: iface.ID, Name: iface.Name, Description: iface.Description,
		Enabled: iface.Enabled, Type: iface.Type, UntaggedVLAN: iface.UntaggedVLAN,
		TaggedVLANs: iface.TaggedVLANs, Addresses: addrs,
	}
}

const (
	// DefaultELINEMTU/DefaultELINEControlWord are hardcoded for v1 rather
	// than service fields — matches the EOS fixture.
	DefaultELINEMTU         = 9100
	DefaultELINEControlWord = true

	FieldVLAN                 = "vlan"
	FieldSubinterfaceNetboxID = "subinterface_netbox_id"
	FieldTerminationNetboxID  = "termination_netbox_id"
)

type ELINERemote struct {
	NeighborIP   string
	PseudowireID int
	MTU          int
	ControlWord  bool
	DeviceName   string
	RemoteIface  string
	RemoteVLAN   int
}

type ELINEStale struct {
	Iface string
	VLAN  int
}

// SDPIDFromNeighbor is the SR OS shared SDP ID (last IPv4 octet).
func SDPIDFromNeighbor(neighborIP string) (int, error) {
	addr, err := netip.ParseAddr(neighborIP)
	if err != nil || !addr.Is4() {
		return 0, fmt.Errorf("neighbor address %q is not a valid IPv4 address", neighborIP)
	}
	last := int(addr.As4()[3])
	if last == 0 || last == 255 {
		return 0, fmt.Errorf("neighbor address %q has no usable SDP ID (last octet %d)", neighborIP, last)
	}
	return last, nil
}

func IsSROS(platform string) bool {
	p := NormalizePlatform(platform)
	return p == "sros" || p == "sros-md"
}

// GenericRenderData is the template context for platform packs. ELINE packs
// use the extra Peer/Remote/SDPID/Stale fields; other types leave them zero.
type GenericRenderData struct {
	Name               string
	Description        string
	ServiceNumericID   int
	Fields             map[string]any
	Endpoint           map[string]any
	Vars               map[string]any
	Device             DCIMDevice
	Interface          DCIMInterface
	LocalIface         string
	LocalVLAN          int
	Role               string
	PeerLocalIface     string
	PeerLocalVLAN      int
	Remote             *ELINERemote
	StaleSubinterfaces []ELINEStale
	SDPID              int
}

func fieldsMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func loadInterface(db *gorm.DB, id uint) (*models.Interface, error) {
	var iface models.Interface
	if err := db.First(&iface, id).Error; err != nil {
		return nil, err
	}
	var addrs []models.Address
	_ = db.Where("interface_id = ?", id).Find(&addrs).Error
	iface.Addresses = addrs
	return &iface, nil
}

func loadDevice(db *gorm.DB, id uint) (*models.Device, error) {
	var d models.Device
	if err := db.First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func vlanFromFields(m map[string]any) int {
	v, ok := m[FieldVLAN]
	if !ok {
		return 0
	}
	n, ok := asInt(v)
	if !ok {
		return 0
	}
	return int(n)
}

func GenericData(db *gorm.DB, svc *models.Service, ep *models.ServiceEndpoint, device *models.Device, iface *models.Interface) (*GenericRenderData, error) {
	vars := map[string]any{}
	if iface != nil {
		m, err := ResolveMap(db, iface.ID)
		if err == nil {
			vars = m
		}
	}
	epFields := fieldsMap(ep.Fields)
	data := &GenericRenderData{
		Name:        svc.ServiceID,
		Description: svc.Comment,
		Fields:      fieldsMap(svc.Fields),
		Endpoint: map[string]any{
			"Role":        ep.Role,
			"DeviceID":    ep.DeviceID,
			"InterfaceID": ep.InterfaceID,
			"Fields":      epFields,
		},
		Vars:       vars,
		Device:     DCIMFromDevice(device),
		Interface:  DCIMFromInterface(iface),
		LocalIface: "",
		LocalVLAN:  vlanFromFields(epFields),
		Role:       ep.Role,
	}
	if iface != nil {
		data.LocalIface = iface.Name
	}
	if svc.PseudowireID != 0 {
		data.ServiceNumericID = svc.PseudowireID
	} else if n, ok := asInt(data.Fields["service_numeric_id"]); ok {
		data.ServiceNumericID = int(n)
	}
	if svc.ServiceType == "ELINE" {
		data.Description = elineDescription(svc.ServiceID, customerName(db, svc.CustomerID))
		siblings, err := ListEndpoints(db, svc.ID)
		if err != nil {
			return nil, err
		}
		if err := fillELINEPeer(db, svc, ep, device, data, siblings); err != nil {
			return nil, err
		}
		data.StaleSubinterfaces = staleOnDevice(db, svc, ep.DeviceID, siblings)
	}
	return data, nil
}

func fillELINEPeer(db *gorm.DB, svc *models.Service, ep *models.ServiceEndpoint, device *models.Device, data *GenericRenderData, siblings []models.ServiceEndpoint) error {
	other := otherEndpoint(ep, siblings)
	if other == nil {
		return nil
	}
	if other.DeviceID == ep.DeviceID {
		peerIface, err := loadInterface(db, other.InterfaceID)
		if err != nil {
			return err
		}
		data.PeerLocalIface = peerIface.Name
		data.PeerLocalVLAN = vlanFromFields(fieldsMap(other.Fields))
		return nil
	}
	peerDev, err := loadDevice(db, other.DeviceID)
	if err != nil {
		return err
	}
	peerIface, _ := loadInterface(db, other.InterfaceID)
	remoteIface := ""
	if peerIface != nil {
		remoteIface = peerIface.Name
	}
	neighbor := loopbackAddr(db, peerDev)
	if neighbor == "" {
		return fmt.Errorf("device %q has no %s address", peerDev.Name, loopbackIfaceName(peerDev.Platform))
	}
	data.Remote = &ELINERemote{
		NeighborIP:   neighbor,
		PseudowireID: svc.PseudowireID,
		MTU:          DefaultELINEMTU,
		ControlWord:  DefaultELINEControlWord,
		DeviceName:   peerDev.Name,
		RemoteIface:  remoteIface,
		RemoteVLAN:   vlanFromFields(fieldsMap(other.Fields)),
	}
	if IsSROS(device.Platform) {
		id, err := SDPIDFromNeighbor(neighbor)
		if err != nil {
			return err
		}
		data.SDPID = id
	}
	return nil
}

func otherEndpoint(ep *models.ServiceEndpoint, siblings []models.ServiceEndpoint) *models.ServiceEndpoint {
	for i := range siblings {
		s := &siblings[i]
		if ep.ID != 0 && s.ID == ep.ID {
			continue
		}
		if ep.ID == 0 && s.DeviceID == ep.DeviceID && s.InterfaceID == ep.InterfaceID && s.Role == ep.Role {
			continue
		}
		return s
	}
	return nil
}

func staleOnDevice(db *gorm.DB, svc *models.Service, deviceID uint, eps []models.ServiceEndpoint) []ELINEStale {
	current := map[string]bool{}
	for _, ep := range eps {
		if ep.DeviceID != deviceID {
			continue
		}
		iface, err := loadInterface(db, ep.InterfaceID)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%s\x00%d", iface.Name, vlanFromFields(fieldsMap(ep.Fields)))
		current[key] = true
	}
	applied := []struct {
		deviceID uint
		iface    string
		vlan     int
	}{
		{svc.AppliedEndpointADeviceID, svc.AppliedEndpointAIface, svc.AppliedEndpointAVlan},
		{svc.AppliedEndpointBDeviceID, svc.AppliedEndpointBIface, svc.AppliedEndpointBVlan},
	}
	var out []ELINEStale
	seen := map[string]bool{}
	for _, a := range applied {
		if a.deviceID != deviceID || a.iface == "" {
			continue
		}
		key := fmt.Sprintf("%s\x00%d", a.iface, a.vlan)
		if current[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ELINEStale{Iface: a.iface, VLAN: a.vlan})
	}
	return out
}

func loopbackIfaceName(platform string) string {
	if IsSROS(platform) {
		return "system"
	}
	return "Loopback0"
}

// RenderedSource is one template or service's rendered CLI (or other payload).
type RenderedSource struct {
	Source      string   `json:"source"`
	Kind        string   `json:"kind"` // template | service
	Platform    string   `json:"platform"`
	PayloadKind string   `json:"payload_kind"`
	Commands    []string `json:"commands"`
	Error       string   `json:"error,omitempty"`
}

type DeviceRender struct {
	DeviceID uint             `json:"device_id"`
	Name     string           `json:"name"`
	Platform string           `json:"platform"`
	Sources  []RenderedSource `json:"sources"`
}

func platformMatch(tmplPlatform, devicePlatform string) bool {
	if strings.TrimSpace(tmplPlatform) == "" {
		return true
	}
	return NormalizePlatform(tmplPlatform) == NormalizePlatform(devicePlatform)
}

func deviceScopeChain(db *gorm.DB, deviceID uint) (map[uint]bool, error) {
	s, err := scopeByDeviceID(db, deviceID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		root, err := RootScope(db)
		if err != nil {
			return nil, err
		}
		ids, err := ancestorIDs(db, root)
		if err != nil {
			return nil, err
		}
		return ids, nil
	}
	return ancestorIDs(db, s)
}

func loopbackAddr(db *gorm.DB, device *models.Device) string {
	name := loopbackIfaceName(device.Platform)
	var ifaces []models.Interface
	if err := db.Where("device_id = ? AND name = ?", device.ID, name).Find(&ifaces).Error; err != nil || len(ifaces) == 0 {
		return ""
	}
	var addrs []models.Address
	if err := db.Where("interface_id = ?", ifaces[0].ID).Find(&addrs).Error; err != nil || len(addrs) == 0 {
		return ""
	}
	addr, _, _ := strings.Cut(addrs[0].Address, "/")
	return addr
}

func customerName(db *gorm.DB, customerID uint) string {
	var c models.Customer
	if err := db.First(&c, customerID).Error; err != nil {
		return ""
	}
	return c.Name
}

func elineDescription(serviceID, customer string) string {
	return fmt.Sprintf("ID=%s %s", serviceID, customer)
}

func renderGenericForDevice(db *gorm.DB, svc *models.Service, device *models.Device, eps []models.ServiceEndpoint) []RenderedSource {
	pack, err := LookupPlatformPack(db, svc.ServiceType, device.Platform)
	if err != nil || pack == nil {
		return []RenderedSource{{
			Source: "service:" + svc.ServiceID, Kind: "service",
			Platform: NormalizePlatform(device.Platform),
			Error:    "no platform pack for " + svc.ServiceType + "/" + device.Platform,
		}}
	}
	var out []RenderedSource
	cleanupDone := false
	for i := range eps {
		ep := &eps[i]
		iface, err := loadInterface(db, ep.InterfaceID)
		if err != nil {
			out = append(out, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
			continue
		}
		data, err := GenericData(db, svc, ep, device, iface)
		if err != nil {
			out = append(out, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
			continue
		}
		body, err := RenderPackApplyBody(db, pack, data)
		cmds := body
		if err == nil && !cleanupDone {
			cl, clErr := RenderPackCleanupIfPresent(db, pack, data)
			if clErr != nil {
				err = clErr
			} else {
				cmds = append(cl, body...)
				cleanupDone = true
			}
		}
		src := RenderedSource{
			Source: "service:" + svc.ServiceID + ":" + ep.Role + ":" + strconv.FormatUint(uint64(ep.ID), 10),
			Kind:   "service", Platform: pack.Platform, PayloadKind: pack.PayloadKind, Commands: cmds,
		}
		if err != nil {
			src.Error = err.Error()
		}
		out = append(out, src)
	}
	return out
}

// RenderDevice returns baseline templates + terminating services for a device.
// It does not talk to the device.
func RenderDevice(db *gorm.DB, deviceID uint) (*DeviceRender, error) {
	device, err := loadDevice(db, deviceID)
	if err != nil {
		return nil, statusErr(404, "device not found")
	}
	result := &DeviceRender{DeviceID: device.ID, Name: device.Name, Platform: device.Platform}

	scopeIDs, err := deviceScopeChain(db, deviceID)
	if err != nil {
		return nil, err
	}
	vars, err := ResolveMapForDevice(db, device.ID)
	if err != nil {
		return nil, err
	}
	var tmpls []models.ConfigTemplate
	if err := db.Where("enabled = ?", true).Find(&tmpls).Error; err != nil {
		return nil, err
	}
	for i := range tmpls {
		t := &tmpls[i]
		if !platformMatch(t.Platform, device.Platform) {
			continue
		}
		if t.ScopeID != nil && !scopeIDs[*t.ScopeID] {
			continue
		}
		kind := t.PayloadKind
		if kind == "" {
			kind = models.PayloadKindCLI
		}
		data := map[string]any{
			"Device": DCIMFromDevice(device),
			"Vars":   vars,
			"Name":   device.Name,
		}
		cmds, err := Render(db, t.Body, "", data)
		src := RenderedSource{
			Source: "template:" + t.Name, Kind: "template",
			Platform: t.Platform, PayloadKind: kind, Commands: cmds,
		}
		if err != nil {
			src.Error = err.Error()
		}
		result.Sources = append(result.Sources, src)
	}

	var eps []models.ServiceEndpoint
	if err := db.Where("device_id = ?", deviceID).Find(&eps).Error; err != nil {
		return nil, err
	}
	bySvc := map[uint][]models.ServiceEndpoint{}
	for _, ep := range eps {
		bySvc[ep.ServiceID] = append(bySvc[ep.ServiceID], ep)
	}
	for svcID, group := range bySvc {
		var svc models.Service
		if err := db.First(&svc, svcID).Error; err != nil {
			continue
		}
		result.Sources = append(result.Sources, renderGenericForDevice(db, &svc, device, group)...)
	}
	return result, nil
}

// RenderService renders every endpoint of a service (preview, no device I/O).
func RenderService(db *gorm.DB, serviceID uint) ([]RenderedSource, error) {
	var svc models.Service
	if err := db.First(&svc, serviceID).Error; err != nil {
		return nil, statusErr(404, "service not found")
	}
	var eps []models.ServiceEndpoint
	if err := db.Where("service_id = ?", serviceID).Find(&eps).Error; err != nil {
		return nil, err
	}
	byDev := map[uint][]models.ServiceEndpoint{}
	for _, ep := range eps {
		byDev[ep.DeviceID] = append(byDev[ep.DeviceID], ep)
	}
	var out []RenderedSource
	for devID, group := range byDev {
		dev, err := loadDevice(db, devID)
		if err != nil {
			out = append(out, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
			continue
		}
		out = append(out, renderGenericForDevice(db, &svc, dev, group)...)
	}
	return out, nil
}
