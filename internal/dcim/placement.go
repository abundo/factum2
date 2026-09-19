package dcim

import (
	"errors"
	"net/http"
	"sort"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlaceRequest struct {
	DeviceID    uint
	RackID      uint
	OffsetTicks int
	Face        string
	Version     int
}

func lockOrder(ids ...uint) []uint {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func lockRacks(tx *gorm.DB, ids ...uint) error {
	for _, id := range lockOrder(ids...) {
		var r models.Rack
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errf(http.StatusNotFound, ReasonNotFound, "rack not found")
			}
			return err
		}
	}
	return nil
}

func lockDevice(tx *gorm.DB, id uint) (models.Device, error) {
	var d models.Device
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return d, errf(http.StatusNotFound, ReasonNotFound, "device not found")
		}
		return d, err
	}
	return d, nil
}

func loadType(tx *gorm.DB, id uint) (models.DeviceType, error) {
	var dt models.DeviceType
	if id == 0 {
		return dt, nil
	}
	err := tx.First(&dt, id).Error
	return dt, err
}

func heightAndDepth(dt models.DeviceType) (height int, full bool, known bool) {
	if dt.HeightTicks == nil {
		return 0, false, false
	}
	h := *dt.HeightTicks
	if h < 0 {
		h = 0
	}
	if dt.FullDepth != nil {
		full = *dt.FullDepth
	}
	return h, full, true
}

func existingPlacement(tx *gorm.DB, deviceID uint) (*models.DevicePlacement, error) {
	var p models.DevicePlacement
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ?", deviceID).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func occupancyMaps(tx *gorm.DB, rackID uint) ([]models.DevicePlacement, map[uint]int, map[uint]bool, error) {
	var placements []models.DevicePlacement
	if err := tx.Where("rack_id = ?", rackID).Find(&placements).Error; err != nil {
		return nil, nil, nil, err
	}
	deviceIDs := make([]uint, 0, len(placements))
	for _, p := range placements {
		deviceIDs = append(deviceIDs, p.DeviceID)
	}
	heights := map[uint]int{}
	depths := map[uint]bool{}
	if len(deviceIDs) == 0 {
		return placements, heights, depths, nil
	}
	var devices []models.Device
	if err := tx.Select("id", "device_type_id").Where("id IN ?", deviceIDs).Find(&devices).Error; err != nil {
		return nil, nil, nil, err
	}
	typeIDs := make([]uint, 0, len(devices))
	typeByDev := map[uint]uint{}
	for _, d := range devices {
		typeByDev[d.ID] = d.DeviceTypeID
		if d.DeviceTypeID != 0 {
			typeIDs = append(typeIDs, d.DeviceTypeID)
		}
	}
	var types []models.DeviceType
	if len(typeIDs) > 0 {
		if err := tx.Where("id IN ?", typeIDs).Find(&types).Error; err != nil {
			return nil, nil, nil, err
		}
	}
	typeByID := map[uint]models.DeviceType{}
	for _, t := range types {
		typeByID[t.ID] = t
	}
	for _, d := range devices {
		t := typeByID[typeByDev[d.ID]]
		h, full, known := heightAndDepth(t)
		if known {
			heights[d.ID] = h
			depths[d.ID] = full
		}
	}
	return placements, heights, depths, nil
}

func validateSlot(rack models.Rack, offsetTicks, heightTicks int, localEditor bool) error {
	if heightTicks <= 0 {
		return errf(http.StatusBadRequest, ReasonUnknownDimensions, "device height is unknown or zero-U")
	}
	if OutOfRack(offsetTicks, heightTicks, rack.HeightTicks()) {
		return errf(http.StatusBadRequest, ReasonOutOfBounds, "placement is outside the rack")
	}
	if localEditor && !WholeUBoundary(offsetTicks, heightTicks) {
		return errf(http.StatusBadRequest, ReasonInvalid, "local placements must sit on whole-U boundaries")
	}
	return nil
}

func checkOccupancy(tx *gorm.DB, rack models.Rack, deviceID uint, offsetTicks, heightTicks int, face string, fullDepth bool) error {
	placements, heights, depths, err := occupancyMaps(tx, rack.ID)
	if err != nil {
		return err
	}
	occ := OccupancyFromPlacements(placements, func(id uint) int { return heights[id] }, func(id uint) bool { return depths[id] })
	start, end := PlacementInterval(offsetTicks, heightTicks)
	front := OccupiesFace(face, fullDepth, models.DeviceFaceFront)
	rear := OccupiesFace(face, fullDepth, models.DeviceFaceRear)
	if hit := ConflictsWith(occ, deviceID, start, end, front, rear); hit != nil {
		return errf(http.StatusConflict, ReasonOverlap, "placement overlaps another device")
	}
	return nil
}

// Place mounts or moves a local device. Imported placements cannot be changed.
func Place(db *gorm.DB, req PlaceRequest) (*models.DevicePlacement, error) {
	if req.Face == "" {
		req.Face = models.DeviceFaceFront
	}
	if req.Face != models.DeviceFaceFront && req.Face != models.DeviceFaceRear {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "face must be front or rear")
	}

	var out *models.DevicePlacement
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, err := existingPlacement(tx, req.DeviceID)
		if err != nil {
			return err
		}
		oldRackID := uint(0)
		if cur != nil {
			oldRackID = cur.RackID
		}
		if err := lockRacks(tx, oldRackID, req.RackID); err != nil {
			return err
		}
		device, err := lockDevice(tx, req.DeviceID)
		if err != nil {
			return err
		}
		if device.VM {
			return errf(http.StatusBadRequest, ReasonInvalid, "virtual machines cannot be racked")
		}
		cur, err = existingPlacement(tx, req.DeviceID)
		if err != nil {
			return err
		}
		if cur != nil && !cur.IsLocal() {
			return errf(http.StatusForbidden, ReasonImportedReadOnly, "imported placements cannot be edited here")
		}
		if cur != nil && req.Version != 0 && cur.Version != req.Version {
			return errf(http.StatusConflict, ReasonStaleVersion, "placement was changed by another editor")
		}

		var rack models.Rack
		if err := tx.First(&rack, req.RackID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errf(http.StatusNotFound, ReasonNotFound, "rack not found")
			}
			return err
		}

		ok, err := DeviceAtLocalSite(tx, device, rack.SiteID)
		if err != nil {
			return err
		}
		if !ok && device.SiteID != 0 {
			return errf(http.StatusBadRequest, ReasonInvalid, "device is not at this rack's site")
		}

		dt, err := loadType(tx, device.DeviceTypeID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		height, full, known := heightAndDepth(dt)
		if !known || height == 0 {
			return errf(http.StatusBadRequest, ReasonUnknownDimensions, "device type height is unknown")
		}
		if err := validateSlot(rack, req.OffsetTicks, height, true); err != nil {
			return err
		}
		if err := checkOccupancy(tx, rack, req.DeviceID, req.OffsetTicks, height, req.Face, full); err != nil {
			return err
		}

		if cur == nil {
			p := models.DevicePlacement{
				DeviceID:    req.DeviceID,
				RackID:      req.RackID,
				OffsetTicks: req.OffsetTicks,
				Face:        req.Face,
				Source:      models.DCIMSourceFactum,
				Version:     1,
			}
			if err := tx.Create(&p).Error; err != nil {
				return err
			}
			out = &p
			return nil
		}
		cur.RackID = req.RackID
		cur.OffsetTicks = req.OffsetTicks
		cur.Face = req.Face
		cur.Version++
		if err := tx.Save(cur).Error; err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, err
}

func Unmount(db *gorm.DB, deviceID uint, version int) error {
	return db.Transaction(func(tx *gorm.DB) error {
		cur, err := existingPlacement(tx, deviceID)
		if err != nil {
			return err
		}
		if cur == nil {
			return errf(http.StatusNotFound, ReasonNotFound, "device is not placed")
		}
		if err := lockRacks(tx, cur.RackID); err != nil {
			return err
		}
		if _, err := lockDevice(tx, deviceID); err != nil {
			return err
		}
		cur, err = existingPlacement(tx, deviceID)
		if err != nil {
			return err
		}
		if cur == nil {
			return errf(http.StatusNotFound, ReasonNotFound, "device is not placed")
		}
		if !cur.IsLocal() {
			return errf(http.StatusForbidden, ReasonImportedReadOnly, "imported placements cannot be edited here")
		}
		if version != 0 && cur.Version != version {
			return errf(http.StatusConflict, ReasonStaleVersion, "placement was changed by another editor")
		}
		return tx.Delete(cur).Error
	})
}

// ValidateRackHeight rejects shrinking a rack over existing placements.
func ValidateRackHeight(db *gorm.DB, rack models.Rack, newHeightU int) error {
	if newHeightU <= 0 {
		return errf(http.StatusBadRequest, ReasonInvalid, "rack height must be at least 1U")
	}
	placements, heights, _, err := occupancyMaps(db, rack.ID)
	if err != nil {
		return err
	}
	rackTicks := newHeightU * models.TicksPerU
	for _, p := range placements {
		h := heights[p.DeviceID]
		if h <= 0 {
			continue
		}
		if OutOfRack(p.OffsetTicks, h, rackTicks) {
			return errf(http.StatusBadRequest, ReasonOutOfBounds, "existing placement would fall outside the shorter rack")
		}
	}
	return nil
}

// ValidateDeviceTypeResize rejects a size change that would overlap or
// overflow any placement of that type.
func ValidateDeviceTypeResize(db *gorm.DB, typeID uint, newHeight *int, newFull *bool) error {
	if typeID == 0 {
		return nil
	}
	var devices []models.Device
	if err := db.Select("id").Where("device_type_id = ?", typeID).Find(&devices).Error; err != nil {
		return err
	}
	if len(devices) == 0 {
		return nil
	}
	ids := make([]uint, len(devices))
	for i, d := range devices {
		ids[i] = d.ID
	}
	var placements []models.DevicePlacement
	if err := db.Where("device_id IN ?", ids).Find(&placements).Error; err != nil {
		return err
	}
	if len(placements) == 0 {
		return nil
	}

	var dt models.DeviceType
	if err := db.First(&dt, typeID).Error; err != nil {
		return err
	}
	height, full, known := heightAndDepth(dt)
	if newHeight != nil {
		height = *newHeight
		known = true
	}
	if newFull != nil {
		full = *newFull
	}
	if !known {
		return nil
	}
	if height == 0 {
		return errf(http.StatusBadRequest, ReasonUnknownDimensions, "placed devices cannot shrink to zero-U")
	}

	byRack := map[uint][]models.DevicePlacement{}
	for _, p := range placements {
		byRack[p.RackID] = append(byRack[p.RackID], p)
	}
	for rackID, mine := range byRack {
		var rack models.Rack
		if err := db.First(&rack, rackID).Error; err != nil {
			return err
		}
		all, heights, depths, err := occupancyMaps(db, rackID)
		if err != nil {
			return err
		}
		for _, p := range mine {
			heights[p.DeviceID] = height
			depths[p.DeviceID] = full
			if err := validateSlot(rack, p.OffsetTicks, height, p.IsLocal()); err != nil {
				return err
			}
		}
		occ := OccupancyFromPlacements(all, func(id uint) int { return heights[id] }, func(id uint) bool { return depths[id] })
		for _, p := range mine {
			start, end := PlacementInterval(p.OffsetTicks, height)
			front := OccupiesFace(p.Face, full, models.DeviceFaceFront)
			rear := OccupiesFace(p.Face, full, models.DeviceFaceRear)
			if hit := ConflictsWith(occ, p.DeviceID, start, end, front, rear); hit != nil {
				return errf(http.StatusConflict, ReasonOverlap, "new device type size overlaps another device")
			}
		}
	}
	return nil
}
