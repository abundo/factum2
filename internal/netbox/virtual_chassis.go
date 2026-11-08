package netbox

import (
	"errors"
	"strconv"

	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// assignVirtualChassis upserts the NetBox virtual chassis on nb and sets
// the Factum membership columns on device. A device with no chassis is
// cleared. The chassis master (shelf controller) is resolved later, after
// every device in the batch exists.
func assignVirtualChassis(db *gorm.DB, device *models.Device, nb *netboxtool.NBDevice) error {
	if nb.VM || nb.VirtualChassisID == 0 {
		device.VirtualChassisID = 0
		device.VCPosition = 0
		device.VCPriority = 0
		return nil
	}
	id, err := upsertVirtualChassis(db, nb)
	if err != nil {
		return err
	}
	device.VirtualChassisID = id
	device.VCPosition = nb.VCPosition
	device.VCPriority = nb.VCPriority
	return nil
}

func upsertVirtualChassis(db *gorm.DB, nb *netboxtool.NBDevice) (uint, error) {
	row := models.VirtualChassis{
		NetboxID:       nb.VirtualChassisID,
		Name:           nb.VirtualChassisName,
		Domain:         nb.VirtualChassisDomain,
		MasterNetboxID: nb.VirtualChassisMasterID,
	}
	err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "netbox_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "domain", "master_netbox_id", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return 0, err
	}
	if row.ID != 0 {
		return row.ID, nil
	}
	var existing models.VirtualChassis
	if err := db.Select("id").Where("netbox_id = ?", nb.VirtualChassisID).Take(&existing).Error; err != nil {
		return 0, err
	}
	return existing.ID, nil
}

// finishVirtualChassis drops chassis that no longer have a member and
// points each chassis at its shelf-controller device.
func finishVirtualChassis(db *gorm.DB) error {
	if err := deleteUnreferencedVirtualChassis(db); err != nil {
		return err
	}
	return resolveVirtualChassisMasters(db)
}

func deleteUnreferencedVirtualChassis(db *gorm.DB) error {
	var referenced []uint
	if err := db.Model(&models.Device{}).
		Where("virtual_chassis_id != 0").
		Pluck("virtual_chassis_id", &referenced).Error; err != nil {
		return err
	}
	referenced = uniqueUints(referenced)
	q := db.Where("netbox_id != 0")
	if len(referenced) > 0 {
		q = q.Where("id NOT IN ?", referenced)
	}
	return q.Delete(&models.VirtualChassis{}).Error
}

func resolveVirtualChassisMasters(db *gorm.DB) error {
	var rows []models.VirtualChassis
	if err := db.Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var want uint
		if row.MasterNetboxID != 0 {
			var master models.Device
			err := db.Select("id").
				Where("netbox_id = ? AND vm = ?", row.MasterNetboxID, false).
				Take(&master).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				want = master.ID
			}
		}
		if want == row.MasterDeviceID {
			continue
		}
		if err := db.Model(&models.VirtualChassis{}).Where("id = ?", row.ID).Update("master_device_id", want).Error; err != nil {
			return err
		}
	}
	return nil
}

// deleteVirtualChassisByNetboxID clears members and removes the chassis.
// A missing row is not an error.
func deleteVirtualChassisByNetboxID(db *gorm.DB, netboxID uint) error {
	if netboxID == 0 {
		return nil
	}
	var row models.VirtualChassis
	err := db.Where("netbox_id = ?", netboxID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := db.Model(&models.Device{}).Where("virtual_chassis_id = ?", row.ID).Updates(map[string]any{
		"virtual_chassis_id": 0,
		"vc_position":        0,
		"vc_priority":        0,
	}).Error; err != nil {
		return err
	}
	return db.Delete(&models.VirtualChassis{}, row.ID).Error
}

// deviceIDsInVirtualChassis lists NetBox device ids that belong to the
// chassis. An unknown chassis is an empty list.
func deviceIDsInVirtualChassis(nb *netboxtool.NetboxClient, id uint) ([]uint, error) {
	type row struct {
		ID uint `json:"id"`
	}
	rows, err := restListAll[row](nb, "/api/dcim/devices/?virtual_chassis_id="+strconv.FormatUint(uint64(id), 10))
	if err != nil {
		return nil, err
	}
	out := make([]uint, 0, len(rows))
	for _, row := range rows {
		if row.ID != 0 {
			out = append(out, row.ID)
		}
	}
	return out, nil
}

// expandVirtualChassis turns a chassis create/update into a refetch of
// every member. NetBox records a rename on the chassis, not on the
// devices. Members that left the chassis are included from the local
// rows so their membership is cleared. A delete is applied after the
// device pass.
func (p *deltaPlan) expandVirtualChassis(db *gorm.DB, nb *netboxtool.NetboxClient) error {
	for id, action := range p.virtualChassis {
		if action == "delete" || id == 0 {
			continue
		}
		members, err := deviceIDsInVirtualChassis(nb, id)
		if err != nil {
			return err
		}
		local, err := localVirtualChassisMemberNetboxIDs(db, id)
		if err != nil {
			return err
		}
		for _, devID := range append(members, local...) {
			p.noteDevice(devID, false, "update")
		}
	}
	return nil
}

func uniqueUints(ids []uint) []uint {
	if len(ids) == 0 {
		return ids
	}
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func localVirtualChassisMemberNetboxIDs(db *gorm.DB, vcNetboxID uint) ([]uint, error) {
	var ids []uint
	err := db.Model(&models.Device{}).
		Joins("JOIN virtual_chassis ON virtual_chassis.id = devices.virtual_chassis_id").
		Where("virtual_chassis.netbox_id = ? AND devices.netbox_id != 0 AND devices.vm = ?", vcNetboxID, false).
		Pluck("devices.netbox_id", &ids).Error
	return ids, err
}
