package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// --------------------------------------------------------------------------
//
//	API
//
// --------------------------------------------------------------------------

// fetchDeviceList loads devices without their interfaces/addresses, for the
// device list view which doesn't render them.
func fetchDeviceList(ctx context.Context, DB *gorm.DB) ([]models.Device, error) {
	return gorm.G[models.Device](DB).Order("Name").Find(ctx)
}

// fetchDevices loads devices (optionally filtered to ids), with their
// interfaces and addresses assembled via separate flat queries rather than
// a preload join, which produced huge row-multiplying joins for devices
// with many interfaces/addresses.
func fetchDevices(ctx context.Context, DB *gorm.DB, ids []uint) ([]models.Device, error) {
	q := gorm.G[models.Device](DB).Order("Name")
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}
	items, err := q.Find(ctx)
	if err != nil {
		return nil, err
	}

	deviceIDs := make([]uint, len(items))
	for i, tmp := range items {
		deviceIDs[i] = tmp.ID
	}

	interfaces, err := gorm.G[models.Interface](DB).Where("device_id IN ?", deviceIDs).Find(ctx)
	if err != nil {
		return nil, err
	}

	interfaceIDs := make([]uint, len(interfaces))
	for i, tmp := range interfaces {
		interfaceIDs[i] = tmp.ID
	}

	addresses, err := gorm.G[models.Address](DB).Where("interface_id IN ?", interfaceIDs).Find(ctx)
	if err != nil {
		return nil, err
	}

	var opticalPorts []models.OpticalPort
	if len(interfaceIDs) > 0 {
		opticalPorts, err = gorm.G[models.OpticalPort](DB).Where("interface_id IN ?", interfaceIDs).Find(ctx)
		if err != nil {
			return nil, err
		}
	}
	portByIface := make(map[uint]models.OpticalPort, len(opticalPorts))
	for _, p := range opticalPorts {
		portByIface[p.InterfaceID] = p
	}

	addressesByInterfaceID := make(map[uint][]models.Address, len(interfaces))
	for _, addr := range addresses {
		addressesByInterfaceID[addr.InterfaceID] = append(addressesByInterfaceID[addr.InterfaceID], addr)
	}

	var epRows []models.ServiceEndpoint
	if len(interfaceIDs) > 0 {
		epRows, err = gorm.G[models.ServiceEndpoint](DB).
			Where("interface_id IN ?", interfaceIDs).
			Find(ctx)
		if err != nil {
			return nil, err
		}
	}
	svcIDs := make([]uint, 0, len(epRows))
	seenSvc := map[uint]bool{}
	for _, ep := range epRows {
		if seenSvc[ep.ServiceID] {
			continue
		}
		seenSvc[ep.ServiceID] = true
		svcIDs = append(svcIDs, ep.ServiceID)
	}
	var services []models.Service
	if len(svcIDs) > 0 {
		services, err = gorm.G[models.Service](DB).Where("id IN ?", svcIDs).Find(ctx)
		if err != nil {
			return nil, err
		}
	}

	// ServiceEndpoint.InterfaceID is the physical port. Once provisioned,
	// the real termination is the per-VLAN subinterface ("<parent>.<vlan>")
	// recorded in Fields.subinterface_netbox_id - hang the Services button
	// on that subinterface when it exists, not on the parent physical port.
	ifaceByID := make(map[uint]models.Interface, len(interfaces))
	ifaceIDByNetboxID := make(map[uint]uint, len(interfaces))
	ifaceIDByDeviceAndName := make(map[string]uint, len(interfaces))
	for _, iface := range interfaces {
		ifaceByID[iface.ID] = iface
		if iface.NetboxID != 0 {
			ifaceIDByNetboxID[iface.NetboxID] = iface.ID
		}
		ifaceIDByDeviceAndName[fmt.Sprintf("%d\x00%s", iface.DeviceID, iface.Name)] = iface.ID
	}

	resolveServiceInterfaceID := func(physicalID, subNetboxID uint, vlan int) uint {
		if physicalID == 0 {
			return 0
		}
		if subNetboxID != 0 {
			if id, ok := ifaceIDByNetboxID[subNetboxID]; ok {
				return id
			}
		}
		if vlan > 0 {
			if phys, ok := ifaceByID[physicalID]; ok {
				key := fmt.Sprintf("%d\x00%s.%d", phys.DeviceID, phys.Name, vlan)
				if id, ok := ifaceIDByDeviceAndName[key]; ok {
					return id
				}
			}
		}
		return physicalID
	}

	var hopRows []models.ServiceHop
	if len(interfaceIDs) > 0 {
		hopRows, _ = gorm.G[models.ServiceHop](DB).
			Where("kind = ? AND interface_id IN ?", models.HopInterface, interfaceIDs).
			Find(ctx)
	}
	hopServiceIDs := make([]uint, 0, len(hopRows))
	for _, h := range hopRows {
		hopServiceIDs = append(hopServiceIDs, h.ServiceID)
	}
	if len(hopServiceIDs) > 0 {
		hopSvcs, err := gorm.G[models.Service](DB).Where("id IN ?", hopServiceIDs).Find(ctx)
		if err == nil {
			services = append(services, hopSvcs...)
		}
	}

	svcByID := make(map[uint]models.Service, len(services))
	for _, svc := range services {
		svcByID[svc.ID] = svc
	}
	servicesByInterfaceID := make(map[uint][]models.InterfaceServiceRef, len(epRows))
	for _, ep := range epRows {
		svc, ok := svcByID[ep.ServiceID]
		if !ok {
			continue
		}
		sub, _ := cfgmgmt.NetboxIDsFromFields(ep.Fields)
		id := resolveServiceInterfaceID(ep.InterfaceID, sub, cfgmgmt.VLANFromFields(ep.Fields))
		if id == 0 {
			continue
		}
		ref := models.InterfaceServiceRef{ID: svc.ID, ServiceID: svc.ServiceID}
		servicesByInterfaceID[id] = append(servicesByInterfaceID[id], ref)
	}
	for _, h := range hopRows {
		if h.InterfaceID == nil {
			continue
		}
		svc, ok := svcByID[h.ServiceID]
		if !ok {
			continue
		}
		ref := models.InterfaceServiceRef{ID: svc.ID, ServiceID: svc.ServiceID}
		servicesByInterfaceID[*h.InterfaceID] = append(servicesByInterfaceID[*h.InterfaceID], ref)
	}

	interfacesByDeviceID := make(map[uint][]models.Interface, len(items))
	for _, iface := range interfaces {
		iface.Addresses = addressesByInterfaceID[iface.ID]
		iface.Services = servicesByInterfaceID[iface.ID]
		if p, ok := portByIface[iface.ID]; ok {
			cp := p
			iface.Optical = &cp
		}
		interfacesByDeviceID[iface.DeviceID] = append(interfacesByDeviceID[iface.DeviceID], iface)
	}

	for i := range items {
		items[i].Interfaces = interfacesByDeviceID[items[i].ID]
	}
	return items, nil
}

// Fetch all devices. Default is the shallow list (no nested
// interfaces/addresses) used by the device-list UI. ?include=interfaces
// loads the full snapshot instead - DNS sync (internal/factum.GetDevicesWithInterfaces)
// needs per-interface addresses and cannot use the shallow list.
func (ctrl *Controller) ApiGetDevices(c *echo.Context) error {
	ctx := c.Request().Context()
	var (
		items []models.Device
		err   error
	)
	if c.QueryParam("include") == "interfaces" {
		items, err = fetchDevices(ctx, ctrl.DB, nil)
	} else {
		items, err = fetchDeviceList(ctx, ctrl.DB)
	}
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"message": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

// ConnectionListDTO is a cable for typeahead pickers (maintenance fibers).
type ConnectionListDTO struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

func connectionDisplayName(deviceA, ifaceA, deviceB, ifaceB, label string) string {
	left := strings.TrimSpace(deviceA + " " + ifaceA)
	right := strings.TrimSpace(deviceB + " " + ifaceB)
	name := left + " ↔ " + right
	if label != "" {
		name += " (" + label + ")"
	}
	return name
}

func (ctrl *Controller) ApiGetConnections(c *echo.Context) error {
	ctx := c.Request().Context()
	conns, err := gorm.G[models.Connection](ctrl.DB).Find(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if len(conns) == 0 {
		return c.JSON(http.StatusOK, []ConnectionListDTO{})
	}
	deviceIDs := make([]uint, 0, len(conns)*2)
	ifaceIDs := make([]uint, 0, len(conns)*2)
	for _, conn := range conns {
		deviceIDs = append(deviceIDs, conn.DeviceAID, conn.DeviceBID)
		ifaceIDs = append(ifaceIDs, conn.InterfaceAID, conn.InterfaceBID)
	}
	devices, err := gorm.G[models.Device](ctrl.DB).Where("id IN ?", deviceIDs).Find(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	ifaces, err := gorm.G[models.Interface](ctrl.DB).Where("id IN ?", ifaceIDs).Find(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	deviceName := make(map[uint]string, len(devices))
	for _, d := range devices {
		deviceName[d.ID] = d.Name
	}
	ifaceName := make(map[uint]string, len(ifaces))
	for _, i := range ifaces {
		ifaceName[i.ID] = i.Name
	}
	out := make([]ConnectionListDTO, 0, len(conns))
	for _, conn := range conns {
		out = append(out, ConnectionListDTO{
			ID: conn.ID,
			Name: connectionDisplayName(
				deviceName[conn.DeviceAID], ifaceName[conn.InterfaceAID],
				deviceName[conn.DeviceBID], ifaceName[conn.InterfaceBID],
				conn.Label,
			),
		})
	}
	return c.JSON(http.StatusOK, out)
}

// ApiGetDeviceByName returns the device with a given name as a one-element
// array (empty if there's no such device), not as a bare object - the same
// shape as /api/device, so internal/factum.FactumClient parses both with the
// same []*models.Device. Used by tools that only know a device by name and
// have no access to the primary's Postgres (currently factum2-driver-cli, via
// internal/drivers.NewDriverName).
func (ctrl *Controller) ApiGetDeviceByName(c *echo.Context) error {
	ctx := c.Request().Context()
	name := c.Param("name")

	matches, err := gorm.G[models.Device](ctrl.DB).Where("name = ?", name).Find(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if len(matches) == 0 {
		return c.JSON(http.StatusOK, []models.Device{})
	}

	ids := make([]uint, len(matches))
	for i, device := range matches {
		ids[i] = device.ID
	}
	items, err := fetchDevices(ctx, ctrl.DB, ids)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiGetDeviceByID(c *echo.Context) error {
	// data := ctrl.GetUser(c)
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	items, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	if len(items) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"message": "item not found"})
	}
	return c.JSON(http.StatusOK, items[0])
}

func (ctrl *Controller) ApiDeviceImpact(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	out, err := optical.DeviceDownImpact(ctrl.DB, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, out)
}

func isLocalDevice(d models.Device) bool {
	return d.NetboxID == 0 && d.CfSource != "netbox"
}

func catalogSourceNetbox(source string) bool {
	return source == "netbox"
}

func applyOptionalBool(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

func applyDeviceWrite(db *gorm.DB, device *models.Device, dto models.DeviceCreateDTO) error {
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if dto.DeviceTypeID == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "device_type_id is required")
	}
	var dt models.DeviceType
	if err := db.First(&dt, dto.DeviceTypeID).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "device type not found")
	}
	var mfr models.Manufacturer
	if err := db.First(&mfr, dt.ManufacturerID).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "manufacturer not found")
	}
	status := strings.TrimSpace(dto.Status)
	if status == "" {
		status = "active"
	}
	kind := strings.ToLower(strings.TrimSpace(dto.OpticalKind))
	if kind != "" {
		if alias, ok := models.OpticalKindAliases[kind]; ok {
			kind = alias
		}
		if !models.IsOpticalKind(kind) {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid optical_kind")
		}
	}
	device.Name = name
	device.Comments = strings.TrimSpace(dto.Comments)
	if dto.Enabled != nil {
		device.Enabled = *dto.Enabled
	} else if device.ID == 0 {
		device.Enabled = true
	}
	device.Manufacturer = mfr.Name
	device.ModelName = dt.Model
	device.DeviceTypeID = dt.ID
	if err := applyDeviceSite(db, device, dto); err != nil {
		return err
	}
	device.Role = strings.TrimSpace(dto.Role)
	device.Status = status
	device.CfLocation = strings.TrimSpace(dto.CfLocation)
	applyOptionalBool(&device.CfMonitorIcinga, dto.CfMonitorIcinga)
	applyOptionalBool(&device.CfMonitorLibrenms, dto.CfMonitorLibrenms)
	applyOptionalBool(&device.CfMonitorGrafana, dto.CfMonitorGrafana)
	applyOptionalBool(&device.CfBackupOxidized, dto.CfBackupOxidized)
	applyOptionalBool(&device.CfAlarmInterfaces, dto.CfAlarmInterfaces)
	device.OpticalKind = kind
	platformID := dto.PlatformID
	if platformID == 0 {
		platformID = dt.PlatformID
	}
	device.PlatformID = 0
	device.Platform = ""
	if platformID != 0 {
		var plat models.Platform
		if err := db.First(&plat, platformID).Error; err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "platform not found")
		}
		device.PlatformID = plat.ID
		device.Platform = plat.Slug
	}
	return nil
}

func applyDeviceSite(db *gorm.DB, device *models.Device, dto models.DeviceCreateDTO) error {
	if dto.SiteID != 0 {
		var site models.Site
		if err := db.First(&site, dto.SiteID).Error; err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "site not found")
		}
		device.SiteID = site.ID
		device.Site = site.Name
		return nil
	}
	name := strings.TrimSpace(dto.Site)
	if name == "" {
		device.SiteID = 0
		device.Site = ""
		return nil
	}
	var site models.Site
	if err := db.Where("LOWER(name) = LOWER(?)", name).First(&site).Error; err == nil {
		device.SiteID = site.ID
		device.Site = site.Name
		return nil
	}
	device.SiteID = 0
	device.Site = name
	return nil
}

func httpErrorJSON(c *echo.Context, err error) error {
	var he *echo.HTTPError
	if errors.As(err, &he) {
		msg := he.Message
		if msg == "" {
			msg = he.Error()
		}
		return c.JSON(he.Code, map[string]any{"error": msg})
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

// ApiDeviceCreate inserts a Factum-local device (NetboxID=0, CfSource=factum).
// Manufacturer/model/platform names are copied from catalog rows so the
// existing denormalized Device columns stay populated for list/detail views.
func (ctrl *Controller) ApiDeviceCreate(c *echo.Context) error {
	var dto models.DeviceCreateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	device := models.Device{CfSource: "factum"}
	if err := applyDeviceWrite(ctrl.DB, &device, dto); err != nil {
		return httpErrorJSON(c, err)
	}
	if err := ctrl.DB.Create(&device).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := instantiateDeviceTypeInterfaces(ctrl.DB, device.ID, device.DeviceTypeID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, device)
}

// ApiDeviceUpdate edits a Factum-local device. NetBox-synced rows are
// rejected — the next sync would overwrite them.
func (ctrl *Controller) ApiDeviceUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var existing models.Device
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if !isLocalDevice(existing) {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "devices synced from NetBox cannot be edited here"})
	}
	var dto models.DeviceCreateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := applyDeviceWrite(ctrl.DB, &existing, dto); err != nil {
		return httpErrorJSON(c, err)
	}
	if err := ctrl.DB.Save(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := instantiateDeviceTypeInterfaces(ctrl.DB, existing.ID, existing.DeviceTypeID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, existing)
}

// ApiDeviceDelete removes a Factum-local device. NetBox-synced rows are
// rejected — they would come back on the next sync.
func (ctrl *Controller) ApiDeviceDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var existing models.Device
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if !isLocalDevice(existing) {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "devices synced from NetBox cannot be deleted here"})
	}
	var ifaces []models.Interface
	if err := ctrl.DB.Where("device_id = ?", existing.ID).Find(&ifaces).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if len(ifaces) > 0 {
		ifaceIDs := make([]uint, len(ifaces))
		for i, iface := range ifaces {
			ifaceIDs[i] = iface.ID
		}
		if err := ctrl.DB.Where("interface_id IN ?", ifaceIDs).Delete(&models.Address{}).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}
	if err := ctrl.DB.Where("device_id = ?", existing.ID).Delete(&models.Interface{}).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Delete(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) guardFactumCatalog(kind string, next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := echo.PathParam[uint](c, "id")
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		var source string
		var loadErr error
		switch kind {
		case "manufacturer":
			var row models.Manufacturer
			loadErr = ctrl.DB.Select("source").First(&row, id).Error
			source = row.Source
		case "device type":
			var row models.DeviceType
			loadErr = ctrl.DB.Select("source").First(&row, id).Error
			source = row.Source
		case "platform":
			var row models.Platform
			loadErr = ctrl.DB.Select("source").First(&row, id).Error
			source = row.Source
		default:
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "unknown catalog kind"})
		}
		if loadErr != nil {
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
			}
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": loadErr.Error()})
		}
		if catalogSourceNetbox(source) {
			return c.JSON(http.StatusForbidden, map[string]any{
				"error": kind + "s synced from NetBox cannot be edited here",
			})
		}
		return next(c)
	}
}

func instantiateDeviceTypeInterfaces(db *gorm.DB, deviceID, deviceTypeID uint) error {
	if deviceID == 0 || deviceTypeID == 0 {
		return nil
	}
	var tmpls []models.InterfaceTemplate
	if err := db.Where("device_type_id = ?", deviceTypeID).Find(&tmpls).Error; err != nil {
		return err
	}
	for _, tmpl := range tmpls {
		if err := ensureDeviceInterfaceFromTemplate(db, deviceID, tmpl); err != nil {
			return err
		}
	}
	return nil
}

func ensureDeviceInterfaceFromTemplate(db *gorm.DB, deviceID uint, tmpl models.InterfaceTemplate) error {
	var n int64
	if err := db.Model(&models.Interface{}).Where("device_id = ? AND name = ?", deviceID, tmpl.Name).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	typ := strings.TrimSpace(tmpl.Type)
	if typ == "" {
		typ = "other"
	}
	iface := models.Interface{
		DeviceID:    deviceID,
		Name:        tmpl.Name,
		Type:        typ,
		Label:       tmpl.Label,
		Description: tmpl.Description,
		Enabled:     true,
	}
	return db.Create(&iface).Error
}

func applyInterfaceTemplateToLocalDevices(db *gorm.DB, tmpl models.InterfaceTemplate) error {
	var devices []models.Device
	if err := db.Where("device_type_id = ?", tmpl.DeviceTypeID).Find(&devices).Error; err != nil {
		return err
	}
	for _, d := range devices {
		if !isLocalDevice(d) {
			continue
		}
		if err := ensureDeviceInterfaceFromTemplate(db, d.ID, tmpl); err != nil {
			return err
		}
	}
	return nil
}

type DCIMInterfaceDTO struct {
	ID          uint   `json:"id"`
	DeviceID    uint   `json:"device_id"`
	DeviceName  string `json:"device_name"`
	Site        string `json:"site"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	VRF         string `json:"vrf"`
	NetboxID    uint   `json:"netbox_id"`
	Source      string `json:"source"`
}

func interfaceSource(netboxID uint) string {
	if netboxID != 0 {
		return "netbox"
	}
	return "factum"
}

type DCIMInterfaceListDTO struct {
	Items  []DCIMInterfaceDTO `json:"items"`
	Total  int64              `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

func parseListLimitOffset(c *echo.Context, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	if v := strings.TrimSpace(c.QueryParam("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := strings.TrimSpace(c.QueryParam("offset")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

func likeContains(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return "%" + q + "%"
}

func (ctrl *Controller) ApiGetDCIMInterfaces(c *echo.Context) error {
	limit, offset := parseListLimitOffset(c, 100, 500)
	q := strings.TrimSpace(c.QueryParam("q"))
	sort := strings.TrimSpace(c.QueryParam("sort"))
	desc := c.QueryParam("desc") == "true" || c.QueryParam("desc") == "1"

	dbq := ctrl.DB.Model(&models.Interface{}).
		Joins("JOIN devices ON devices.id = interfaces.device_id")
	if q != "" {
		pat := likeContains(q)
		dbq = dbq.Where(
			"LOWER(interfaces.name) LIKE LOWER(?) ESCAPE '\\' OR LOWER(interfaces.description) LIKE LOWER(?) ESCAPE '\\' OR LOWER(interfaces.type) LIKE LOWER(?) ESCAPE '\\' OR LOWER(devices.name) LIKE LOWER(?) ESCAPE '\\'",
			pat, pat, pat, pat,
		)
	}

	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	orderCol := "devices.name, interfaces.name"
	switch sort {
	case "name":
		orderCol = "interfaces.name"
	case "type":
		orderCol = "interfaces.type"
	case "description":
		orderCol = "interfaces.description"
	case "enabled":
		orderCol = "interfaces.enabled"
	case "source":
		orderCol = "(interfaces.netbox_id <> 0)"
	case "device_name":
		orderCol = "devices.name, interfaces.name"
	}
	if desc {
		if sort == "device_name" || sort == "" {
			orderCol = "devices.name DESC, interfaces.name DESC"
		} else {
			orderCol += " DESC"
		}
	}

	var ifaces []models.Interface
	if err := dbq.Select("interfaces.*").Order(orderCol).Limit(limit).Offset(offset).Find(&ifaces).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	out := DCIMInterfaceListDTO{Items: []DCIMInterfaceDTO{}, Total: total, Limit: limit, Offset: offset}
	if len(ifaces) == 0 {
		return c.JSON(http.StatusOK, out)
	}
	deviceIDs := make([]uint, 0, len(ifaces))
	seen := map[uint]bool{}
	for _, iface := range ifaces {
		if seen[iface.DeviceID] {
			continue
		}
		seen[iface.DeviceID] = true
		deviceIDs = append(deviceIDs, iface.DeviceID)
	}
	var devices []models.Device
	if err := ctrl.DB.Where("id IN ?", deviceIDs).Find(&devices).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	devByID := make(map[uint]models.Device, len(devices))
	for _, d := range devices {
		devByID[d.ID] = d
	}
	out.Items = make([]DCIMInterfaceDTO, 0, len(ifaces))
	for _, iface := range ifaces {
		d := devByID[iface.DeviceID]
		out.Items = append(out.Items, DCIMInterfaceDTO{
			ID:          iface.ID,
			DeviceID:    iface.DeviceID,
			DeviceName:  d.Name,
			Site:        d.Site,
			Name:        iface.Name,
			Type:        iface.Type,
			Label:       iface.Label,
			Description: iface.Description,
			Enabled:     iface.Enabled,
			VRF:         iface.VRF,
			NetboxID:    iface.NetboxID,
			Source:      interfaceSource(iface.NetboxID),
		})
	}
	return c.JSON(http.StatusOK, out)
}

func applyLocalInterfaceWrite(db *gorm.DB, iface *models.Interface, dto models.InterfaceCreateDTO, creating bool) error {
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	q := db.Model(&models.Interface{}).Where("device_id = ? AND name = ?", iface.DeviceID, name)
	if !creating {
		q = q.Where("id <> ?", iface.ID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "interface name already exists on this device")
	}
	typ := strings.TrimSpace(dto.Type)
	if typ == "" {
		typ = "other"
	}
	iface.Name = name
	iface.Type = typ
	iface.Label = strings.TrimSpace(dto.Label)
	iface.Description = strings.TrimSpace(dto.Description)
	iface.VRF = strings.TrimSpace(dto.VRF)
	if dto.Enabled != nil {
		iface.Enabled = *dto.Enabled
	} else if creating {
		iface.Enabled = true
	}
	return nil
}

func (ctrl *Controller) ApiCreateDCIMInterface(c *echo.Context) error {
	var dto models.InterfaceCreateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.DeviceID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "device_id is required"})
	}
	var device models.Device
	if err := ctrl.DB.First(&device, dto.DeviceID).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "device not found"})
	}
	if !isLocalDevice(device) {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "interfaces on NetBox-synced devices cannot be created here"})
	}
	iface := models.Interface{DeviceID: device.ID}
	if err := applyLocalInterfaceWrite(ctrl.DB, &iface, dto, true); err != nil {
		return httpErrorJSON(c, err)
	}
	if err := ctrl.DB.Create(&iface).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, iface)
}

func (ctrl *Controller) ApiUpdateDCIMInterface(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var iface models.Interface
	if err := ctrl.DB.First(&iface, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if iface.NetboxID != 0 {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "interfaces synced from NetBox cannot be edited here"})
	}
	var device models.Device
	if err := ctrl.DB.First(&device, iface.DeviceID).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	if !isLocalDevice(device) {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "interfaces on NetBox-synced devices cannot be edited here"})
	}
	var dto models.InterfaceCreateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := applyLocalInterfaceWrite(ctrl.DB, &iface, dto, false); err != nil {
		return httpErrorJSON(c, err)
	}
	if err := ctrl.DB.Save(&iface).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, iface)
}

func (ctrl *Controller) ApiDeleteDCIMInterface(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var iface models.Interface
	if err := ctrl.DB.First(&iface, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if iface.NetboxID != 0 {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "interfaces synced from NetBox cannot be deleted here"})
	}
	var device models.Device
	if err := ctrl.DB.First(&device, iface.DeviceID).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	if !isLocalDevice(device) {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "interfaces on NetBox-synced devices cannot be deleted here"})
	}
	if err := ctrl.DB.Where("interface_id = ?", iface.ID).Delete(&models.Address{}).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Delete(&iface).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

type DCIMAddressDTO struct {
	ID            uint   `json:"id"`
	Address       string `json:"address"`
	DNSName       string `json:"dns_name"`
	VRF           string `json:"vrf"`
	Role          string `json:"role"`
	PrefixID      *uint  `json:"prefix_id"`
	Prefix        string `json:"prefix"`
	InterfaceID   uint   `json:"interface_id"`
	InterfaceName string `json:"interface_name"`
	DeviceID      uint   `json:"device_id"`
	DeviceName    string `json:"device_name"`
	NetboxID      uint   `json:"netbox_id"`
	Source        string `json:"source"`
}

type DCIMAddressListDTO struct {
	Items  []DCIMAddressDTO `json:"items"`
	Total  int64            `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

func (ctrl *Controller) ApiGetDCIMAddresses(c *echo.Context) error {
	limit, offset := parseListLimitOffset(c, 100, 500)
	q := strings.TrimSpace(c.QueryParam("q"))
	sort := strings.TrimSpace(c.QueryParam("sort"))
	desc := c.QueryParam("desc") == "true" || c.QueryParam("desc") == "1"

	dbq := ctrl.DB.Model(&models.Address{}).
		Joins("JOIN interfaces ON interfaces.id = addresses.interface_id").
		Joins("JOIN devices ON devices.id = interfaces.device_id").
		Joins("LEFT JOIN ipam_prefixes ON ipam_prefixes.id = addresses.prefix_id").
		Joins("LEFT JOIN ipam_vrfs ON ipam_vrfs.id = ipam_prefixes.vrf_id")
	if q != "" {
		pat := likeContains(q)
		dbq = dbq.Where(
			"LOWER(addresses.address) LIKE LOWER(?) ESCAPE '\\' OR LOWER(addresses.dns_name) LIKE LOWER(?) ESCAPE '\\' OR LOWER(COALESCE(ipam_vrfs.name, addresses.vrf)) LIKE LOWER(?) ESCAPE '\\' OR LOWER(ipam_prefixes.prefix) LIKE LOWER(?) ESCAPE '\\' OR LOWER(interfaces.name) LIKE LOWER(?) ESCAPE '\\' OR LOWER(devices.name) LIKE LOWER(?) ESCAPE '\\'",
			pat, pat, pat, pat, pat, pat,
		)
	}

	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	orderCol := "devices.name, interfaces.name, addresses.address"
	switch sort {
	case "address":
		orderCol = "addresses.address"
	case "dns_name":
		orderCol = "addresses.dns_name"
	case "vrf":
		orderCol = "COALESCE(ipam_vrfs.name, addresses.vrf)"
	case "prefix":
		orderCol = "ipam_prefixes.prefix"
	case "role":
		orderCol = "addresses.role"
	case "interface_name":
		orderCol = "interfaces.name"
	case "source":
		orderCol = "(addresses.netbox_id <> 0)"
	case "device_name":
		orderCol = "devices.name, interfaces.name, addresses.address"
	}
	if desc {
		if sort == "device_name" || sort == "" {
			orderCol = "devices.name DESC, interfaces.name DESC, addresses.address DESC"
		} else {
			orderCol += " DESC"
		}
	}

	var addrs []models.Address
	if err := dbq.Select("addresses.*").Order(orderCol).Limit(limit).Offset(offset).Find(&addrs).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	out := DCIMAddressListDTO{Items: []DCIMAddressDTO{}, Total: total, Limit: limit, Offset: offset}
	if len(addrs) == 0 {
		return c.JSON(http.StatusOK, out)
	}
	ifaceIDs := make([]uint, 0, len(addrs))
	seenIface := map[uint]bool{}
	for _, a := range addrs {
		if seenIface[a.InterfaceID] {
			continue
		}
		seenIface[a.InterfaceID] = true
		ifaceIDs = append(ifaceIDs, a.InterfaceID)
	}
	var ifaces []models.Interface
	if err := ctrl.DB.Where("id IN ?", ifaceIDs).Find(&ifaces).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	ifaceByID := make(map[uint]models.Interface, len(ifaces))
	deviceIDs := make([]uint, 0, len(ifaces))
	seenDev := map[uint]bool{}
	for _, iface := range ifaces {
		ifaceByID[iface.ID] = iface
		if seenDev[iface.DeviceID] {
			continue
		}
		seenDev[iface.DeviceID] = true
		deviceIDs = append(deviceIDs, iface.DeviceID)
	}
	var devices []models.Device
	if err := ctrl.DB.Where("id IN ?", deviceIDs).Find(&devices).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	devByID := make(map[uint]models.Device, len(devices))
	for _, d := range devices {
		devByID[d.ID] = d
	}
	prefixIDs := make([]uint, 0)
	seenPfx := map[uint]bool{}
	for _, a := range addrs {
		if a.PrefixID == nil || *a.PrefixID == 0 || seenPfx[*a.PrefixID] {
			continue
		}
		seenPfx[*a.PrefixID] = true
		prefixIDs = append(prefixIDs, *a.PrefixID)
	}
	pfxByID := map[uint]models.IpamPrefix{}
	vrfByID := map[uint]models.IpamVRF{}
	if len(prefixIDs) > 0 {
		var pfxs []models.IpamPrefix
		if err := ctrl.DB.Where("id IN ?", prefixIDs).Find(&pfxs).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		vrfIDs := make([]uint, 0)
		seenV := map[uint]bool{}
		for _, p := range pfxs {
			pfxByID[p.ID] = p
			if seenV[p.VRFID] {
				continue
			}
			seenV[p.VRFID] = true
			vrfIDs = append(vrfIDs, p.VRFID)
		}
		if len(vrfIDs) > 0 {
			var vrfs []models.IpamVRF
			if err := ctrl.DB.Where("id IN ?", vrfIDs).Find(&vrfs).Error; err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
			}
			for _, v := range vrfs {
				vrfByID[v.ID] = v
			}
		}
	}
	out.Items = make([]DCIMAddressDTO, 0, len(addrs))
	for _, a := range addrs {
		iface := ifaceByID[a.InterfaceID]
		d := devByID[iface.DeviceID]
		dto := DCIMAddressDTO{
			ID:            a.ID,
			Address:       a.Address,
			DNSName:       a.DNSName,
			VRF:           a.VRF,
			Role:          a.Role,
			PrefixID:      a.PrefixID,
			InterfaceID:   a.InterfaceID,
			InterfaceName: iface.Name,
			DeviceID:      iface.DeviceID,
			DeviceName:    d.Name,
			NetboxID:      a.NetboxID,
			Source:        interfaceSource(a.NetboxID),
		}
		if a.PrefixID != nil {
			if p, ok := pfxByID[*a.PrefixID]; ok {
				dto.Prefix = p.Prefix
				if v, ok := vrfByID[p.VRFID]; ok {
					dto.VRF = v.Name
				}
			}
		}
		out.Items = append(out.Items, dto)
	}
	return c.JSON(http.StatusOK, out)
}

func applyAddressWrite(db *gorm.DB, addr *models.Address, dto models.AddressCreateDTO, creating bool) error {
	if dto.InterfaceID == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "interface_id is required")
	}
	var iface models.Interface
	if err := db.First(&iface, dto.InterfaceID).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "interface not found")
	}
	raw := strings.TrimSpace(dto.Address)
	if raw == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "address is required")
	}
	pfx, err := netip.ParsePrefix(raw)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "address must be a CIDR prefix (e.g. 10.0.0.1/24)")
	}
	canon := pfx.String()
	q := db.Model(&models.Address{}).Where("interface_id = ? AND address = ?", dto.InterfaceID, canon)
	if !creating {
		q = q.Where("id <> ?", addr.ID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "address already exists on this interface")
	}
	addr.InterfaceID = dto.InterfaceID
	addr.Address = canon
	addr.DNSName = strings.TrimSpace(dto.DNSName)
	addr.VRF = strings.TrimSpace(dto.VRF)
	addr.Role = strings.TrimSpace(dto.Role)
	if err := ipam.ResolveAddressPrefix(db, addr, pfx, addr.VRF, dto.PrefixID); err != nil {
		var se *ipam.StatusError
		if errors.As(err, &se) {
			return echo.NewHTTPError(se.Status, se.Message)
		}
		return err
	}
	return nil
}

func (ctrl *Controller) ApiCreateDCIMAddress(c *echo.Context) error {
	var dto models.AddressCreateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var addr models.Address
	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := applyAddressWrite(tx, &addr, dto, true); err != nil {
			return err
		}
		if err := tx.Create(&addr).Error; err != nil {
			return err
		}
		return syncDevicePrimaryFromAddress(tx, &addr, dto.Management)
	}); err != nil {
		return httpErrorJSON(c, err)
	}
	return c.JSON(http.StatusCreated, addr)
}

func (ctrl *Controller) ApiUpdateDCIMAddress(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var addr models.Address
	if err := ctrl.DB.First(&addr, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if addr.NetboxID != 0 {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "addresses synced from NetBox cannot be edited here"})
	}
	var dto models.AddressCreateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := applyAddressWrite(tx, &addr, dto, false); err != nil {
			return err
		}
		if err := tx.Save(&addr).Error; err != nil {
			return err
		}
		return syncDevicePrimaryFromAddress(tx, &addr, dto.Management)
	}); err != nil {
		return httpErrorJSON(c, err)
	}
	return c.JSON(http.StatusOK, addr)
}

func (ctrl *Controller) ApiDeleteDCIMAddress(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var addr models.Address
	if err := ctrl.DB.First(&addr, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if addr.NetboxID != 0 {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "addresses synced from NetBox cannot be deleted here"})
	}
	clear := false
	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&addr).Error; err != nil {
			return err
		}
		return syncDevicePrimaryFromAddress(tx, &addr, &clear)
	}); err != nil {
		return httpErrorJSON(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func addressFamilyIsIPv4(addr *models.Address) (bool, error) {
	pfx, err := netip.ParsePrefix(addr.Address)
	if err != nil {
		return false, echo.NewHTTPError(http.StatusBadRequest, "address must be a CIDR prefix (e.g. 10.0.0.1/24)")
	}
	return pfx.Addr().Is4(), nil
}

func devicePrimaryRefersTo(device *models.Device, addr *models.Address, v4 bool) bool {
	id := device.PrimaryIPv4ID
	text := device.PrimaryIPv4
	if !v4 {
		id = device.PrimaryIPv6ID
		text = device.PrimaryIPv6
	}
	if id != 0 {
		if addr.ID != 0 && id == addr.ID {
			return true
		}
		if addr.NetboxID != 0 && id == addr.NetboxID {
			return true
		}
		return false
	}
	return text != "" && text == addr.Address
}

func setDevicePrimary(device *models.Device, addr *models.Address, v4 bool) {
	if v4 {
		device.PrimaryIPv4ID = addr.ID
		device.PrimaryIPv4 = addr.Address
		return
	}
	device.PrimaryIPv6ID = addr.ID
	device.PrimaryIPv6 = addr.Address
}

func clearDevicePrimary(device *models.Device, v4 bool) {
	if v4 {
		device.PrimaryIPv4ID = 0
		device.PrimaryIPv4 = ""
		return
	}
	device.PrimaryIPv6ID = 0
	device.PrimaryIPv6 = ""
}

func saveDevicePrimary(db *gorm.DB, device *models.Device) error {
	return db.Model(&models.Device{}).Where("id = ?", device.ID).Updates(map[string]any{
		"primary_ipv4":    device.PrimaryIPv4,
		"primary_ipv4_id": device.PrimaryIPv4ID,
		"primary_ipv6":    device.PrimaryIPv6,
		"primary_ipv6_id": device.PrimaryIPv6ID,
	}).Error
}

// syncDevicePrimaryFromAddress assigns or clears the device's primary IPv4
// or IPv6 from an interface address. management true sets this address as
// the primary for its family; false clears it if it currently is; nil
// leaves the assignment but keeps the denormalized address string in sync
// when this row is already the primary.
func syncDevicePrimaryFromAddress(db *gorm.DB, addr *models.Address, management *bool) error {
	var iface models.Interface
	if err := db.First(&iface, addr.InterfaceID).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "interface not found")
	}
	var device models.Device
	if err := db.First(&device, iface.DeviceID).Error; err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "device not found")
	}
	v4, err := addressFamilyIsIPv4(addr)
	if err != nil {
		return err
	}
	wasV4 := devicePrimaryRefersTo(&device, addr, true)
	wasV6 := devicePrimaryRefersTo(&device, addr, false)
	want := management
	if want == nil {
		if !wasV4 && !wasV6 {
			return nil
		}
		t := true
		want = &t
	}
	if *want {
		setDevicePrimary(&device, addr, v4)
		if v4 && wasV6 {
			clearDevicePrimary(&device, false)
		}
		if !v4 && wasV4 {
			clearDevicePrimary(&device, true)
		}
	} else {
		if wasV4 {
			clearDevicePrimary(&device, true)
		}
		if wasV6 {
			clearDevicePrimary(&device, false)
		}
	}
	return saveDevicePrimary(db, &device)
}

func (ctrl *Controller) ApiGetInterfaceTemplates(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dt models.DeviceType
	if err := ctrl.DB.First(&dt, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var rows []models.InterfaceTemplate
	if err := ctrl.DB.Where("device_type_id = ?", id).Order("name").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if rows == nil {
		rows = []models.InterfaceTemplate{}
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiCreateInterfaceTemplate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dt models.DeviceType
	if err := ctrl.DB.First(&dt, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.InterfaceTemplateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	typ := strings.TrimSpace(dto.Type)
	if typ == "" {
		typ = "other"
	}
	row := models.InterfaceTemplate{
		DeviceTypeID: dt.ID,
		Name:         name,
		Type:         typ,
		Label:        strings.TrimSpace(dto.Label),
		Description:  strings.TrimSpace(dto.Description),
		Source:       "factum",
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := applyInterfaceTemplateToLocalDevices(ctrl.DB, row); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiUpdateInterfaceTemplate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var row models.InterfaceTemplate
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if catalogSourceNetbox(row.Source) {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "interface templates synced from NetBox cannot be edited here"})
	}
	var dto models.InterfaceTemplateDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	typ := strings.TrimSpace(dto.Type)
	if typ == "" {
		typ = "other"
	}
	row.Name = name
	row.Type = typ
	row.Label = strings.TrimSpace(dto.Label)
	row.Description = strings.TrimSpace(dto.Description)
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDeleteInterfaceTemplate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var row models.InterfaceTemplate
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if catalogSourceNetbox(row.Source) {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "interface templates synced from NetBox cannot be deleted here"})
	}
	if err := ctrl.DB.Delete(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
