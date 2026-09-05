package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// TopologyDeviceDTO is one device positioned on the network map. Only
// devices with resolved coordinates (see models.Device.Latitude/Longitude)
// are ever included - the map has nowhere to place the rest.
type TopologyDeviceDTO struct {
	ID          uint    `json:"id"`
	Name        string  `json:"name"`
	Site        string  `json:"site"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	OpticalKind string  `json:"optical_kind"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// TopologyEdgeDTO is one Connection (Netbox cable) between two devices
// already present in the same response's Devices list.
type TopologyEdgeDTO struct {
	ID         uint   `json:"id"`
	DeviceAID  uint   `json:"device_a_id"`
	InterfaceA string `json:"interface_a"`
	DeviceBID  uint   `json:"device_b_id"`
	InterfaceB string `json:"interface_b"`
	Label      string `json:"label"`
}

// TopologySiteDTO is a Netbox site plotted on the map independently of any
// device - see models.Site, synced by internal/netbox.syncSites - so a
// site with no devices of its own still shows up.
type TopologySiteDTO struct {
	ID        uint    `json:"id"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type TopologyDTO struct {
	Devices []TopologyDeviceDTO `json:"devices"`
	Edges   []TopologyEdgeDTO   `json:"edges"`
	Sites   []TopologySiteDTO   `json:"sites"`
}

// TopologyDeviceListDTO is one device for the map's location-assignment
// panel. Unlike TopologyDeviceDTO it includes devices with no coordinates,
// so the operator can find unplaced devices and pin them.
type TopologyDeviceListDTO struct {
	ID        uint     `json:"id"`
	Name      string   `json:"name"`
	Site      string   `json:"site"`
	SiteID    uint     `json:"site_id"`
	Role      string   `json:"role"`
	Status    string   `json:"status"`
	NetboxID  uint     `json:"netbox_id"`
	VM        bool     `json:"vm"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type TopologyDevicesDTO struct {
	Devices []TopologyDeviceListDTO `json:"devices"`
}

type topologyLocationRequest struct {
	SiteName        string   `json:"site_name"`
	Latitude        *float64 `json:"latitude"`
	Longitude       *float64 `json:"longitude"`
	PhysicalAddress string   `json:"physical_address"`
}

type TopologyLocationResponse struct {
	Device TopologyDeviceListDTO `json:"device"`
	Site   *TopologySiteDTO      `json:"site,omitempty"`
}

// fetchTopology loads every device with resolved GPS coordinates, the
// connections between them, and every synced Site (models.Site - present
// even without a device) for the network map view. Devices with no
// coordinates (own or inherited from their site) are omitted rather than
// breaking the map; a connection touching an omitted device is omitted too,
// since there's nowhere on the map to draw the missing end.
func fetchTopology(ctx context.Context, DB *gorm.DB) (*TopologyDTO, error) {
	devices, err := gorm.G[models.Device](DB).
		Where("latitude IS NOT NULL AND longitude IS NOT NULL").
		Order("Name").
		Find(ctx)
	if err != nil {
		return nil, err
	}

	onMap := make(map[uint]bool, len(devices))
	out := &TopologyDTO{Devices: make([]TopologyDeviceDTO, 0, len(devices))}
	for _, d := range devices {
		onMap[d.ID] = true
		out.Devices = append(out.Devices, TopologyDeviceDTO{
			ID:          d.ID,
			Name:        d.Name,
			Site:        d.Site,
			Role:        d.Role,
			Status:      d.Status,
			OpticalKind: d.OpticalKind,
			Latitude:    *d.Latitude,
			Longitude:   *d.Longitude,
		})
	}

	connections, err := gorm.G[models.Connection](DB).Find(ctx)
	if err != nil {
		return nil, err
	}

	interfaceIDs := make([]uint, 0, len(connections)*2)
	for _, c := range connections {
		interfaceIDs = append(interfaceIDs, c.InterfaceAID, c.InterfaceBID)
	}
	interfaces, err := gorm.G[models.Interface](DB).Where("id IN ?", interfaceIDs).Find(ctx)
	if err != nil {
		return nil, err
	}
	nameByID := make(map[uint]string, len(interfaces))
	for _, i := range interfaces {
		nameByID[i.ID] = i.Name
	}

	out.Edges = make([]TopologyEdgeDTO, 0, len(connections))
	for _, c := range connections {
		if !onMap[c.DeviceAID] || !onMap[c.DeviceBID] {
			continue
		}
		out.Edges = append(out.Edges, TopologyEdgeDTO{
			ID:         c.ID,
			DeviceAID:  c.DeviceAID,
			InterfaceA: nameByID[c.InterfaceAID],
			DeviceBID:  c.DeviceBID,
			InterfaceB: nameByID[c.InterfaceBID],
			Label:      c.Label,
		})
	}

	sites, err := gorm.G[models.Site](DB).Order("Name").Find(ctx)
	if err != nil {
		return nil, err
	}
	out.Sites = make([]TopologySiteDTO, 0, len(sites))
	for _, s := range sites {
		out.Sites = append(out.Sites, TopologySiteDTO{
			ID:        s.ID,
			Name:      s.Name,
			Latitude:  s.Latitude,
			Longitude: s.Longitude,
		})
	}
	return out, nil
}

// ApiGetTopology returns every mappable device and the connections between
// them, for the 3D network map view.
func (ctrl *Controller) ApiGetTopology(c *echo.Context) error {
	topo, err := fetchTopology(c.Request().Context(), ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, topo)
}

func topologyDeviceListDTO(d models.Device) TopologyDeviceListDTO {
	return TopologyDeviceListDTO{
		ID:        d.ID,
		Name:      d.Name,
		Site:      d.Site,
		SiteID:    d.SiteID,
		Role:      d.Role,
		Status:    d.Status,
		NetboxID:  d.NetboxID,
		VM:        d.VM,
		Latitude:  d.Latitude,
		Longitude: d.Longitude,
	}
}

func topologySiteDTO(s models.Site) TopologySiteDTO {
	return TopologySiteDTO{
		ID:        s.ID,
		Name:      s.Name,
		Latitude:  s.Latitude,
		Longitude: s.Longitude,
	}
}

// ApiGetTopologyDevices returns every physical device (placed or not) for
// the map's location-assignment panel. Virtual machines are omitted: they
// have no coordinates of their own, only the host they run on does. The
// main /topology payload still omits unlocated devices because the map
// has nowhere to plot them.
func (ctrl *Controller) ApiGetTopologyDevices(c *echo.Context) error {
	devices, err := gorm.G[models.Device](ctrl.DB).Order("Name").Find(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := TopologyDevicesDTO{Devices: make([]TopologyDeviceListDTO, 0, len(devices))}
	for _, d := range devices {
		if d.VM {
			continue
		}
		out.Devices = append(out.Devices, topologyDeviceListDTO(d))
	}
	return c.JSON(http.StatusOK, out)
}

// ApiTopologyDeviceLocation writes GPS to Netbox: onto the named site
// (creating it if needed) when site_name is set, or onto the device
// itself when site_name is empty.
func (ctrl *Controller) ApiTopologyDeviceLocation(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var req topologyLocationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	device, err := gorm.G[models.Device](ctrl.DB).Where("id = ?", id).First(c.Request().Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if !netboxConfigured(settings) {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "netbox is not configured"})
	}
	nb, err := ctrl.newNetboxClient(settings)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	got, err := netbox.AssignDeviceLocation(ctrl.DB, nb, device, netbox.AssignLocationInput{
		SiteName:        req.SiteName,
		Latitude:        req.Latitude,
		Longitude:       req.Longitude,
		PhysicalAddress: strings.TrimSpace(req.PhysicalAddress),
	})
	if err != nil {
		if errors.Is(err, netbox.ErrInvalidLocation) {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	resp := TopologyLocationResponse{Device: topologyDeviceListDTO(got.Device)}
	if got.Site != nil {
		s := topologySiteDTO(*got.Site)
		resp.Site = &s
	}
	return c.JSON(http.StatusOK, resp)
}

type TopologyGeocodeDTO struct {
	Address string `json:"address"`
}

// ApiTopologyGeocode reverse-geocodes a map click into a physical address
// (OSM Nominatim) so the assign-locations panel can show it and, when a
// site is being written, POST it onto the Netbox site. Missing / failed
// lookups return an empty address, not an error — the coordinates are
// still usable on their own.
func (ctrl *Controller) ApiTopologyGeocode(c *echo.Context) error {
	lat, err := strconv.ParseFloat(c.QueryParam("lat"), 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid lat"})
	}
	lng, err := strconv.ParseFloat(c.QueryParam("lng"), 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid lng"})
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "coordinates out of range"})
	}
	addr, err := netbox.ReverseGeocode(lat, lng)
	if err != nil {
		return c.JSON(http.StatusOK, TopologyGeocodeDTO{Address: ""})
	}
	return c.JSON(http.StatusOK, TopologyGeocodeDTO{Address: addr})
}
