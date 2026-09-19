package dcim

import (
	"math"
	"net/http"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const (
	maxFloorRacks       = 200
	maxFloorAnnotations = 200
)

type FloorLayoutWrite struct {
	Revision    int                      `json:"revision"`
	WidthMM     int                      `json:"width_mm"`
	HeightMM    int                      `json:"height_mm"`
	GridMM      int                      `json:"grid_mm"`
	Racks       []FloorPlanRackDTO       `json:"racks"`
	Annotations []FloorPlanAnnotationDTO `json:"annotations"`
}

type Footprint struct {
	MinX, MinY, MaxX, MaxY int
}

func rotatedFootprint(x, y, width, depth, rotation int) Footprint {
	w, d := width, depth
	if w <= 0 {
		w = 600
	}
	if d <= 0 {
		d = 1000
	}
	rot := ((rotation % 360) + 360) % 360
	switch rot {
	case 90, 270:
		w, d = d, w
	case 0, 180:
	default:
		rot = (rot / 90) * 90
		if rot == 90 || rot == 270 {
			w, d = d, w
		}
	}
	return Footprint{MinX: x, MinY: y, MaxX: x + w, MaxY: y + d}
}

func footprintsOverlap(a, b Footprint) bool {
	return a.MinX < b.MaxX && b.MinX < a.MaxX && a.MinY < b.MaxY && b.MinY < a.MaxY
}

func SnapMM(v, grid int) int {
	if grid <= 0 {
		return v
	}
	return int(math.Round(float64(v)/float64(grid))) * grid
}

func ListFloorPlans(db *gorm.DB, siteID uint) ([]models.FloorPlan, error) {
	q := db.Order("name")
	if siteID != 0 {
		q = q.Where("site_id = ?", siteID)
	}
	var plans []models.FloorPlan
	if err := q.Find(&plans).Error; err != nil {
		return nil, err
	}
	if plans == nil {
		plans = []models.FloorPlan{}
	}
	return plans, nil
}

func CreateFloorPlan(db *gorm.DB, siteID uint, name string, width, height, grid int) (*models.FloorPlan, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "name is required")
	}
	if siteID == 0 {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "site_id is required")
	}
	var site models.Site
	if err := db.First(&site, siteID).Error; err != nil {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "site not found")
	}
	p := models.FloorPlan{SiteID: siteID, Name: name, WidthMM: width, HeightMM: height, GridMM: grid}
	if err := db.Create(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func GetFloorPlan(db *gorm.DB, id uint) (*FloorPlanDTO, error) {
	var p models.FloorPlan
	if err := db.First(&p, id).Error; err != nil {
		return nil, errf(http.StatusNotFound, ReasonNotFound, "floor plan not found")
	}
	var site models.Site
	_ = db.First(&site, p.SiteID)

	var rows []models.FloorPlanRack
	if err := db.Where("floor_plan_id = ?", id).Find(&rows).Error; err != nil {
		return nil, err
	}
	rackIDs := make([]uint, 0, len(rows))
	for _, r := range rows {
		rackIDs = append(rackIDs, r.RackID)
	}
	rackByID := map[uint]models.Rack{}
	if len(rackIDs) > 0 {
		var racks []models.Rack
		if err := db.Where("id IN ?", rackIDs).Find(&racks).Error; err != nil {
			return nil, err
		}
		for _, r := range racks {
			rackByID[r.ID] = r
		}
	}
	rackDTOs := make([]FloorPlanRackDTO, 0, len(rows))
	used := map[uint]bool{}
	for _, row := range rows {
		used[row.RackID] = true
		sum := RackSummary{ID: row.RackID, Name: "unresolved"}
		if r, ok := rackByID[row.RackID]; ok {
			s, err := summarizeOne(db, r, site.Name)
			if err != nil {
				return nil, err
			}
			sum = s
		}
		rackDTOs = append(rackDTOs, FloorPlanRackDTO{
			ID: row.ID, RackID: row.RackID, XMM: row.XMM, YMM: row.YMM, Rotation: row.Rotation, Rack: sum,
		})
	}

	var anns []models.FloorPlanAnnotation
	if err := db.Where("floor_plan_id = ?", id).Find(&anns).Error; err != nil {
		return nil, err
	}
	annDTOs := make([]FloorPlanAnnotationDTO, 0, len(anns))
	for _, a := range anns {
		annDTOs = append(annDTOs, FloorPlanAnnotationDTO{
			ID: a.ID, Kind: a.Kind, Text: a.Text, XMM: a.XMM, YMM: a.YMM,
			WidthMM: a.WidthMM, HeightMM: a.HeightMM, Rotation: a.Rotation,
		})
	}

	var siteRacks []models.Rack
	if err := db.Where("site_id = ?", p.SiteID).Order("name").Find(&siteRacks).Error; err != nil {
		return nil, err
	}
	available := []RackSummary{}
	for _, r := range siteRacks {
		if used[r.ID] {
			continue
		}
		s, err := summarizeOne(db, r, site.Name)
		if err != nil {
			return nil, err
		}
		available = append(available, s)
	}

	return &FloorPlanDTO{
		ID: p.ID, SiteID: p.SiteID, SiteName: site.Name, Name: p.Name,
		WidthMM: p.WidthMM, HeightMM: p.HeightMM, GridMM: p.GridMM, Revision: p.Revision,
		Racks: rackDTOs, Annotations: annDTOs, Available: available,
	}, nil
}

func SaveFloorLayout(db *gorm.DB, id uint, w FloorLayoutWrite) (*FloorPlanDTO, error) {
	if len(w.Racks) > maxFloorRacks {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "too many racks on this plan")
	}
	if len(w.Annotations) > maxFloorAnnotations {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "too many annotations on this plan")
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		var p models.FloorPlan
		if err := tx.First(&p, id).Error; err != nil {
			return errf(http.StatusNotFound, ReasonNotFound, "floor plan not found")
		}
		if w.Revision != 0 && p.Revision != w.Revision {
			return errf(http.StatusConflict, ReasonStaleVersion, "floor plan was changed by another editor")
		}
		width, height, grid := p.WidthMM, p.HeightMM, p.GridMM
		if w.WidthMM > 0 {
			width = w.WidthMM
		}
		if w.HeightMM > 0 {
			height = w.HeightMM
		}
		if w.GridMM > 0 {
			grid = w.GridMM
		}
		if width <= 0 || height <= 0 || grid <= 0 {
			return errf(http.StatusBadRequest, ReasonInvalid, "plan dimensions must be positive")
		}

		seenRack := map[uint]bool{}
		fps := make([]Footprint, 0, len(w.Racks))
		for _, row := range w.Racks {
			if row.RackID == 0 {
				return errf(http.StatusBadRequest, ReasonInvalid, "rack_id is required")
			}
			if seenRack[row.RackID] {
				return errf(http.StatusBadRequest, ReasonInvalid, "a rack can appear only once on a plan")
			}
			seenRack[row.RackID] = true
			if !finiteInt(row.XMM) || !finiteInt(row.YMM) {
				return errf(http.StatusBadRequest, ReasonInvalid, "rack coordinates must be finite")
			}
			rot := row.Rotation
			if rot%90 != 0 {
				return errf(http.StatusBadRequest, ReasonInvalid, "rotation must be a 90-degree increment")
			}
			var rack models.Rack
			if err := tx.First(&rack, row.RackID).Error; err != nil {
				return errf(http.StatusBadRequest, ReasonInvalid, "rack not found")
			}
			if rack.SiteID != p.SiteID {
				return errf(http.StatusBadRequest, ReasonInvalid, "rack is not at this plan's site")
			}
			fp := rotatedFootprint(row.XMM, row.YMM, rack.WidthMM, rack.DepthMM, rot)
			if fp.MinX < 0 || fp.MinY < 0 || fp.MaxX > width || fp.MaxY > height {
				return errf(http.StatusBadRequest, ReasonOutOfBounds, "rack footprint is outside the room")
			}
			for _, other := range fps {
				if footprintsOverlap(fp, other) {
					return errf(http.StatusConflict, ReasonOverlap, "rack footprints overlap")
				}
			}
			fps = append(fps, fp)
		}
		for _, a := range w.Annotations {
			switch a.Kind {
			case models.FloorAnnotationLabel, models.FloorAnnotationAisle, models.FloorAnnotationZone:
			default:
				return errf(http.StatusBadRequest, ReasonInvalid, "unknown annotation kind")
			}
		}

		if err := tx.Where("floor_plan_id = ?", id).Delete(&models.FloorPlanRack{}).Error; err != nil {
			return err
		}
		if err := tx.Where("floor_plan_id = ?", id).Delete(&models.FloorPlanAnnotation{}).Error; err != nil {
			return err
		}
		for _, row := range w.Racks {
			rec := models.FloorPlanRack{
				FloorPlanID: id, RackID: row.RackID, XMM: row.XMM, YMM: row.YMM, Rotation: row.Rotation,
			}
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
		}
		for _, a := range w.Annotations {
			rec := models.FloorPlanAnnotation{
				FloorPlanID: id, Kind: a.Kind, Text: a.Text,
				XMM: a.XMM, YMM: a.YMM, WidthMM: a.WidthMM, HeightMM: a.HeightMM, Rotation: a.Rotation,
			}
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
		}
		p.WidthMM = width
		p.HeightMM = height
		p.GridMM = grid
		p.Revision++
		return tx.Save(&p).Error
	})
	if err != nil {
		return nil, err
	}
	return GetFloorPlan(db, id)
}

func finiteInt(v int) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
