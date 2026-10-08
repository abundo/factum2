package netbox

import (
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// devicePlaceFilter chooses which devices to attach in the config tree.
// all places every device. Otherwise a device is placed when its factum id
// is listed or it is a NetBox device whose site id is listed.
type devicePlaceFilter struct {
	all           bool
	factumIDs     map[uint]struct{}
	netboxSiteIDs map[uint]struct{}
}

func scopeKindForNetbox(kind string) (string, error) {
	switch kind {
	case models.SiteNetboxKindRegion:
		return models.ConfigScopeKindRegion, nil
	case models.SiteNetboxKindSite:
		return models.ConfigScopeKindSite, nil
	case models.SiteNetboxKindLocation:
		return models.ConfigScopeKindLocation, nil
	default:
		return "", fmt.Errorf("unknown netbox site kind %q", kind)
	}
}

func syncedNetboxKind(kind string) bool {
	switch kind {
	case models.SiteNetboxKindRegion, models.SiteNetboxKindSite, models.SiteNetboxKindLocation:
		return true
	}
	return false
}

// syncConfigTree mirrors NetBox regions, sites, and locations into the
// config tree under global. Operator-created scopes are left in place.
// An empty fetch does not delete previously mirrored nodes.
func syncConfigTree(db *gorm.DB, items []dcimTreeItem, reporter jobevent.Reporter) error {
	if err := cfgmgmt.Seed(db); err != nil {
		return err
	}
	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		return err
	}

	seen := make(map[siteKey]bool, len(items))
	var created, updated int
	for _, item := range items {
		n, u, err := upsertConfigNode(db, root.ID, item, false)
		if err != nil {
			return err
		}
		seen[item.key()] = true
		created += n
		updated += u
	}
	for _, item := range items {
		if _, _, err := upsertConfigNode(db, root.ID, item, true); err != nil {
			return err
		}
	}

	var existing []models.ConfigScope
	if err := db.Where("netbox_id != 0").Find(&existing).Error; err != nil {
		return err
	}
	var deleted int
	if len(items) > 0 {
		for _, row := range existing {
			if !syncedNetboxKind(row.NetboxKind) || seen[siteKey{row.NetboxKind, row.NetboxID}] {
				continue
			}
			n, err := deleteConfigNode(db, row.NetboxKind, row.NetboxID)
			if err != nil {
				return err
			}
			deleted += n
		}
	}
	if reporter != nil {
		reporter.Emit(jobevent.Info, "Netbox config tree: %d new, %d updated, %d deleted", created, updated, deleted)
	}
	return nil
}

func lookupConfigScopeID(db *gorm.DB, kind string, netboxID uint) (*uint, error) {
	if kind == "" || netboxID == 0 {
		return nil, nil
	}
	var row models.ConfigScope
	err := db.Select("id").Where("netbox_kind = ? AND netbox_id = ?", kind, netboxID).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row.ID, nil
}

func linkedFactumSiteID(db *gorm.DB, kind string, netboxID uint) *uint {
	var row models.Site
	err := db.Select("id").Where("netbox_kind = ? AND netbox_id = ?", kind, netboxID).Take(&row).Error
	if err != nil {
		return nil
	}
	return &row.ID
}

func configNodeName(item dcimTreeItem) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		return fmt.Sprintf("%s-%d", item.Kind, item.ID)
	}
	return name
}

// upsertConfigNode writes one mirrored scope. setParent false leaves an
// existing parent alone (a new node hangs off global). setParent true
// parents the node under its NetBox parent, or global when that parent
// is not in the tree yet.
func upsertConfigNode(db *gorm.DB, rootID uint, item dcimTreeItem, setParent bool) (created, updated int, err error) {
	kind, err := scopeKindForNetbox(item.Kind)
	if err != nil {
		return 0, 0, err
	}
	var existing models.ConfigScope
	lookupErr := db.Where("netbox_kind = ? AND netbox_id = ?", item.Kind, item.ID).Take(&existing).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, lookupErr
	}
	isNew := errors.Is(lookupErr, gorm.ErrRecordNotFound)

	parentID := rootID
	if setParent && item.ParentID != 0 {
		found, err := lookupConfigScopeID(db, item.ParentKind, item.ParentID)
		if err != nil {
			return 0, 0, err
		}
		if found != nil {
			parentID = *found
		}
	} else if !isNew && existing.ParentID != nil {
		parentID = *existing.ParentID
	}
	if !isNew && parentID == existing.ID {
		parentID = rootID
	}

	siteID := linkedFactumSiteID(db, item.Kind, item.ID)
	name := configNodeName(item)
	if isNew {
		row := models.ConfigScope{
			ParentID:   &parentID,
			Name:       name,
			Kind:       kind,
			SiteID:     siteID,
			Enabled:    true,
			NetboxKind: item.Kind,
			NetboxID:   item.ID,
		}
		if err := db.Create(&row).Error; err != nil {
			return 0, 0, err
		}
		return 1, 0, nil
	}

	changes := map[string]any{
		"name":        name,
		"kind":        kind,
		"netbox_kind": item.Kind,
		"site_id":     siteID,
	}
	if setParent || existing.ParentID == nil {
		changes["parent_id"] = parentID
	}
	if err := db.Model(&existing).Updates(changes).Error; err != nil {
		return 0, 0, err
	}
	return 0, 1, nil
}

// deleteConfigNode removes one mirrored scope and reparents its children
// onto the deleted node's parent. No-op when none matches.
func deleteConfigNode(db *gorm.DB, kind string, netboxID uint) (int, error) {
	var row models.ConfigScope
	err := db.Where("netbox_kind = ? AND netbox_id = ?", kind, netboxID).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	parent := row.ParentID
	if err := db.Model(&models.ConfigScope{}).Where("parent_id = ?", row.ID).Update("parent_id", parent).Error; err != nil {
		return 0, err
	}
	if err := db.Delete(&row).Error; err != nil {
		return 0, err
	}
	return 1, nil
}

// placeSyncedDevices attaches devices in the config tree. A NetBox device
// hangs under the scope for its NetBox site, or under global when that
// site is not in the tree. A Factum device hangs under a scope linked to
// its Organization site. A Factum device that is already placed, and has
// no such site, is left where the operator put it. Anything not yet in
// the tree is placed under global.
func placeSyncedDevices(db *gorm.DB, f devicePlaceFilter, reporter jobevent.Reporter) error {
	if !f.all && len(f.factumIDs) == 0 && len(f.netboxSiteIDs) == 0 {
		return nil
	}
	if err := cfgmgmt.Seed(db); err != nil {
		return err
	}
	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		return err
	}
	var siteScopes []models.ConfigScope
	if err := db.Where("kind = ? AND netbox_kind = ? AND netbox_id != 0", models.ConfigScopeKindSite, models.SiteNetboxKindSite).
		Find(&siteScopes).Error; err != nil {
		return err
	}
	byNetboxSite := make(map[uint]uint, len(siteScopes))
	for _, s := range siteScopes {
		byNetboxSite[s.NetboxID] = s.ID
	}

	var devices []models.Device
	q := db.Model(&models.Device{})
	if !f.all {
		ids := mapKeys(f.factumIDs)
		sites := mapKeys(f.netboxSiteIDs)
		switch {
		case len(ids) > 0 && len(sites) > 0:
			q = q.Where("id IN ? OR (netbox_id != 0 AND site_id IN ?)", ids, sites)
		case len(ids) > 0:
			q = q.Where("id IN ?", ids)
		default:
			q = q.Where("netbox_id != 0 AND site_id IN ?", sites)
		}
	}
	if err := q.Find(&devices).Error; err != nil {
		return err
	}

	var placed int
	for _, device := range devices {
		parent, ok, err := configParentForDevice(db, device, root.ID, byNetboxSite)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, err := cfgmgmt.AttachDevice(db, parent, device.ID); err != nil {
			return fmt.Errorf("config tree device %s: %w", device.Name, err)
		}
		placed++
	}
	if reporter != nil {
		reporter.Emit(jobevent.Info, "Netbox config tree: %d devices placed", placed)
	}
	return nil
}

func mapKeys(m map[uint]struct{}) []uint {
	if len(m) == 0 {
		return nil
	}
	out := make([]uint, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

// configParentForDevice returns the scope the device should hang under.
// ok false means leave an existing placement alone.
func configParentForDevice(db *gorm.DB, device models.Device, rootID uint, byNetboxSite map[uint]uint) (uint, bool, error) {
	var existing models.ConfigScope
	err := db.Where("kind = ? AND device_id = ?", models.ConfigScopeKindDevice, device.ID).Take(&existing).Error
	has := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}

	if device.NetboxID != 0 {
		if device.SiteID != 0 {
			if id, ok := byNetboxSite[device.SiteID]; ok {
				return id, true, nil
			}
		}
		return rootID, true, nil
	}

	if device.SiteID != 0 {
		var linked models.ConfigScope
		err := db.Where("site_id = ? AND kind = ? AND netbox_id != 0", device.SiteID, models.ConfigScopeKindSite).Take(&linked).Error
		if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
			err = db.Where("site_id = ? AND netbox_id != 0", device.SiteID).Take(&linked).Error
		}
		if err == nil {
			return linked.ID, true, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, err
		}
	}
	if has {
		return 0, false, nil
	}
	return rootID, true, nil
}

func detachConfigDevice(db *gorm.DB, deviceID uint) error {
	var scope models.ConfigScope
	err := db.Where("kind = ? AND device_id = ?", models.ConfigScopeKindDevice, deviceID).Take(&scope).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return cfgmgmt.DetachDevice(db, scope.ID)
}
