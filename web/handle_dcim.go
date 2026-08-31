package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/abundo/factum2/internal/cfgmgmt"
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
