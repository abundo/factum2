package dcim

import "github.com/abundo/factum2/models"

type RackSummary struct {
	ID                uint   `json:"id"`
	SiteID            uint   `json:"site_id"`
	SiteName          string `json:"site_name,omitempty"`
	Name              string `json:"name"`
	Source            string `json:"source"`
	NetboxID          uint   `json:"netbox_id,omitempty"`
	HeightU           int    `json:"height_u"`
	WidthMM           int    `json:"width_mm"`
	DepthMM           int    `json:"depth_mm"`
	StartUnit         int    `json:"start_unit"`
	Numbering         string `json:"numbering"`
	Version           int    `json:"version"`
	DeviceCount       int    `json:"device_count"`
	OccupancyPercent  int    `json:"occupancy_percent"`
	UnknownDimensions int    `json:"unknown_dimensions"`
	HasConflict       bool   `json:"has_conflict"`
	ReadOnly          bool   `json:"read_only"`
}

type PlacementDTO struct {
	ID            uint    `json:"id"`
	DeviceID      uint    `json:"device_id"`
	DeviceName    string  `json:"device_name"`
	DeviceTypeID  uint    `json:"device_type_id,omitempty"`
	ModelName     string  `json:"model_name,omitempty"`
	OffsetTicks   int     `json:"offset_ticks"`
	HeightTicks   int     `json:"height_ticks"`
	HeightU       float64 `json:"height_u"`
	Face          string  `json:"face"`
	FullDepth     bool    `json:"full_depth"`
	Source        string  `json:"source"`
	Version       int     `json:"version"`
	Conflict      string  `json:"conflict,omitempty"`
	ReadOnly      bool    `json:"read_only"`
	UnknownHeight bool    `json:"unknown_height"`
	ZeroU         bool    `json:"zero_u"`
}

type DeviceBrief struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	DeviceTypeID uint   `json:"device_type_id,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
	VM           bool   `json:"vm"`
	Source       string `json:"source,omitempty"`
	NetboxID     uint   `json:"netbox_id,omitempty"`
	HeightTicks  *int   `json:"height_ticks"`
	FullDepth    *bool  `json:"full_depth"`
}

type ElevationDTO struct {
	Rack        RackSummary    `json:"rack"`
	Placements  []PlacementDTO `json:"placements"`
	Unplaced    []DeviceBrief  `json:"unplaced"`
	Accessories []DeviceBrief  `json:"accessories"`
	Issues      []string       `json:"issues"`
	UnitLabels  []UnitLabel    `json:"unit_labels"`
}

type UnitLabel struct {
	OffsetU int `json:"offset_u"`
	Label   int `json:"label"`
}

type FloorPlanRackDTO struct {
	ID       uint        `json:"id,omitempty"`
	RackID   uint        `json:"rack_id"`
	XMM      int         `json:"x_mm"`
	YMM      int         `json:"y_mm"`
	Rotation int         `json:"rotation"`
	Rack     RackSummary `json:"rack"`
}

type FloorPlanAnnotationDTO struct {
	ID       uint   `json:"id,omitempty"`
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	XMM      int    `json:"x_mm"`
	YMM      int    `json:"y_mm"`
	WidthMM  int    `json:"width_mm"`
	HeightMM int    `json:"height_mm"`
	Rotation int    `json:"rotation"`
}

type FloorPlanDTO struct {
	ID          uint                     `json:"id"`
	SiteID      uint                     `json:"site_id"`
	SiteName    string                   `json:"site_name,omitempty"`
	Name        string                   `json:"name"`
	WidthMM     int                      `json:"width_mm"`
	HeightMM    int                      `json:"height_mm"`
	GridMM      int                      `json:"grid_mm"`
	Revision    int                      `json:"revision"`
	Racks       []FloorPlanRackDTO       `json:"racks"`
	Annotations []FloorPlanAnnotationDTO `json:"annotations"`
	Available   []RackSummary            `json:"available_racks,omitempty"`
}

type GraphNode struct {
	ID         uint        `json:"id"`
	Name       string      `json:"name"`
	External   bool        `json:"external,omitempty"`
	Site       string      `json:"site,omitempty"`
	RackID     uint        `json:"rack_id,omitempty"`
	Interfaces []GraphPort `json:"interfaces"`
}

type GraphPort struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type GraphEdge struct {
	ID           uint   `json:"id"`
	Label        string `json:"label,omitempty"`
	DeviceAID    uint   `json:"device_a_id"`
	InterfaceAID uint   `json:"interface_a_id"`
	DeviceBID    uint   `json:"device_b_id"`
	InterfaceBID uint   `json:"interface_b_id"`
	Unavailable  bool   `json:"unavailable,omitempty"`
}

type GraphDTO struct {
	Scope     string      `json:"scope"`
	Truncated bool        `json:"truncated"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
}

// PairLink is the cable occupying one interface, if any.
type PairLink struct {
	ID                uint   `json:"id"`
	Label             string `json:"label,omitempty"`
	NetboxID          uint   `json:"netbox_id,omitempty"`
	Source            string `json:"source"`
	PeerDeviceID      uint   `json:"peer_device_id"`
	PeerDeviceName    string `json:"peer_device_name"`
	PeerInterfaceID   uint   `json:"peer_interface_id"`
	PeerInterfaceName string `json:"peer_interface_name"`
}

type PairPort struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Type        string    `json:"type,omitempty"`
	Enabled     bool      `json:"enabled"`
	Connection  *PairLink `json:"connection,omitempty"`
}

type PairDevice struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	Site       string     `json:"site,omitempty"`
	Interfaces []PairPort `json:"interfaces"`
}

type PairCable struct {
	ID           uint   `json:"id"`
	Label        string `json:"label,omitempty"`
	NetboxID     uint   `json:"netbox_id,omitempty"`
	Source       string `json:"source"`
	DeviceAID    uint   `json:"device_a_id"`
	InterfaceAID uint   `json:"interface_a_id"`
	DeviceBID    uint   `json:"device_b_id"`
	InterfaceBID uint   `json:"interface_b_id"`
}

// PairDTO is two devices with every interface and the cables between them.
type PairDTO struct {
	DeviceA PairDevice  `json:"device_a"`
	DeviceB PairDevice  `json:"device_b"`
	Cables  []PairCable `json:"cables"`
}

type LayoutDTO struct {
	Scope    string        `json:"scope"`
	Revision int           `json:"revision"`
	Nodes    map[string]XY `json:"nodes"`
}

type XY struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func rackReadOnly(r models.Rack) bool {
	return !r.IsLocal()
}
