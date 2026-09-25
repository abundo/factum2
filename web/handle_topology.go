package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	ID           uint    `json:"id"`
	Name         string  `json:"name"`
	Site         string  `json:"site"`
	Role         string  `json:"role"`
	Status       string  `json:"status"`
	Manufacturer string  `json:"manufacturer"`
	ModelName    string  `json:"model_name"`
	OpticalKind  string  `json:"optical_kind"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
}

// TopologyEdgeDTO is one Connection (Netbox cable) between two devices
// already present in the same response's Devices list.
type TopologyEdgeDTO struct {
	ID                    uint   `json:"id"`
	DeviceAID             uint   `json:"device_a_id"`
	InterfaceA            string `json:"interface_a"`
	InterfaceADescription string `json:"interface_a_description"`
	DeviceBID             uint   `json:"device_b_id"`
	InterfaceB            string `json:"interface_b"`
	InterfaceBDescription string `json:"interface_b_description"`
	Label                 string `json:"label"`
	// Kind is how the map colors this cable: "fiber" (an LF/LI hop),
	// "wavelength" (a VL/VI hop), "mixed" when both ride it, or "" when
	// no optical service hop references it. Capacity (CN/CI) is not a
	// cable color — those are TopologyServiceLinkDTO arcs.
	Kind       string   `json:"kind,omitempty"`
	ServiceIDs []string `json:"service_ids,omitempty"`
}

// TopologyServiceLinkDTO is one service drawn as its own arc between two
// devices already present in Devices. Capacity (CN/CI) always uses this:
// it has endpoints, not a cable path. Wavelength (VL/VI) and fiber (LF/LI)
// use it only when the path's ends are on the map but none of its hops are
// a cable already drawn — those hops recolor the cable instead.
type TopologyServiceLinkDTO struct {
	ServiceID string `json:"service_id"`
	Category  string `json:"category"`
	Kind      string `json:"kind"`
	DeviceAID uint   `json:"device_a_id"`
	DeviceBID uint   `json:"device_b_id"`
	Label     string `json:"label"`
}

// TopologySiteDTO is a Netbox site plotted on the map independently of any
// device - see models.Site, synced by internal/netbox.syncSites - so a
// site with no devices of its own still shows up.
type TopologySiteDTO struct {
	ID        uint    `json:"id"`
	Name      string  `json:"name"`
	Source    string  `json:"source"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type TopologyDTO struct {
	Devices      []TopologyDeviceDTO      `json:"devices"`
	Edges        []TopologyEdgeDTO        `json:"edges"`
	ServiceLinks []TopologyServiceLinkDTO `json:"service_links"`
	Sites        []TopologySiteDTO        `json:"sites"`
}

// TopologyDeviceListDTO is one device for the map's location-assignment
// panel. Unlike TopologyDeviceDTO it includes devices with no coordinates,
// so the operator can find unplaced devices and pin them.
type TopologyDeviceListDTO struct {
	ID           uint     `json:"id"`
	Name         string   `json:"name"`
	Site         string   `json:"site"`
	SiteID       uint     `json:"site_id"`
	Role         string   `json:"role"`
	Status       string   `json:"status"`
	Manufacturer string   `json:"manufacturer"`
	ModelName    string   `json:"model_name"`
	NetboxID     uint     `json:"netbox_id"`
	VM           bool     `json:"vm"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
}

// TopologySiteListDTO is one site for the map's location-assignment panel,
// including sites that have no GPS yet.
type TopologySiteListDTO struct {
	ID        uint     `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type TopologyDevicesDTO struct {
	Devices []TopologyDeviceListDTO `json:"devices"`
	Sites   []TopologySiteListDTO   `json:"sites"`
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
			ID:           d.ID,
			Name:         d.Name,
			Site:         d.Site,
			Role:         d.Role,
			Status:       d.Status,
			Manufacturer: d.Manufacturer,
			ModelName:    d.ModelName,
			OpticalKind:  d.OpticalKind,
			Latitude:     *d.Latitude,
			Longitude:    *d.Longitude,
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
	type ifaceInfo struct {
		Name, Description string
	}
	ifaceByID := make(map[uint]ifaceInfo, len(interfaces))
	for _, i := range interfaces {
		ifaceByID[i.ID] = ifaceInfo{Name: i.Name, Description: i.Description}
	}

	out.Edges = make([]TopologyEdgeDTO, 0, len(connections))
	for _, c := range connections {
		if !onMap[c.DeviceAID] || !onMap[c.DeviceBID] {
			continue
		}
		a := ifaceByID[c.InterfaceAID]
		b := ifaceByID[c.InterfaceBID]
		out.Edges = append(out.Edges, TopologyEdgeDTO{
			ID:                    c.ID,
			DeviceAID:             c.DeviceAID,
			InterfaceA:            a.Name,
			InterfaceADescription: a.Description,
			DeviceBID:             c.DeviceBID,
			InterfaceB:            b.Name,
			InterfaceBDescription: b.Description,
			Label:                 c.Label,
		})
	}

	out.ServiceLinks = []TopologyServiceLinkDTO{}
	if err := applyTopologyServiceColors(ctx, DB, onMap, out); err != nil {
		return nil, err
	}

	sites, err := gorm.G[models.Site](DB).Order("Name").Find(ctx)
	if err != nil {
		return nil, err
	}
	out.Sites = make([]TopologySiteDTO, 0, len(sites))
	for _, s := range sites {
		if !s.HasCoordinates() {
			continue
		}
		out.Sites = append(out.Sites, topologySiteDTO(s))
	}
	return out, nil
}

// topologyLinkKind maps a service-id category onto a map line color.
// LF/LI are dark fiber, VL/VI are wavelength, CN/CI are capacity.
// Anything else (free-text Lime ids) is not colored.
func topologyLinkKind(category string) string {
	switch category {
	case "LF", "LI":
		return "fiber"
	case "VL", "VI":
		return "wavelength"
	case "CN", "CI":
		return "capacity"
	default:
		return ""
	}
}

type topologyEdgeMark struct {
	kinds    map[string]struct{}
	services map[string]struct{}
}

func (m *topologyEdgeMark) add(kind, serviceID string) {
	if m.kinds == nil {
		m.kinds = map[string]struct{}{}
		m.services = map[string]struct{}{}
	}
	m.kinds[kind] = struct{}{}
	if serviceID != "" {
		m.services[serviceID] = struct{}{}
	}
}

func (m topologyEdgeMark) kind() string {
	switch len(m.kinds) {
	case 0:
		return ""
	case 1:
		for k := range m.kinds {
			return k
		}
	}
	return "mixed"
}

func (m topologyEdgeMark) serviceIDs() []string {
	if len(m.services) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.services))
	for id := range m.services {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// applyTopologyServiceColors recolors cables that a fiber or wavelength
// hop rides, and adds direct arcs for capacity services plus fiber or
// wavelength paths whose hops are not already a drawn cable.
func applyTopologyServiceColors(ctx context.Context, DB *gorm.DB, onMap map[uint]bool, out *TopologyDTO) error {
	services, err := gorm.G[models.Service](DB).Find(ctx)
	if err != nil {
		return err
	}
	byPK := make(map[uint]models.Service, len(services))
	for _, s := range services {
		byPK[s.ID] = s
	}

	edgeIndex := make(map[uint]int, len(out.Edges))
	for i, e := range out.Edges {
		edgeIndex[e.ID] = i
	}

	hops, err := gorm.G[models.ServiceHop](DB).Where("kind = ?", models.HopConnection).Find(ctx)
	if err != nil {
		return err
	}
	marks := map[uint]*topologyEdgeMark{}
	hopVisible := map[uint]bool{}
	for _, h := range hops {
		if h.ConnectionID == nil {
			continue
		}
		if _, drawn := edgeIndex[*h.ConnectionID]; !drawn {
			continue
		}
		svc, ok := byPK[h.ServiceID]
		if !ok {
			continue
		}
		kind := topologyLinkKind(models.CategoryFromServiceID(svc.ServiceID))
		if kind != "fiber" && kind != "wavelength" {
			continue
		}
		mark := marks[*h.ConnectionID]
		if mark == nil {
			mark = &topologyEdgeMark{}
			marks[*h.ConnectionID] = mark
		}
		mark.add(kind, svc.ServiceID)
		hopVisible[h.ServiceID] = true
	}
	for connID, mark := range marks {
		e := &out.Edges[edgeIndex[connID]]
		e.Kind = mark.kind()
		e.ServiceIDs = mark.serviceIDs()
	}

	paths, err := gorm.G[models.ServicePath](DB).Find(ctx)
	if err != nil {
		return err
	}
	ifaceIDs := make([]uint, 0, len(paths)*2)
	for _, p := range paths {
		ifaceIDs = append(ifaceIDs, p.EndpointAInterfaceID, p.EndpointZInterfaceID)
	}
	ifaceDevice := map[uint]uint{}
	if len(ifaceIDs) > 0 {
		ifaces, err := gorm.G[models.Interface](DB).Where("id IN ?", ifaceIDs).Find(ctx)
		if err != nil {
			return err
		}
		for _, iface := range ifaces {
			ifaceDevice[iface.ID] = iface.DeviceID
		}
	}

	endpoints, err := gorm.G[models.ServiceEndpoint](DB).Find(ctx)
	if err != nil {
		return err
	}
	devicesByService := map[uint][]uint{}
	for _, ep := range endpoints {
		if !onMap[ep.DeviceID] {
			continue
		}
		devicesByService[ep.ServiceID] = append(devicesByService[ep.ServiceID], ep.DeviceID)
	}

	links := make([]TopologyServiceLinkDTO, 0)
	for _, svc := range services {
		if topologyLinkKind(models.CategoryFromServiceID(svc.ServiceID)) != "capacity" {
			continue
		}
		for _, pair := range topologyStarPairs(devicesByService[svc.ID]) {
			links = append(links, TopologyServiceLinkDTO{
				ServiceID: svc.ServiceID,
				Category:  models.CategoryFromServiceID(svc.ServiceID),
				Kind:      "capacity",
				DeviceAID: pair[0],
				DeviceBID: pair[1],
				Label:     svc.ServiceID,
			})
		}
	}
	for _, p := range paths {
		svc, ok := byPK[p.ServiceID]
		if !ok || hopVisible[p.ServiceID] {
			continue
		}
		kind := topologyLinkKind(models.CategoryFromServiceID(svc.ServiceID))
		if kind != "fiber" && kind != "wavelength" {
			continue
		}
		a := ifaceDevice[p.EndpointAInterfaceID]
		b := ifaceDevice[p.EndpointZInterfaceID]
		if a == 0 || b == 0 || a == b || !onMap[a] || !onMap[b] {
			continue
		}
		links = append(links, TopologyServiceLinkDTO{
			ServiceID: svc.ServiceID,
			Category:  models.CategoryFromServiceID(svc.ServiceID),
			Kind:      kind,
			DeviceAID: a,
			DeviceBID: b,
			Label:     svc.ServiceID,
		})
	}
	slices.SortFunc(links, func(a, b TopologyServiceLinkDTO) int {
		if a.ServiceID != b.ServiceID {
			return strings.Compare(a.ServiceID, b.ServiceID)
		}
		if a.DeviceAID != b.DeviceAID {
			return int(a.DeviceAID) - int(b.DeviceAID)
		}
		return int(a.DeviceBID) - int(b.DeviceBID)
	})
	out.ServiceLinks = links
	return nil
}

// topologyStarPairs joins every other on-map endpoint device to the
// lowest device id. Two endpoints are one line; an ELAN is a star rather
// than a full mesh.
func topologyStarPairs(deviceIDs []uint) [][2]uint {
	if len(deviceIDs) < 2 {
		return nil
	}
	seen := make(map[uint]struct{}, len(deviceIDs))
	ids := make([]uint, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) < 2 {
		return nil
	}
	out := make([][2]uint, 0, len(ids)-1)
	for _, id := range ids[1:] {
		out = append(out, [2]uint{ids[0], id})
	}
	return out
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
		ID:           d.ID,
		Name:         d.Name,
		Site:         d.Site,
		SiteID:       d.SiteID,
		Role:         d.Role,
		Status:       d.Status,
		Manufacturer: d.Manufacturer,
		ModelName:    d.ModelName,
		NetboxID:     d.NetboxID,
		VM:           d.VM,
		Latitude:     d.Latitude,
		Longitude:    d.Longitude,
	}
}

func topologySiteDTO(s models.Site) TopologySiteDTO {
	return TopologySiteDTO{
		ID:        s.ID,
		Name:      s.Name,
		Source:    s.Source,
		Latitude:  s.Latitude,
		Longitude: s.Longitude,
	}
}

func topologySiteListDTO(s models.Site) TopologySiteListDTO {
	dto := TopologySiteListDTO{ID: s.ID, Name: s.Name, Source: s.Source}
	if s.HasCoordinates() {
		lat, lng := s.Latitude, s.Longitude
		dto.Latitude = &lat
		dto.Longitude = &lng
	}
	return dto
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
	sites, err := gorm.G[models.Site](ctrl.DB).Order("Name").Find(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out.Sites = make([]TopologySiteListDTO, 0, len(sites))
	for _, s := range sites {
		out.Sites = append(out.Sites, topologySiteListDTO(s))
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

	if device.NetboxID == 0 {
		got, err := assignFactumDeviceLocation(ctrl.DB, device, req)
		if err != nil {
			if errors.Is(err, netbox.ErrInvalidLocation) {
				return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			}
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, factumLocationResponse(got))
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

type TopologySiteLocationResponse struct {
	Site TopologySiteDTO `json:"site"`
}

// ApiTopologySiteLocation writes GPS onto a Factum-created site and
// mirrors it onto devices that inherit that site's coordinates.
func (ctrl *Controller) ApiTopologySiteLocation(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var req topologyLocationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	site, err := gorm.G[models.Site](ctrl.DB).Where("id = ?", id).First(c.Request().Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "site not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	got, err := assignFactumSiteLocation(ctrl.DB, site, req.Latitude, req.Longitude)
	if err != nil {
		if errors.Is(err, netbox.ErrInvalidLocation) {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, TopologySiteLocationResponse{Site: topologySiteDTO(*got)})
}

func factumLocationResponse(got *netbox.AssignLocationResult) TopologyLocationResponse {
	resp := TopologyLocationResponse{Device: topologyDeviceListDTO(got.Device)}
	if got.Site != nil {
		s := topologySiteDTO(*got.Site)
		resp.Site = &s
	}
	return resp
}

func roundLocationCoord(v float64) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 6, 64), 64)
	if err != nil {
		return v
	}
	return r
}

func locationCoords(lat, lng *float64) (float64, float64, error) {
	if lat == nil || lng == nil {
		return 0, 0, fmt.Errorf("%w: latitude and longitude are required", netbox.ErrInvalidLocation)
	}
	latV := roundLocationCoord(*lat)
	lngV := roundLocationCoord(*lng)
	if latV < -90 || latV > 90 {
		return 0, 0, fmt.Errorf("%w: latitude must be between -90 and 90", netbox.ErrInvalidLocation)
	}
	if lngV < -180 || lngV > 180 {
		return 0, 0, fmt.Errorf("%w: longitude must be between -180 and 180", netbox.ErrInvalidLocation)
	}
	return latV, lngV, nil
}

func assignFactumDeviceLocation(db *gorm.DB, device models.Device, req topologyLocationRequest) (*netbox.AssignLocationResult, error) {
	if device.VM {
		return nil, fmt.Errorf("%w: virtual machines have no coordinates of their own", netbox.ErrInvalidLocation)
	}
	name := strings.TrimSpace(req.SiteName)
	if strings.EqualFold(name, "Default") {
		return nil, fmt.Errorf("%w: Default is a placeholder and cannot be used as a site", netbox.ErrInvalidLocation)
	}
	lat, lng, err := locationCoords(req.Latitude, req.Longitude)
	if err != nil {
		return nil, err
	}
	if name == "" {
		if err := db.Model(&models.Device{}).Where("id = ?", device.ID).Updates(map[string]any{
			"latitude":  lat,
			"longitude": lng,
		}).Error; err != nil {
			return nil, err
		}
		var out models.Device
		if err := db.First(&out, device.ID).Error; err != nil {
			return nil, err
		}
		return &netbox.AssignLocationResult{Device: out}, nil
	}

	site, err := ensureFactumSiteCoords(db, name, lat, lng)
	if err != nil {
		return nil, err
	}
	if err := db.Model(&models.Device{}).Where("id = ?", device.ID).Updates(map[string]any{
		"site":      site.Name,
		"site_id":   site.ID,
		"latitude":  lat,
		"longitude": lng,
	}).Error; err != nil {
		return nil, err
	}
	if err := inheritSiteCoords(db, *site, lat, lng, device.ID); err != nil {
		return nil, err
	}
	var out models.Device
	if err := db.First(&out, device.ID).Error; err != nil {
		return nil, err
	}
	return &netbox.AssignLocationResult{Device: out, Site: site}, nil
}

func assignFactumSiteLocation(db *gorm.DB, site models.Site, latPtr, lngPtr *float64) (*models.Site, error) {
	if !site.IsLocal() {
		return nil, fmt.Errorf("%w: sites synced from NetBox cannot be edited here", netbox.ErrInvalidLocation)
	}
	lat, lng, err := locationCoords(latPtr, lngPtr)
	if err != nil {
		return nil, err
	}
	prev := site
	if err := db.Model(&models.Site{}).Where("id = ?", site.ID).Updates(map[string]any{
		"latitude":  lat,
		"longitude": lng,
	}).Error; err != nil {
		return nil, err
	}
	site.Latitude = lat
	site.Longitude = lng
	if err := inheritSiteCoords(db, prev, lat, lng, 0); err != nil {
		return nil, err
	}
	var out models.Site
	if err := db.First(&out, site.ID).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func ensureFactumSiteCoords(db *gorm.DB, name string, lat, lng float64) (*models.Site, error) {
	var site models.Site
	err := db.Where("LOWER(name) = LOWER(?)", name).First(&site).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		site = models.Site{
			Name:      name,
			Latitude:  lat,
			Longitude: lng,
			Source:    models.SiteSourceFactum,
		}
		if err := db.Create(&site).Error; err != nil {
			return nil, err
		}
		return &site, nil
	}
	if err != nil {
		return nil, err
	}
	if !site.IsLocal() {
		return nil, fmt.Errorf("%w: site %s is synced from NetBox and cannot be edited here", netbox.ErrInvalidLocation, site.Name)
	}
	if err := db.Model(&models.Site{}).Where("id = ?", site.ID).Updates(map[string]any{
		"latitude":  lat,
		"longitude": lng,
	}).Error; err != nil {
		return nil, err
	}
	site.Latitude = lat
	site.Longitude = lng
	return &site, nil
}

func inheritSiteCoords(db *gorm.DB, site models.Site, lat, lng float64, skipDeviceID uint) error {
	q := db.Model(&models.Device{}).Where("site_id = ? OR site = ?", site.ID, site.Name)
	if skipDeviceID != 0 {
		q = q.Where("id <> ?", skipDeviceID)
	}
	if site.HasCoordinates() {
		q = q.Where(
			"(latitude IS NULL AND longitude IS NULL) OR (latitude = ? AND longitude = ?)",
			site.Latitude, site.Longitude,
		)
	} else {
		q = q.Where("latitude IS NULL AND longitude IS NULL")
	}
	return q.Updates(map[string]any{
		"site":      site.Name,
		"site_id":   site.ID,
		"latitude":  lat,
		"longitude": lng,
	}).Error
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
