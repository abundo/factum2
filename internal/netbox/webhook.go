package netbox

import (
	"errors"
	"strings"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// SyncCable applies one Netbox cable to factum's Connection table: refetch
// by id, upsert if both ends resolve to synced interfaces, otherwise
// remove any local row for that netbox_id. Used by the webhook; full sync
// still goes through syncCables.
func SyncCable(db *gorm.DB, netboxID uint, reporter jobevent.Reporter) error {
	nb, err := netboxFromSettings(db)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	cable, err := nb.GetInterfaceCable(netboxID)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	created, updated, deleted, skipped, err := ApplyCable(db, netboxID, cable)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Netbox cable sync: %d new, %d updated, %d deleted, %d skipped (unresolved endpoint)",
		created, updated, deleted, skipped)
	if err := optical.RebuildStale(db); err != nil {
		reporter.Emit(jobevent.Warning, "optical retrace after cable webhook: %v", err)
	}
	return nil
}

// SyncSite applies one Netbox dcim.site to factum's Site table.
func SyncSite(db *gorm.DB, netboxID uint, reporter jobevent.Reporter) error {
	return SyncDCIMTreeItem(db, models.SiteNetboxKindSite, netboxID, reporter)
}

// SyncRegion applies one Netbox dcim.region to factum's Site table.
func SyncRegion(db *gorm.DB, netboxID uint, reporter jobevent.Reporter) error {
	return SyncDCIMTreeItem(db, models.SiteNetboxKindRegion, netboxID, reporter)
}

// SyncLocation applies one Netbox dcim.location to factum's Site table.
func SyncLocation(db *gorm.DB, netboxID uint, reporter jobevent.Reporter) error {
	return SyncDCIMTreeItem(db, models.SiteNetboxKindLocation, netboxID, reporter)
}

func netboxFromSettings(db *gorm.DB) (*netboxtool.NetboxClient, error) {
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		return nil, err
	}
	return netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   settings.NetboxApiURL,
		Token: settings.NetboxApiToken,
	})
}

// ApplyCable upserts or removes one Connection. cable == nil means the
// object is gone or not interface-to-interface — delete the local row.
// Unresolved endpoints also drop the local row (skip=1) rather than leave
// a stale Connection pointing at the old terminations.
func ApplyCable(db *gorm.DB, netboxID uint, cable *netboxtool.NBCable) (created, updated, deleted, skipped int, err error) {
	if cable == nil {
		n, err := DeleteConnectionByNetboxID(db, netboxID)
		return 0, 0, n, 0, err
	}

	var aIntf, bIntf models.Interface
	aErr := db.Select("id", "device_id", "netbox_id").Where("netbox_id = ?", cable.AInterface).First(&aIntf).Error
	bErr := db.Select("id", "device_id", "netbox_id").Where("netbox_id = ?", cable.BInterface).First(&bIntf).Error
	if errors.Is(aErr, gorm.ErrRecordNotFound) || errors.Is(bErr, gorm.ErrRecordNotFound) {
		n, err := DeleteConnectionByNetboxID(db, netboxID)
		return 0, 0, n, 1, err
	}
	if aErr != nil {
		return 0, 0, 0, 0, aErr
	}
	if bErr != nil {
		return 0, 0, 0, 0, bErr
	}

	var existing models.Connection
	lookupErr := db.Select("id").Where("netbox_id = ?", cable.NetboxID).First(&existing).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, 0, 0, lookupErr
	}
	isNew := errors.Is(lookupErr, gorm.ErrRecordNotFound)

	conn := models.Connection{
		NetboxID:     cable.NetboxID,
		DeviceAID:    aIntf.DeviceID,
		InterfaceAID: aIntf.ID,
		DeviceBID:    bIntf.DeviceID,
		InterfaceBID: bIntf.ID,
		Label:        cable.Label,
	}
	if err := deleteLocalCablesOnInterfaces(db, aIntf.ID, bIntf.ID); err != nil {
		return 0, 0, 0, 0, err
	}
	// NetBox allows one cable per interface. A cable that moved onto these
	// ports replaced whatever used to be there; drop that Connection and
	// its interface cable_id. NetBox does not change-log the interface
	// when a cable is deleted (the cable column is cleared with a queryset
	// update), so a device refetch never runs and device-sync would keep
	// reading the deleted id.
	n, err := deleteOtherCablesOnInterfaces(db, cable.NetboxID, aIntf.ID, bIntf.ID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	deleted += n
	if err := releaseCableID(db, cable.NetboxID); err != nil {
		return 0, 0, 0, 0, err
	}
	if err := assignCableID(db, aIntf.ID, cable.NetboxID); err != nil {
		return 0, 0, 0, 0, err
	}
	if err := assignCableID(db, bIntf.ID, cable.NetboxID); err != nil {
		return 0, 0, 0, 0, err
	}
	if isNew {
		if err := db.Create(&conn).Error; err != nil {
			if !isUniqueViolation(err) {
				return 0, 0, 0, 0, err
			}
			// The GUI save inserted this netbox_id first.
			var winner models.Connection
			if err := db.Select("id").Where("netbox_id = ?", cable.NetboxID).First(&winner).Error; err != nil {
				return 0, 0, 0, 0, err
			}
			existing = winner
		} else {
			return 1, 0, deleted, 0, nil
		}
	}
	if err := db.Model(&models.Connection{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"device_a_id":    conn.DeviceAID,
		"interface_a_id": conn.InterfaceAID,
		"device_b_id":    conn.DeviceBID,
		"interface_b_id": conn.InterfaceBID,
		"label":          conn.Label,
	}).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	return 0, 1, deleted, 0, nil
}

// deleteLocalCablesOnInterfaces drops Factum-only cables on these ports so a
// later NetBox cable can occupy them. NetBox-synced rows are left alone.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint") || strings.Contains(s, "duplicate key")
}

func deleteLocalCablesOnInterfaces(db *gorm.DB, ifaceIDs ...uint) error {
	if len(ifaceIDs) == 0 {
		return nil
	}
	return db.Where("netbox_id = 0 AND (interface_a_id IN ? OR interface_b_id IN ?)", ifaceIDs, ifaceIDs).
		Delete(&models.Connection{}).Error
}

// DeleteConnectionByNetboxID removes one Connection by its Netbox cable id
// and marks optical paths that used it stale. Interface cable_id values
// that still name it are cleared even when the Connection row is already
// gone, so a webhook that won the race does not leave device-sync pointing
// at a deleted cable. The returned count is still 1 only when a row was
// removed.
func DeleteConnectionByNetboxID(db *gorm.DB, netboxID uint) (int, error) {
	if err := releaseCableID(db, netboxID); err != nil {
		return 0, err
	}
	var conn models.Connection
	err := db.Where("netbox_id = ?", netboxID).First(&conn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	_ = optical.MarkStaleByConnection(db, conn.ID)
	if err := db.Delete(&models.Connection{}, conn.ID).Error; err != nil {
		return 0, err
	}
	return 1, nil
}

// releaseCableID clears interface.cable_id for one NetBox cable. A missing
// id is a no-op. UpdateColumn writes the zero.
func releaseCableID(db *gorm.DB, cableNetboxID uint) error {
	if cableNetboxID == 0 {
		return nil
	}
	return db.Model(&models.Interface{}).Where("cable_id = ?", cableNetboxID).UpdateColumn("cable_id", 0).Error
}

func assignCableID(db *gorm.DB, interfaceID, cableNetboxID uint) error {
	if interfaceID == 0 {
		return nil
	}
	return db.Model(&models.Interface{}).Where("id = ?", interfaceID).UpdateColumn("cable_id", cableNetboxID).Error
}

// deleteOtherCablesOnInterfaces removes NetBox-backed connections that
// still occupy these ports under a different cable id, and clears that
// id on every interface that carried it (both ends, not only this port).
func deleteOtherCablesOnInterfaces(db *gorm.DB, keepNetboxID uint, ifaceIDs ...uint) (int, error) {
	if len(ifaceIDs) == 0 {
		return 0, nil
	}
	var others []models.Connection
	err := db.Where("netbox_id <> 0 AND netbox_id <> ? AND (interface_a_id IN ? OR interface_b_id IN ?)", keepNetboxID, ifaceIDs, ifaceIDs).
		Find(&others).Error
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, other := range others {
		n, err := DeleteConnectionByNetboxID(db, other.NetboxID)
		if err != nil {
			return deleted, err
		}
		deleted += n
	}
	return deleted, nil
}

// ApplySite upserts or removes one dcim.site row. site == nil means gone or
// Default — delete the local synced row. Missing coordinates no longer
// delete the row; the org tree keeps unplotted sites. Parent is left
// unchanged so a map pin does not strip a synced region parent.
func ApplySite(db *gorm.DB, netboxID uint, site *netboxtool.NetboxSite) (created, updated, deleted int, err error) {
	if site == nil {
		n, err := DeleteSiteByNetboxID(db, netboxID)
		return 0, 0, n, err
	}
	item := dcimTreeItem{
		Kind: models.SiteNetboxKindSite,
		ID:   site.ID,
		Name: site.Name,
		Slug: models.Slugify(site.Name),
	}
	if site.Latitude != nil {
		item.Latitude = float64(*site.Latitude)
	}
	if site.Longitude != nil {
		item.Longitude = float64(*site.Longitude)
	}
	created, updated, err = upsertNetboxNode(db, item, false)
	if err != nil {
		return created, updated, 0, err
	}
	if err := cfgmgmt.Seed(db); err != nil {
		return created, updated, 0, err
	}
	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		return created, updated, 0, err
	}
	if _, _, err := upsertConfigNode(db, root.ID, item, false); err != nil {
		return created, updated, 0, err
	}
	if err := placeSyncedDevices(db, devicePlaceFilter{
		netboxSiteIDs: map[uint]struct{}{netboxID: {}},
	}, nil); err != nil {
		return created, updated, 0, err
	}
	return created, updated, 0, nil
}
