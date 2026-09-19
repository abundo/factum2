package netbox

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/abundo/factum2/internal/dcim"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/factum2/internal/netboxtool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type nbNestedName struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type nbRackREST struct {
	ID           uint          `json:"id"`
	Name         string        `json:"name"`
	Site         nbNestedName  `json:"site"`
	Location     *nbNestedName `json:"location"`
	UHeight      int           `json:"u_height"`
	Width        int           `json:"width"`
	Depth        int           `json:"depth"`
	OuterWidth   *int          `json:"outer_width"`
	OuterDepth   *int          `json:"outer_depth"`
	StartingUnit int           `json:"starting_unit"`
	DescUnits    bool          `json:"desc_units"`
}

type nbDeviceTypeREST struct {
	ID          uint    `json:"id"`
	Model       string  `json:"model"`
	UHeight     float64 `json:"u_height"`
	IsFullDepth bool    `json:"is_full_depth"`
	FrontImage  *string `json:"front_image"`
	RearImage   *string `json:"rear_image"`
}

type nbDeviceRackREST struct {
	ID         uint          `json:"id"`
	Name       string        `json:"name"`
	DeviceType nbNestedName  `json:"device_type"`
	Rack       *nbNestedName `json:"rack"`
	Position   *float64      `json:"position"`
	Face       *struct {
		Value string `json:"value"`
	} `json:"face"`
}

func syncRacksAndPlacements(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	if err := syncDeviceTypePhysical(db, nb, reporter); err != nil {
		return err
	}
	if err := syncRacks(db, nb, reporter); err != nil {
		return err
	}
	return syncDevicePlacements(db, nb, reporter)
}

func syncDeviceTypePhysical(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbDeviceTypeREST](nb, "/api/dcim/device-types/")
	if err != nil {
		reporter.Emit(jobevent.Warning, "NetBox device-type dimensions unavailable: %s", err)
		return nil
	}
	for _, row := range rows {
		var dt models.DeviceType
		if err := db.Where("netbox_id = ? AND netbox_id != 0", row.ID).Take(&dt).Error; err != nil {
			continue
		}
		ticks := dcim.UToTicks(row.UHeight)
		dt.HeightTicks = &ticks
		full := row.IsFullDepth
		dt.FullDepth = &full
		if row.FrontImage != nil {
			dt.FrontImage = *row.FrontImage
		}
		if row.RearImage != nil {
			dt.RearImage = *row.RearImage
		}
		if err := db.Model(&dt).Select("HeightTicks", "FullDepth", "FrontImage", "RearImage").Updates(&dt).Error; err != nil {
			return err
		}
	}
	reporter.Emit(jobevent.Info, "NetBox device-type dimensions: %d", len(rows))
	return nil
}

func syncRacks(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbRackREST](nb, "/api/dcim/racks/")
	if err != nil {
		reporter.Emit(jobevent.Warning, "NetBox racks unavailable: %s", err)
		return nil
	}
	seen := make([]uint, 0, len(rows))
	for _, row := range rows {
		siteID := resolveRackSite(db, row)
		if siteID == 0 {
			slog.Warn("netbox: rack has no local site", "rack", row.Name, "netbox_id", row.ID)
			continue
		}
		numbering := models.RackNumberingAscending
		if row.DescUnits {
			numbering = models.RackNumberingDescending
		}
		height := row.UHeight
		if height <= 0 {
			height = 42
		}
		width := row.Width
		if row.OuterWidth != nil && *row.OuterWidth > 0 {
			width = *row.OuterWidth
		}
		if width <= 0 {
			width = 600
		}
		depth := row.Depth
		if row.OuterDepth != nil && *row.OuterDepth > 0 {
			depth = *row.OuterDepth
		}
		if depth <= 0 {
			depth = 1000
		}
		start := row.StartingUnit
		if start <= 0 {
			start = 1
		}
		rack := models.Rack{
			SiteID:    siteID,
			Name:      row.Name,
			Source:    models.DCIMSourceNetbox,
			NetboxID:  row.ID,
			HeightU:   height,
			WidthMM:   width,
			DepthMM:   depth,
			StartUnit: start,
			Numbering: numbering,
			Version:   1,
		}
		err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "netbox_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "netbox_id != 0"},
			}},
			DoUpdates: clause.AssignmentColumns([]string{
				"site_id", "name", "source", "height_u", "width_mm", "depth_mm", "start_unit", "numbering", "updated_at",
			}),
		}).Create(&rack).Error
		if err != nil {
			return fmt.Errorf("upsert rack %s: %w", row.Name, err)
		}
		seen = append(seen, row.ID)
	}
	if err := deleteMissingImportedRacks(db, seen); err != nil {
		return err
	}
	reporter.Emit(jobevent.Info, "NetBox racks: %d", len(rows))
	return nil
}

func resolveRackSite(db *gorm.DB, row nbRackREST) uint {
	if row.Location != nil && row.Location.ID != 0 {
		var loc models.Site
		if err := db.Where("netbox_kind = ? AND netbox_id = ?", models.SiteNetboxKindLocation, row.Location.ID).
			Take(&loc).Error; err == nil {
			return loc.ID
		}
	}
	if row.Site.ID != 0 {
		var site models.Site
		if err := db.Where("netbox_kind = ? AND netbox_id = ?", models.SiteNetboxKindSite, row.Site.ID).
			Take(&site).Error; err == nil {
			return site.ID
		}
	}
	return 0
}

func deleteMissingImportedRacks(db *gorm.DB, seen []uint) error {
	q := db.Where("source = ? AND netbox_id != 0", models.DCIMSourceNetbox)
	if len(seen) > 0 {
		q = q.Where("netbox_id NOT IN ?", seen)
	}
	var gone []models.Rack
	if err := q.Find(&gone).Error; err != nil {
		return err
	}
	for _, r := range gone {
		if err := db.Where("rack_id = ? AND source = ?", r.ID, models.DCIMSourceNetbox).
			Delete(&models.DevicePlacement{}).Error; err != nil {
			return err
		}
		if err := db.Where("rack_id = ?", r.ID).Delete(&models.FloorPlanRack{}).Error; err != nil {
			return err
		}
		if err := db.Delete(&r).Error; err != nil {
			return err
		}
	}
	return nil
}

func syncDevicePlacements(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbDeviceRackREST](nb, "/api/dcim/devices/")
	if err != nil {
		reporter.Emit(jobevent.Warning, "NetBox device placements unavailable: %s", err)
		return nil
	}
	var racks []models.Rack
	if err := db.Where("source = ? AND netbox_id != 0", models.DCIMSourceNetbox).Find(&racks).Error; err != nil {
		return err
	}
	rackByNB := map[uint]models.Rack{}
	for _, r := range racks {
		rackByNB[r.NetboxID] = r
	}
	var devices []models.Device
	if err := db.Where("netbox_id != 0 AND vm = ?", false).Find(&devices).Error; err != nil {
		return err
	}
	devByNB := map[uint]models.Device{}
	for _, d := range devices {
		devByNB[d.NetboxID] = d
	}
	var types []models.DeviceType
	if err := db.Find(&types).Error; err != nil {
		return err
	}
	typeByID := map[uint]models.DeviceType{}
	typeByNB := map[uint]models.DeviceType{}
	for _, t := range types {
		typeByID[t.ID] = t
		if t.NetboxID != 0 {
			typeByNB[t.NetboxID] = t
		}
	}

	seenDevices := map[uint]bool{}
	type occKey struct {
		rack uint
		face string
		a, b int
	}
	claimed := map[occKey]uint{}

	for _, row := range rows {
		dev, ok := devByNB[row.ID]
		if !ok {
			continue
		}
		if row.Rack == nil || row.Rack.ID == 0 || row.Position == nil {
			continue
		}
		rack, ok := rackByNB[row.Rack.ID]
		if !ok {
			continue
		}
		dt := typeByID[dev.DeviceTypeID]
		if dt.ID == 0 {
			dt = typeByNB[row.DeviceType.ID]
		}
		heightU := 0.0
		if dt.HeightTicks != nil {
			heightU = dcim.TicksToU(*dt.HeightTicks)
		}
		face := models.DeviceFaceFront
		if row.Face != nil && strings.EqualFold(row.Face.Value, models.DeviceFaceRear) {
			face = models.DeviceFaceRear
		}
		offset := dcim.OffsetFromNetboxPosition(rack.HeightU, rack.Numbering, rack.StartUnit, *row.Position, heightU)
		heightTicks := dcim.UToTicks(heightU)
		full := dt.FullDepth != nil && *dt.FullDepth
		faces := []string{face}
		if full {
			faces = []string{models.DeviceFaceFront, models.DeviceFaceRear}
		}
		conflict := ""
		for _, f := range faces {
			key := occKey{rack: rack.ID, face: f, a: offset, b: offset + heightTicks}
			for other, owner := range claimed {
				if other.rack != rack.ID || other.face != f {
					continue
				}
				if dcim.IntervalsOverlap(key.a, key.b, other.a, other.b) && owner != dev.ID {
					conflict = "overlap"
					break
				}
			}
			claimed[key] = dev.ID
		}
		p := models.DevicePlacement{
			DeviceID:    dev.ID,
			RackID:      rack.ID,
			OffsetTicks: offset,
			Face:        face,
			Source:      models.DCIMSourceNetbox,
			Version:     1,
			Conflict:    conflict,
		}
		var existing models.DevicePlacement
		err := db.Where("device_id = ?", dev.ID).Take(&existing).Error
		if err == nil {
			if existing.Source != models.DCIMSourceNetbox {
				continue
			}
			existing.RackID = p.RackID
			existing.OffsetTicks = p.OffsetTicks
			existing.Face = p.Face
			existing.Conflict = p.Conflict
			existing.Source = models.DCIMSourceNetbox
			if err := db.Save(&existing).Error; err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&p).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		seenDevices[dev.ID] = true
	}

	var imported []models.DevicePlacement
	if err := db.Where("source = ?", models.DCIMSourceNetbox).Find(&imported).Error; err != nil {
		return err
	}
	for _, p := range imported {
		if seenDevices[p.DeviceID] {
			continue
		}
		if err := db.Delete(&p).Error; err != nil {
			return err
		}
	}
	reporter.Emit(jobevent.Info, "NetBox device placements: %d", len(seenDevices))
	return nil
}
