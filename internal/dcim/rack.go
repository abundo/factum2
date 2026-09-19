package dcim

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

type RackWrite struct {
	SiteID    uint   `json:"site_id"`
	Name      string `json:"name"`
	HeightU   int    `json:"height_u"`
	WidthMM   int    `json:"width_mm"`
	DepthMM   int    `json:"depth_mm"`
	StartUnit int    `json:"start_unit"`
	Numbering string `json:"numbering"`
	Version   int    `json:"version"`
}

func summarizeRacks(db *gorm.DB, racks []models.Rack) ([]RackSummary, error) {
	if len(racks) == 0 {
		return []RackSummary{}, nil
	}
	siteIDs := make([]uint, 0, len(racks))
	rackIDs := make([]uint, 0, len(racks))
	for _, r := range racks {
		siteIDs = append(siteIDs, r.SiteID)
		rackIDs = append(rackIDs, r.ID)
	}
	var sites []models.Site
	if err := db.Select("id", "name").Where("id IN ?", siteIDs).Find(&sites).Error; err != nil {
		return nil, err
	}
	siteName := map[uint]string{}
	for _, s := range sites {
		siteName[s.ID] = s.Name
	}

	out := make([]RackSummary, 0, len(racks))
	for _, r := range racks {
		sum, err := summarizeOne(db, r, siteName[r.SiteID])
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}

func summarizeOne(db *gorm.DB, r models.Rack, siteName string) (RackSummary, error) {
	placements, heights, depths, err := occupancyMaps(db, r.ID)
	if err != nil {
		return RackSummary{}, err
	}
	unknown := 0
	conflict := false
	for _, p := range placements {
		if _, ok := heights[p.DeviceID]; !ok {
			unknown++
		}
		if p.Conflict != "" {
			conflict = true
		}
	}
	occ := OccupancyFromPlacements(placements, func(id uint) int { return heights[id] }, func(id uint) bool { return depths[id] })
	return RackSummary{
		ID:                r.ID,
		SiteID:            r.SiteID,
		SiteName:          siteName,
		Name:              r.Name,
		Source:            r.Source,
		NetboxID:          r.NetboxID,
		HeightU:           r.HeightU,
		WidthMM:           r.WidthMM,
		DepthMM:           r.DepthMM,
		StartUnit:         r.StartUnit,
		Numbering:         r.Numbering,
		Version:           r.Version,
		DeviceCount:       len(placements),
		OccupancyPercent:  OccupancyPercent(occ, r.HeightTicks()),
		UnknownDimensions: unknown,
		HasConflict:       conflict,
		ReadOnly:          rackReadOnly(r),
	}, nil
}

func ListRacks(db *gorm.DB, siteID uint) ([]RackSummary, error) {
	q := db.Order("name")
	if siteID != 0 {
		q = q.Where("site_id = ?", siteID)
	}
	var racks []models.Rack
	if err := q.Find(&racks).Error; err != nil {
		return nil, err
	}
	return summarizeRacks(db, racks)
}

func GetRack(db *gorm.DB, id uint) (*models.Rack, error) {
	var r models.Rack
	if err := db.First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errf(http.StatusNotFound, ReasonNotFound, "rack not found")
		}
		return nil, err
	}
	return &r, nil
}

func CreateRack(db *gorm.DB, w RackWrite) (*models.Rack, error) {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "name is required")
	}
	if w.SiteID == 0 {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "site_id is required")
	}
	var site models.Site
	if err := db.First(&site, w.SiteID).Error; err != nil {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "site not found")
	}
	numbering := w.Numbering
	if numbering == "" {
		numbering = models.RackNumberingAscending
	}
	if numbering != models.RackNumberingAscending && numbering != models.RackNumberingDescending {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "numbering must be ascending or descending")
	}
	r := models.Rack{
		SiteID:    w.SiteID,
		Name:      name,
		Source:    models.DCIMSourceFactum,
		HeightU:   w.HeightU,
		WidthMM:   w.WidthMM,
		DepthMM:   w.DepthMM,
		StartUnit: w.StartUnit,
		Numbering: numbering,
	}
	if err := db.Create(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

func UpdateRack(db *gorm.DB, id uint, w RackWrite) (*models.Rack, error) {
	var r models.Rack
	if err := db.First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errf(http.StatusNotFound, ReasonNotFound, "rack not found")
		}
		return nil, err
	}
	if !r.IsLocal() {
		return nil, errf(http.StatusForbidden, ReasonImportedReadOnly, "imported racks cannot be edited here")
	}
	if w.Version != 0 && r.Version != w.Version {
		return nil, errf(http.StatusConflict, ReasonStaleVersion, "rack was changed by another editor")
	}
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "name is required")
	}
	if w.SiteID != 0 && w.SiteID != r.SiteID {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "move the rack with an explicit inventory operation, not a rack edit")
	}
	height := w.HeightU
	if height == 0 {
		height = r.HeightU
	}
	if err := ValidateRackHeight(db, r, height); err != nil {
		return nil, err
	}
	numbering := w.Numbering
	if numbering == "" {
		numbering = r.Numbering
	}
	if numbering != models.RackNumberingAscending && numbering != models.RackNumberingDescending {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "numbering must be ascending or descending")
	}
	r.Name = name
	r.HeightU = height
	if w.WidthMM != 0 {
		r.WidthMM = w.WidthMM
	}
	if w.DepthMM != 0 {
		r.DepthMM = w.DepthMM
	}
	if w.StartUnit != 0 {
		r.StartUnit = w.StartUnit
	}
	r.Numbering = numbering
	r.Version++
	if err := db.Save(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

func DeleteRack(db *gorm.DB, id uint) error {
	var r models.Rack
	if err := db.First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errf(http.StatusNotFound, ReasonNotFound, "rack not found")
		}
		return err
	}
	if !r.IsLocal() {
		return errf(http.StatusForbidden, ReasonImportedReadOnly, "imported racks cannot be deleted here")
	}
	var n int64
	if err := db.Model(&models.DevicePlacement{}).Where("rack_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errf(http.StatusBadRequest, ReasonConflict, "rack still has devices")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("rack_id = ?", id).Delete(&models.FloorPlanRack{}).Error; err != nil {
			return err
		}
		return tx.Delete(&r).Error
	})
}

func Elevation(db *gorm.DB, rackID uint) (*ElevationDTO, error) {
	r, err := GetRack(db, rackID)
	if err != nil {
		return nil, err
	}
	var site models.Site
	_ = db.First(&site, r.SiteID)
	sum, err := summarizeOne(db, *r, site.Name)
	if err != nil {
		return nil, err
	}

	var placements []models.DevicePlacement
	if err := db.Where("rack_id = ?", rackID).Find(&placements).Error; err != nil {
		return nil, err
	}
	deviceIDs := make([]uint, 0, len(placements))
	for _, p := range placements {
		deviceIDs = append(deviceIDs, p.DeviceID)
	}
	devices := map[uint]models.Device{}
	if len(deviceIDs) > 0 {
		var rows []models.Device
		if err := db.Where("id IN ?", deviceIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, d := range rows {
			devices[d.ID] = d
		}
	}
	typeIDs := map[uint]bool{}
	for _, d := range devices {
		if d.DeviceTypeID != 0 {
			typeIDs[d.DeviceTypeID] = true
		}
	}
	types := map[uint]models.DeviceType{}
	if len(typeIDs) > 0 {
		ids := make([]uint, 0, len(typeIDs))
		for id := range typeIDs {
			ids = append(ids, id)
		}
		var rows []models.DeviceType
		if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, t := range rows {
			types[t.ID] = t
		}
	}

	issues := []string{}
	placeDTOs := make([]PlacementDTO, 0, len(placements))
	accessories := []DeviceBrief{}
	for _, p := range placements {
		d := devices[p.DeviceID]
		t := types[d.DeviceTypeID]
		h, full, known := heightAndDepth(t)
		dto := PlacementDTO{
			ID:            p.ID,
			DeviceID:      p.DeviceID,
			DeviceName:    d.Name,
			DeviceTypeID:  d.DeviceTypeID,
			ModelName:     d.ModelName,
			OffsetTicks:   p.OffsetTicks,
			HeightTicks:   h,
			HeightU:       TicksToU(h),
			Face:          p.Face,
			FullDepth:     full,
			Source:        p.Source,
			Version:       p.Version,
			Conflict:      p.Conflict,
			ReadOnly:      !p.IsLocal(),
			UnknownHeight: !known,
			ZeroU:         known && h == 0,
		}
		if !known {
			issues = append(issues, "unknown_dimensions")
			dto.UnknownHeight = true
		}
		if known && h == 0 {
			accessories = append(accessories, deviceBrief(d, t))
		}
		if p.Conflict != "" {
			issues = append(issues, "imported_conflict")
		}
		placeDTOs = append(placeDTOs, dto)
	}

	unplaced, err := unplacedAtSite(db, r.SiteID, deviceIDs)
	if err != nil {
		return nil, err
	}

	labels := make([]UnitLabel, 0, r.HeightU)
	for u := 0; u < r.HeightU; u++ {
		labels = append(labels, UnitLabel{
			OffsetU: u,
			Label:   DisplayUnit(r.HeightU, r.StartUnit, u, r.Numbering),
		})
	}

	return &ElevationDTO{
		Rack:        sum,
		Placements:  placeDTOs,
		Unplaced:    unplaced,
		Accessories: accessories,
		Issues:      uniqueStrings(issues),
		UnitLabels:  labels,
	}, nil
}

func deviceBrief(d models.Device, t models.DeviceType) DeviceBrief {
	src := d.CfSource
	if src == "" && d.NetboxID != 0 {
		src = models.DCIMSourceNetbox
	}
	return DeviceBrief{
		ID:           d.ID,
		Name:         d.Name,
		DeviceTypeID: d.DeviceTypeID,
		ModelName:    d.ModelName,
		VM:           d.VM,
		Source:       src,
		NetboxID:     d.NetboxID,
		HeightTicks:  t.HeightTicks,
		FullDepth:    t.FullDepth,
	}
}

func unplacedAtSite(db *gorm.DB, siteID uint, placed []uint) ([]DeviceBrief, error) {
	ids, err := DeviceIDsAtLocalSite(db, siteID)
	if err != nil {
		return nil, err
	}
	placedSet := map[uint]bool{}
	for _, id := range placed {
		placedSet[id] = true
	}
	want := make([]uint, 0, len(ids))
	for _, id := range ids {
		if !placedSet[id] {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return []DeviceBrief{}, nil
	}
	var devices []models.Device
	if err := db.Where("id IN ? AND vm = ?", want, false).Order("name").Find(&devices).Error; err != nil {
		return nil, err
	}
	typeIDs := make([]uint, 0, len(devices))
	for _, d := range devices {
		if d.DeviceTypeID != 0 {
			typeIDs = append(typeIDs, d.DeviceTypeID)
		}
	}
	types := map[uint]models.DeviceType{}
	if len(typeIDs) > 0 {
		var rows []models.DeviceType
		if err := db.Where("id IN ?", typeIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, t := range rows {
			types[t.ID] = t
		}
	}
	out := make([]DeviceBrief, 0, len(devices))
	for _, d := range devices {
		out = append(out, deviceBrief(d, types[d.DeviceTypeID]))
	}
	return out, nil
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
