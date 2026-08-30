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

// ELINERenderData is the template context for ELINE packs. Field names match
// the existing embed templates (.Name, .LocalIface, .Remote, .SDPID, …).
type ELINERenderData struct {
	Name               string
	Description        string
	LocalIface         string
	LocalVLAN          int
	PeerLocalIface     string
	PeerLocalVLAN      int
	ServiceNumericID   int
	Remote             *ELINERemote
	StaleSubinterfaces []ELINEStale
	SDPID              int
	Vars               map[string]any
	Device             DCIMDevice
	Interface          DCIMInterface
}

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

// GenericRenderData is the template context for non-ELINE services.
type GenericRenderData struct {
	Name             string
	Description      string
	ServiceNumericID int
	Fields           map[string]any
	Endpoint         map[string]any
	Vars             map[string]any
	Device           DCIMDevice
	Interface        DCIMInterface
	LocalIface       string
	LocalVLAN        int
	Role             string
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
	v, ok := m["vlan"]
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
	if n, ok := asInt(data.Fields["service_numeric_id"]); ok {
		data.ServiceNumericID = int(n)
	}
	return data, nil
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

func elineSidesForDevice(svc *models.Service, deviceID uint) []struct {
	ifaceID uint
	vlan    int
	peer    bool
	peerID  uint
	peerIf  uint
	peerV   int
} {
	var sides []struct {
		ifaceID uint
		vlan    int
		peer    bool
		peerID  uint
		peerIf  uint
		peerV   int
	}
	if svc.EndpointADeviceID == deviceID {
		sides = append(sides, struct {
			ifaceID uint
			vlan    int
			peer    bool
			peerID  uint
			peerIf  uint
			peerV   int
		}{svc.EndpointAInterfaceID, svc.EndpointAVlan, svc.EndpointBDeviceID != 0, svc.EndpointBDeviceID, svc.EndpointBInterfaceID, svc.EndpointBVlan})
	}
	if svc.EndpointBDeviceID == deviceID && svc.EndpointBDeviceID != svc.EndpointADeviceID {
		sides = append(sides, struct {
			ifaceID uint
			vlan    int
			peer    bool
			peerID  uint
			peerIf  uint
			peerV   int
		}{svc.EndpointBInterfaceID, svc.EndpointBVlan, svc.EndpointADeviceID != 0, svc.EndpointADeviceID, svc.EndpointAInterfaceID, svc.EndpointAVlan})
	}
	return sides
}

func loopbackAddr(db *gorm.DB, device *models.Device) string {
	name := "Loopback0"
	if IsSROS(device.Platform) {
		name = "system"
	}
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

func renderELINEForDevice(db *gorm.DB, svc *models.Service, device *models.Device) []RenderedSource {
	pack, err := LookupPlatformPack(db, "ELINE", device.Platform)
	if err != nil || pack == nil {
		return []RenderedSource{{
			Source: "service:" + svc.ServiceID, Kind: "service",
			Platform: NormalizePlatform(device.Platform),
			Error:    "no ELINE platform pack for " + device.Platform,
		}}
	}
	cust := customerName(db, svc.CustomerID)
	desc := elineDescription(svc.ServiceID, cust)
	var out []RenderedSource
	for _, side := range elineSidesForDevice(svc, device.ID) {
		iface, err := loadInterface(db, side.ifaceID)
		if err != nil {
			out = append(out, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
			continue
		}
		vars, _ := ResolveMap(db, iface.ID)
		data := ELINERenderData{
			Name:             svc.ServiceID,
			Description:      desc,
			LocalIface:       iface.Name,
			LocalVLAN:        side.vlan,
			ServiceNumericID: svc.PseudowireID,
			Vars:             vars,
			Device:           DCIMFromDevice(device),
			Interface:        DCIMFromInterface(iface),
		}
		if side.peer && side.peerID == device.ID {
			peerIface, err := loadInterface(db, side.peerIf)
			if err == nil {
				data.PeerLocalIface = peerIface.Name
				data.PeerLocalVLAN = side.peerV
			}
		} else if side.peer {
			peerDev, err := loadDevice(db, side.peerID)
			if err == nil {
				peerIface, _ := loadInterface(db, side.peerIf)
				remoteIface := ""
				if peerIface != nil {
					remoteIface = peerIface.Name
				}
				data.Remote = &ELINERemote{
					NeighborIP:   loopbackAddr(db, peerDev),
					PseudowireID: svc.PseudowireID,
					MTU:          9100,
					ControlWord:  true,
					DeviceName:   peerDev.Name,
					RemoteIface:  remoteIface,
					RemoteVLAN:   side.peerV,
				}
				if IsSROS(device.Platform) && data.Remote.NeighborIP != "" {
					if id, err := SDPIDFromNeighbor(data.Remote.NeighborIP); err == nil {
						data.SDPID = id
					}
				}
			}
		}
		cmds, err := RenderPackApply(db, pack, data)
		src := RenderedSource{
			Source: "service:" + svc.ServiceID, Kind: "service",
			Platform: pack.Platform, PayloadKind: pack.PayloadKind,
			Commands: cmds,
		}
		if err != nil {
			src.Error = err.Error()
		}
		out = append(out, src)
	}
	return out
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

	var elines []models.Service
	if err := db.Where("service_type = ? AND (endpoint_a_device_id = ? OR endpoint_b_device_id = ?)",
		"ELINE", deviceID, deviceID).Find(&elines).Error; err != nil {
		return nil, err
	}
	for i := range elines {
		result.Sources = append(result.Sources, renderELINEForDevice(db, &elines[i], device)...)
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
		if svc.ServiceType == "ELINE" {
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
	if svc.ServiceType == "ELINE" {
		var out []RenderedSource
		ids := []uint{}
		if svc.EndpointADeviceID != 0 {
			ids = append(ids, svc.EndpointADeviceID)
		}
		if svc.EndpointBDeviceID != 0 && svc.EndpointBDeviceID != svc.EndpointADeviceID {
			ids = append(ids, svc.EndpointBDeviceID)
		}
		for _, id := range ids {
			dev, err := loadDevice(db, id)
			if err != nil {
				out = append(out, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
				continue
			}
			out = append(out, renderELINEForDevice(db, &svc, dev)...)
		}
		return out, nil
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
