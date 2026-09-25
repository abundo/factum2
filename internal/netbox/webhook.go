package netbox

import (
	"errors"
	"strings"

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
			return 1, 0, 0, 0, nil
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
	return 0, 1, 0, 0, nil
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
// and marks optical paths that used it stale. No-op if none matches.
func DeleteConnectionByNetboxID(db *gorm.DB, netboxID uint) (int, error) {
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
	return created, updated, 0, err
}
