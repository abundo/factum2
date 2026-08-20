package web

import (
	"context"
	"net/http"

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
