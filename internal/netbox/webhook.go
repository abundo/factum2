package netbox

import (
	"errors"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// SyncSite applies one Netbox site to factum's Site table. A missing,
// Default, or uncoordinated site deletes any local row for that id.
func SyncSite(db *gorm.DB, netboxID uint, reporter jobevent.Reporter) error {
	nb, err := netboxFromSettings(db)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	site, err := nb.GetSite(netboxID)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	created, updated, deleted, err := ApplySite(db, netboxID, site)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Netbox site sync: %d new, %d updated, %d deleted", created, updated, deleted)
	return nil
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
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "netbox_id"}},
		UpdateAll: true,
	}).Create(&conn).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	if isNew {
		return 1, 0, 0, 0, nil
	}
	return 0, 1, 0, 0, nil
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

// ApplySite upserts or removes one Site. site == nil means gone / Default /
// no coordinates — delete the local row.
func ApplySite(db *gorm.DB, netboxID uint, site *netboxtool.NetboxSite) (created, updated, deleted int, err error) {
	if site == nil || site.Latitude == nil || site.Longitude == nil {
		n, err := DeleteSiteByNetboxID(db, netboxID)
		return 0, 0, n, err
	}

	var existing models.Site
	lookupErr := db.Select("id").Where("netbox_id = ?", site.ID).First(&existing).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, 0, lookupErr
	}
	isNew := errors.Is(lookupErr, gorm.ErrRecordNotFound)

	row := models.Site{
		NetboxID:  site.ID,
		Name:      site.Name,
		Latitude:  float64(*site.Latitude),
		Longitude: float64(*site.Longitude),
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "netbox_id"}},
		UpdateAll: true,
	}).Create(&row).Error; err != nil {
		return 0, 0, 0, err
	}
	if isNew {
		return 1, 0, 0, nil
	}
	return 0, 1, 0, nil
}

// DeleteSiteByNetboxID removes one Site by its Netbox id. No-op if none matches.
func DeleteSiteByNetboxID(db *gorm.DB, netboxID uint) (int, error) {
	result := db.Where("netbox_id = ?", netboxID).Delete(&models.Site{})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}
