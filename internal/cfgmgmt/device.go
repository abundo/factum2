package cfgmgmt

import (
	"encoding/json"
	"fmt"
	"net/netip"
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
	FieldVLAN                 = "vlan"
	FieldSubinterfaceNetboxID = "subinterface_netbox_id"
	FieldTerminationNetboxID  = "termination_netbox_id"
	fieldServiceID            = "service_id"
)

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

// FieldMeta is definition metadata exposed to templates (not stored values).
type FieldMeta struct {
	Name        string
	Type        string
	Unit        string
	Description string
}

// GenericRenderData is the template context for service-translation CLI objects.
type GenericRenderData struct {
	Name             string
	Description      string
	ServiceNumericID int
	ConnectionType   string
	Fields           map[string]any
	FieldMeta        map[string]FieldMeta
	Vars             map[string]any
	Device           DCIMDevice
	Interface        DCIMInterface
	LocalIface       string
	Current          RenderEndpoint
	Interfaces       []RenderEndpoint
	Others           []RenderEndpoint
}

// RenderEndpoint is one homogeneous UNI in the template context.
type RenderEndpoint struct {
	Device     DCIMDevice
	Interface  DCIMInterface
	LocalIface string
	Fields     map[string]any
	Commercial *RenderCommercial
	// NeighborIP is the peer device loopback when Device.ID != current device.
	NeighborIP string
}

// RenderCommercial is the resolved commercial Service row for an endpoint.
type RenderCommercial struct {
	ID        uint
	ServiceID string
	Name      string
	Customer  string
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
	return genericData(db, svc, ep, device, iface, nil, false)
}

// GenericDataWithSiblings is GenericData using siblings for .Interfaces/.Others
// instead of the persisted endpoint table (rebind teardown/add).
func GenericDataWithSiblings(db *gorm.DB, svc *models.Service, ep *models.ServiceEndpoint, device *models.Device, iface *models.Interface, siblings []models.ServiceEndpoint) (*GenericRenderData, error) {
	return genericData(db, svc, ep, device, iface, siblings, true)
}

func genericData(db *gorm.DB, svc *models.Service, ep *models.ServiceEndpoint, device *models.Device, iface *models.Interface, siblings []models.ServiceEndpoint, siblingsSet bool) (*GenericRenderData, error) {
	vars := map[string]any{}
	if iface != nil {
		m, err := ResolveMap(db, iface.ID)
		if err == nil {
			vars = m
		}
	}
	svcFields := fieldsMap(svc.Fields)
	var st *models.ServiceType
	if svc.ServiceType != "" {
		found, err := LookupServiceType(db, svc.ServiceType)
		if err != nil {
			return nil, err
		}
		st = found
	}
	filled := FillEndpointDefaults(st, svcFields, fieldsMap(ep.Fields))
	if err := requiredInterfaceFields(st, filled); err != nil {
		return nil, err
	}

	data := &GenericRenderData{
		Name:           svc.ServiceID,
		Description:    svc.Comment,
		ConnectionType: connectionTypeName(db, svc.ConnectionTypeID),
		Fields:         svcFields,
		FieldMeta:      fieldMetaMap(st),
		Vars:           vars,
		Device:         DCIMFromDevice(device),
		Interface:      DCIMFromInterface(iface),
	}
	if iface != nil {
		data.LocalIface = iface.Name
	}
	if svc.PseudowireID != 0 {
		data.ServiceNumericID = svc.PseudowireID
	} else if n, ok := asInt(data.Fields["service_numeric_id"]); ok {
		data.ServiceNumericID = int(n)
	}

	if !siblingsSet {
		var err error
		siblings, err = ListEndpoints(db, svc.ID)
		if err != nil {
			return nil, err
		}
	}

	currentDeviceID := uint(0)
	if device != nil {
		currentDeviceID = device.ID
	} else {
		currentDeviceID = ep.DeviceID
	}

	commercialCache := map[uint]*RenderCommercial{}
	currentRE, err := buildRenderEndpoint(db, st, svc, ep, device, iface, filled, 0, commercialCache)
	if err != nil {
		return nil, err
	}
	data.Current = currentRE

	seenCurrent := false
	for i := range siblings {
		sib := &siblings[i]
		if sib.DeviceID == 0 || sib.InterfaceID == 0 {
			continue
		}
		if sameEndpoint(ep, sib) {
			seenCurrent = true
			data.Interfaces = append(data.Interfaces, currentRE)
			continue
		}
		re, err := buildRenderEndpoint(db, st, svc, sib, nil, nil, nil, currentDeviceID, commercialCache)
		if err != nil {
			return nil, err
		}
		data.Interfaces = append(data.Interfaces, re)
		data.Others = append(data.Others, re)
	}
	if !seenCurrent {
		data.Interfaces = append([]RenderEndpoint{currentRE}, data.Interfaces...)
	}
	return data, nil
}

// FillEndpointDefaults copies same-name service-level values into empty
// interface fields. It does not persist; stored endpoint JSON stays empty.
func FillEndpointDefaults(st *models.ServiceType, serviceFields, epFields map[string]any) map[string]any {
	out := copyFieldMap(epFields)
	if st == nil {
		return out
	}
	for _, f := range st.Interfaces.Fields {
		ev, eok := out[f.Name]
		if eok && !FieldEmpty(f, ev) {
			continue
		}
		sv, sok := serviceFields[f.Name]
		if !sok {
			continue
		}
		sf, ok := schemaFieldByName(st.Schema, f.Name)
		if !ok {
			sf = f
		}
		if FieldEmpty(sf, sv) {
			continue
		}
		out[f.Name] = sv
	}
	return out
}

func requiredInterfaceFields(st *models.ServiceType, filled map[string]any) error {
	if st == nil {
		return nil
	}
	for _, f := range st.Interfaces.Fields {
		if !f.Required {
			continue
		}
		v, ok := filled[f.Name]
		if !ok || FieldEmpty(f, v) {
			return fmt.Errorf("endpoint missing required field %q", f.Name)
		}
	}
	return nil
}

func copyFieldMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func schemaFieldByName(fields []models.FieldSchema, name string) (models.FieldSchema, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return models.FieldSchema{}, false
}

func fieldMetaMap(st *models.ServiceType) map[string]FieldMeta {
	out := map[string]FieldMeta{}
	if st == nil {
		return out
	}
	add := func(fields []models.FieldSchema) {
		for _, f := range fields {
			if f.Name == "" {
				continue
			}
			out[f.Name] = FieldMeta{
				Name: f.Name, Type: f.Type, Unit: f.Unit, Description: f.Description,
			}
		}
	}
	add(st.Schema)
	add(st.Interfaces.Fields)
	return out
}

func connectionTypeName(db *gorm.DB, id *uint) string {
	if id == nil || *id == 0 {
		return ""
	}
	var ct models.ServiceConnectionType
	if err := db.First(&ct, *id).Error; err != nil {
		return ""
	}
	return ct.Name
}

func sameEndpoint(a, b *models.ServiceEndpoint) bool {
	if a == nil || b == nil {
		return false
	}
	if a.ID != 0 && b.ID != 0 {
		return a.ID == b.ID
	}
	aa, bb := *a, *b
	if aa.ServiceID == 0 {
		aa.ServiceID = bb.ServiceID
	} else if bb.ServiceID == 0 {
		bb.ServiceID = aa.ServiceID
	}
	if aa.Role == "" {
		aa.Role = models.EndpointRoleInterface
	}
	if bb.Role == "" {
		bb.Role = models.EndpointRoleInterface
	}
	return EndpointIdentity(aa) == EndpointIdentity(bb)
}

func buildRenderEndpoint(db *gorm.DB, st *models.ServiceType, svc *models.Service, ep *models.ServiceEndpoint, device *models.Device, iface *models.Interface, filled map[string]any, currentDeviceID uint, commercialCache map[uint]*RenderCommercial) (RenderEndpoint, error) {
	if filled == nil {
		filled = FillEndpointDefaults(st, fieldsMap(svc.Fields), fieldsMap(ep.Fields))
	}
	re := RenderEndpoint{Fields: filled}
	if device == nil && ep.DeviceID != 0 {
		d, err := loadDevice(db, ep.DeviceID)
		if err != nil {
			return re, err
		}
		device = d
	}
	if iface == nil && ep.InterfaceID != 0 {
		ifc, err := loadInterface(db, ep.InterfaceID)
		if err != nil {
			return re, err
		}
		iface = ifc
	}
	re.Device = DCIMFromDevice(device)
	re.Interface = DCIMFromInterface(iface)
	if iface != nil {
		re.LocalIface = iface.Name
	}
	if currentDeviceID != 0 && device != nil && device.ID != currentDeviceID {
		re.NeighborIP = loopbackAddr(db, device)
	}
	comm, err := resolveCommercial(db, st, svc, filled, fieldsMap(svc.Fields), commercialCache)
	if err != nil {
		return re, err
	}
	re.Commercial = comm
	return re, nil
}

func hasServiceIDField(st *models.ServiceType) bool {
	if st == nil {
		return false
	}
	check := func(fields []models.FieldSchema) bool {
		for _, f := range fields {
			if f.Name == fieldServiceID || NormalizeFieldType(f.Type) == models.FieldTypeServiceID {
				return true
			}
		}
		return false
	}
	return check(st.Schema) || check(st.Interfaces.Fields)
}

func resolveCommercial(db *gorm.DB, st *models.ServiceType, svc *models.Service, epFields, svcFields map[string]any, cache map[uint]*RenderCommercial) (*RenderCommercial, error) {
	id := FieldUint(epFields, fieldServiceID)
	if id == 0 {
		id = FieldUint(svcFields, fieldServiceID)
	}
	if id == 0 {
		// Same-row: no service_id picker on the definition, so the technical
		// instance is the commercial row. A service_id field that is unset
		// means no commercial CN (two-row / not yet picked).
		if svc == nil || svc.ID == 0 || hasServiceIDField(st) {
			return nil, nil
		}
		id = svc.ID
	}
	if cache != nil {
		if c, ok := cache[id]; ok {
			return c, nil
		}
	}
	var row models.Service
	if svc != nil && svc.ID == id {
		row = *svc
	} else if err := db.First(&row, id).Error; err != nil {
		return nil, fmt.Errorf("service_id %d: %w", id, err)
	}
	c := &RenderCommercial{
		ID:        row.ID,
		ServiceID: row.ServiceID,
		Name:      row.Name,
		Customer:  customerName(db, row.CustomerID),
	}
	if cache != nil {
		cache[id] = c
	}
	return c, nil
}

func loopbackIfaceName(platform string) string {
	if IsSROS(platform) {
		return "system"
	}
	return "Loopback0"
}

// RenderedSource is one CLI object or service's rendered CLI (or other payload).
type RenderedSource struct {
	Source      string   `json:"source"`
	Kind        string   `json:"kind"` // cli | service
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

func renderGenericForDevice(db *gorm.DB, svc *models.Service, device *models.Device, eps, all []models.ServiceEndpoint) []RenderedSource {
	lookupErr := func(msg string) []RenderedSource {
		return []RenderedSource{{
			Source: "service:" + svc.ServiceID, Kind: "service",
			Platform: NormalizePlatform(device.Platform),
			Error:    msg,
		}}
	}
	cli, err := LookupCLIObject(db, svc.ServiceType, device.Platform)
	if err != nil {
		return lookupErr(err.Error())
	}
	if cli == nil {
		return lookupErr(MissingCLIObjectMessage(svc.ServiceType, device.Platform))
	}
	platform := NormalizePlatform(device.Platform)
	if cli.Platform != "" {
		platform = cli.Platform
	}
	payloadKind := models.PayloadKindCLI
	if cli.PayloadKind != "" {
		payloadKind = cli.PayloadKind
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
		data, err := genericData(db, svc, ep, device, iface, all, true)
		if err != nil {
			out = append(out, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
			continue
		}
		cmds, err := renderCLIObject(db, cli, data, !cleanupDone)
		if err == nil {
			cleanupDone = true
		}
		src := RenderedSource{
			Source: device.Name + " / " + iface.Name + " (" + ep.Role + ")",
			Kind:   "service", Platform: platform, PayloadKind: payloadKind, Commands: cmds,
		}
		if err != nil {
			src.Error = err.Error()
		}
		out = append(out, src)
	}
	return out
}

// RenderDevice returns baseline CLI + terminating services for a device.
// It does not talk to the device.
func RenderDevice(db *gorm.DB, deviceID uint) (*DeviceRender, error) {
	device, err := loadDevice(db, deviceID)
	if err != nil {
		return nil, statusErr(404, "device not found")
	}
	result := &DeviceRender{DeviceID: device.ID, Name: device.Name, Platform: device.Platform}

	vars, err := ResolveMapForDevice(db, device.ID)
	if err != nil {
		return nil, err
	}
	cliSources, err := renderBaselineCLI(db, device, vars)
	if err != nil {
		return nil, err
	}
	result.Sources = append(result.Sources, cliSources...)

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
		var all []models.ServiceEndpoint
		if err := db.Where("service_id = ?", svcID).Find(&all).Error; err != nil {
			result.Sources = append(result.Sources, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
			continue
		}
		result.Sources = append(result.Sources, renderGenericForDevice(db, &svc, device, group, all)...)
	}
	return result, nil
}

// RenderService renders every saved endpoint of a service (preview, no device I/O).
func RenderService(db *gorm.DB, serviceID uint) ([]RenderedSource, error) {
	var eps []models.ServiceEndpoint
	if err := db.Where("service_id = ?", serviceID).Find(&eps).Error; err != nil {
		return nil, err
	}
	return RenderServiceEndpoints(db, serviceID, eps, nil)
}

// RenderServiceEndpoints renders the given endpoints against a saved service
// (preview, no device I/O). Incomplete rows (no device or interface) are
// skipped. fields, when non-empty, overlays Service.Fields for the render.
func RenderServiceEndpoints(db *gorm.DB, serviceID uint, eps []models.ServiceEndpoint, fields json.RawMessage) ([]RenderedSource, error) {
	var svc models.Service
	if err := db.First(&svc, serviceID).Error; err != nil {
		return nil, statusErr(404, "service not found")
	}
	if len(fields) > 0 && string(fields) != "null" {
		svc.Fields = fields
	}
	ready := make([]models.ServiceEndpoint, 0, len(eps))
	for _, ep := range eps {
		if ep.DeviceID == 0 || ep.InterfaceID == 0 {
			continue
		}
		ready = append(ready, ep)
	}
	byDev := map[uint][]models.ServiceEndpoint{}
	for _, ep := range ready {
		byDev[ep.DeviceID] = append(byDev[ep.DeviceID], ep)
	}
	var out []RenderedSource
	for devID, group := range byDev {
		dev, err := loadDevice(db, devID)
		if err != nil {
			out = append(out, RenderedSource{Source: "service:" + svc.ServiceID, Kind: "service", Error: err.Error()})
			continue
		}
		out = append(out, renderGenericForDevice(db, &svc, dev, group, ready)...)
	}
	return out, nil
}
