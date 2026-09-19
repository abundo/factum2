package models

import "gorm.io/gorm"

const (
	DCIMSourceFactum = "factum"
	DCIMSourceNetbox = "netbox"

	RackNumberingAscending  = "ascending"
	RackNumberingDescending = "descending"

	DeviceFaceFront = "front"
	DeviceFaceRear  = "rear"

	FloorAnnotationLabel = "label"
	FloorAnnotationAisle = "aisle"
	FloorAnnotationZone  = "zone"
)

// TicksPerU is the internal occupancy resolution: two ticks equal 1U.
const TicksPerU = 2

// Rack is a physical equipment rack. SiteID is a local sites.id.
type Rack struct {
	FactumModel
	SiteID    uint   `json:"site_id" gorm:"index;not null"`
	Name      string `json:"name" gorm:"type:varchar(255);not null"`
	Source    string `json:"source" gorm:"type:varchar(32)"`
	NetboxID  uint   `json:"netbox_id"`
	HeightU   int    `json:"height_u" gorm:"column:height_u;not null"`
	WidthMM   int    `json:"width_mm" gorm:"column:width_mm"`
	DepthMM   int    `json:"depth_mm" gorm:"column:depth_mm"`
	StartUnit int    `json:"start_unit"`
	Numbering string `json:"numbering" gorm:"type:varchar(32)"`
	Version   int    `json:"version"`
}

func (r *Rack) BeforeCreate(tx *gorm.DB) error {
	if r.Source == "" {
		r.Source = DCIMSourceFactum
	}
	if r.HeightU == 0 {
		r.HeightU = 42
	}
	if r.WidthMM == 0 {
		r.WidthMM = 600
	}
	if r.DepthMM == 0 {
		r.DepthMM = 1000
	}
	if r.StartUnit == 0 {
		r.StartUnit = 1
	}
	if r.Numbering == "" {
		r.Numbering = RackNumberingAscending
	}
	if r.Version == 0 {
		r.Version = 1
	}
	return nil
}

func (r Rack) IsLocal() bool {
	return r.Source != DCIMSourceNetbox
}

func (r Rack) HeightTicks() int {
	return r.HeightU * TicksPerU
}

// DevicePlacement is the one authoritative rack mount for a physical device.
type DevicePlacement struct {
	FactumModel
	DeviceID    uint   `json:"device_id" gorm:"uniqueIndex;not null"`
	RackID      uint   `json:"rack_id" gorm:"index;not null"`
	OffsetTicks int    `json:"offset_ticks" gorm:"column:offset_ticks"`
	Face        string `json:"face" gorm:"type:varchar(16)"`
	Source      string `json:"source" gorm:"type:varchar(32)"`
	Version     int    `json:"version"`
	// Conflict is set when imported occupancy is inconsistent with other
	// imported rows. Local writers treat the occupied interval conservatively.
	Conflict string `json:"conflict,omitempty" gorm:"type:varchar(64)"`
}

func (p *DevicePlacement) BeforeCreate(tx *gorm.DB) error {
	if p.Source == "" {
		p.Source = DCIMSourceFactum
	}
	if p.Face == "" {
		p.Face = DeviceFaceFront
	}
	if p.Version == 0 {
		p.Version = 1
	}
	return nil
}

func (p DevicePlacement) IsLocal() bool {
	return p.Source != DCIMSourceNetbox
}

// FloorPlan is a Factum drawing of a room. SiteID is a local sites.id.
type FloorPlan struct {
	FactumModel
	SiteID   uint   `json:"site_id" gorm:"index;not null"`
	Name     string `json:"name" gorm:"type:varchar(255);not null"`
	WidthMM  int    `json:"width_mm" gorm:"column:width_mm"`
	HeightMM int    `json:"height_mm" gorm:"column:height_mm"`
	GridMM   int    `json:"grid_mm" gorm:"column:grid_mm"`
	Revision int    `json:"revision"`
}

func (p *FloorPlan) BeforeCreate(tx *gorm.DB) error {
	if p.WidthMM == 0 {
		p.WidthMM = 20000
	}
	if p.HeightMM == 0 {
		p.HeightMM = 15000
	}
	if p.GridMM == 0 {
		p.GridMM = 600
	}
	if p.Revision == 0 {
		p.Revision = 1
	}
	return nil
}

// FloorPlanRack places a rack on a floor plan. X/Y are mm from the plan origin.
type FloorPlanRack struct {
	FactumModel
	FloorPlanID uint `json:"floor_plan_id" gorm:"uniqueIndex:idx_floor_plan_racks_plan_rack;not null"`
	RackID      uint `json:"rack_id" gorm:"uniqueIndex:idx_floor_plan_racks_plan_rack;index;not null"`
	XMM         int  `json:"x_mm" gorm:"column:x_mm"`
	YMM         int  `json:"y_mm" gorm:"column:y_mm"`
	Rotation    int  `json:"rotation"`
}

// FloorPlanAnnotation is a label, aisle or zone on a floor plan.
type FloorPlanAnnotation struct {
	FactumModel
	FloorPlanID uint   `json:"floor_plan_id" gorm:"index;not null"`
	Kind        string `json:"kind" gorm:"type:varchar(32)"`
	Text        string `json:"text" gorm:"type:varchar(255)"`
	XMM         int    `json:"x_mm" gorm:"column:x_mm"`
	YMM         int    `json:"y_mm" gorm:"column:y_mm"`
	WidthMM     int    `json:"width_mm" gorm:"column:width_mm"`
	HeightMM    int    `json:"height_mm" gorm:"column:height_mm"`
	Rotation    int    `json:"rotation"`
}

// ConnectionViewLayout stores per-user graph node coordinates for a scope.
// Nodes JSON is keyed by local device IDs. It never stores cable inventory.
type ConnectionViewLayout struct {
	FactumModel
	UserID   uint   `json:"user_id" gorm:"uniqueIndex:idx_connection_view_layouts_user_scope;not null"`
	Scope    string `json:"scope" gorm:"uniqueIndex:idx_connection_view_layouts_user_scope;type:varchar(64);not null"`
	Revision int    `json:"revision"`
	Nodes    string `json:"nodes" gorm:"type:text"`
}

func (l *ConnectionViewLayout) BeforeCreate(tx *gorm.DB) error {
	if l.Revision == 0 {
		l.Revision = 1
	}
	if l.Nodes == "" {
		l.Nodes = "{}"
	}
	return nil
}
