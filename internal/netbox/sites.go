package netbox

import (
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

// nbNestedID is the nested id object NetBox REST returns for parent/region/site.
type nbNestedID struct {
	ID uint `json:"id"`
}

type nbRegionREST struct {
	ID     uint        `json:"id"`
	Name   string      `json:"name"`
	Slug   string      `json:"slug"`
	Parent *nbNestedID `json:"parent"`
}

type nbSiteListREST struct {
	ID        uint        `json:"id"`
	Name      string      `json:"name"`
	Slug      string      `json:"slug"`
	Region    *nbNestedID `json:"region"`
	Latitude  *float64    `json:"latitude"`
	Longitude *float64    `json:"longitude"`
}

type nbLocationREST struct {
	ID     uint        `json:"id"`
	Name   string      `json:"name"`
	Slug   string      `json:"slug"`
	Site   nbNestedID  `json:"site"`
	Parent *nbNestedID `json:"parent"`
}

type siteKey struct {
	kind string
	id   uint
}

// dcimTreeItem is one NetBox region/site/location to upsert as models.Site.
type dcimTreeItem struct {
	Kind       string
	ID         uint
	Name       string
	Slug       string
	ParentKind string
	ParentID   uint
	Latitude   float64
	Longitude  float64
}

func (it dcimTreeItem) key() siteKey { return siteKey{it.Kind, it.ID} }

func restGetOptional[T any](nb *netboxtool.NetboxClient, endpoint string) (*T, error) {
	var row T
	err := nb.RestGet(endpoint, &row)
	if err != nil {
		if strings.Contains(err.Error(), "404 Not Found") {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func isDefaultSiteName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "Default")
}

func listDCIMTree(nb *netboxtool.NetboxClient) ([]dcimTreeItem, error) {
	regions, err := restListAll[nbRegionREST](nb, "/api/dcim/regions/")
	if err != nil {
		return nil, err
	}
	sites, err := restListAll[nbSiteListREST](nb, "/api/dcim/sites/")
	if err != nil {
		return nil, err
	}
	locations, err := restListAll[nbLocationREST](nb, "/api/dcim/locations/")
	if err != nil {
		return nil, err
	}

	out := make([]dcimTreeItem, 0, len(regions)+len(sites)+len(locations))
	for _, r := range regions {
		item := dcimTreeItem{
			Kind: models.SiteNetboxKindRegion,
			ID:   r.ID,
			Name: r.Name,
			Slug: r.Slug,
		}
		if r.Parent != nil && r.Parent.ID != 0 {
			item.ParentKind = models.SiteNetboxKindRegion
			item.ParentID = r.Parent.ID
		}
		out = append(out, item)
	}
	for _, s := range sites {
		if isDefaultSiteName(s.Name) {
			continue
		}
		item := dcimTreeItem{
			Kind: models.SiteNetboxKindSite,
			ID:   s.ID,
			Name: s.Name,
			Slug: s.Slug,
		}
		if s.Latitude != nil {
			item.Latitude = *s.Latitude
		}
		if s.Longitude != nil {
			item.Longitude = *s.Longitude
		}
		if s.Region != nil && s.Region.ID != 0 {
			item.ParentKind = models.SiteNetboxKindRegion
			item.ParentID = s.Region.ID
		}
		out = append(out, item)
	}
	for _, loc := range locations {
		item := dcimTreeItem{
			Kind: models.SiteNetboxKindLocation,
			ID:   loc.ID,
			Name: loc.Name,
			Slug: loc.Slug,
		}
		if loc.Parent != nil && loc.Parent.ID != 0 {
			item.ParentKind = models.SiteNetboxKindLocation
			item.ParentID = loc.Parent.ID
		} else if loc.Site.ID != 0 {
			item.ParentKind = models.SiteNetboxKindSite
			item.ParentID = loc.Site.ID
		}
		out = append(out, item)
	}
	return out, nil
}

func fetchDCIMTreeItem(nb *netboxtool.NetboxClient, kind string, id uint) (*dcimTreeItem, error) {
	switch kind {
	case models.SiteNetboxKindRegion:
		row, err := restGetOptional[nbRegionREST](nb, fmt.Sprintf("/api/dcim/regions/%d/", id))
		if err != nil || row == nil {
			return nil, err
		}
		item := dcimTreeItem{Kind: kind, ID: row.ID, Name: row.Name, Slug: row.Slug}
		if row.Parent != nil && row.Parent.ID != 0 {
			item.ParentKind = models.SiteNetboxKindRegion
			item.ParentID = row.Parent.ID
		}
		return &item, nil
	case models.SiteNetboxKindSite:
		row, err := restGetOptional[nbSiteListREST](nb, fmt.Sprintf("/api/dcim/sites/%d/", id))
		if err != nil || row == nil {
			return nil, err
		}
		if isDefaultSiteName(row.Name) {
			return nil, nil
		}
		item := dcimTreeItem{Kind: kind, ID: row.ID, Name: row.Name, Slug: row.Slug}
		if row.Latitude != nil {
			item.Latitude = *row.Latitude
		}
		if row.Longitude != nil {
			item.Longitude = *row.Longitude
		}
		if row.Region != nil && row.Region.ID != 0 {
			item.ParentKind = models.SiteNetboxKindRegion
			item.ParentID = row.Region.ID
		}
		return &item, nil
	case models.SiteNetboxKindLocation:
		row, err := restGetOptional[nbLocationREST](nb, fmt.Sprintf("/api/dcim/locations/%d/", id))
		if err != nil || row == nil {
			return nil, err
		}
		item := dcimTreeItem{Kind: kind, ID: row.ID, Name: row.Name, Slug: row.Slug}
		if row.Parent != nil && row.Parent.ID != 0 {
			item.ParentKind = models.SiteNetboxKindLocation
			item.ParentID = row.Parent.ID
		} else if row.Site.ID != 0 {
			item.ParentKind = models.SiteNetboxKindSite
			item.ParentID = row.Site.ID
		}
		return &item, nil
	default:
		return nil, fmt.Errorf("unknown netbox site kind %q", kind)
	}
}

func lookupSiteID(db *gorm.DB, kind string, netboxID uint) (*uint, error) {
	if kind == "" || netboxID == 0 {
		return nil, nil
	}
	var row models.Site
	err := db.Select("id").Where("netbox_kind = ? AND netbox_id = ?", kind, netboxID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row.ID, nil
}

func upsertNetboxNode(db *gorm.DB, item dcimTreeItem, setParent bool) (created, updated int, err error) {
	var existing models.Site
	lookupErr := db.Where("netbox_kind = ? AND netbox_id = ?", item.Kind, item.ID).First(&existing).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, lookupErr
	}
	isNew := errors.Is(lookupErr, gorm.ErrRecordNotFound)

	var parentID *uint
	if setParent && item.ParentID != 0 {
		parentID, err = lookupSiteID(db, item.ParentKind, item.ParentID)
		if err != nil {
			return 0, 0, err
		}
	}

	if isNew {
		row := models.Site{
			Name:       item.Name,
			Slug:       item.Slug,
			Source:     models.SiteSourceNetbox,
			NetboxKind: item.Kind,
			NetboxID:   item.ID,
			Latitude:   item.Latitude,
			Longitude:  item.Longitude,
		}
		if setParent {
			row.ParentID = parentID
		}
		if err := db.Create(&row).Error; err != nil {
			return 0, 0, err
		}
		return 1, 0, nil
	}

	changes := map[string]any{
		"name":        item.Name,
		"slug":        item.Slug,
		"source":      models.SiteSourceNetbox,
		"netbox_kind": item.Kind,
		"latitude":    item.Latitude,
		"longitude":   item.Longitude,
	}
	if setParent {
		if parentID == nil {
			changes["parent_id"] = nil
		} else {
			changes["parent_id"] = *parentID
		}
	}
	if err := db.Model(&existing).Updates(changes).Error; err != nil {
		return 0, 0, err
	}
	return 0, 1, nil
}

// deleteNetboxNode removes one synced Site by (kind, netbox id) and reparents
// its children onto the deleted node's parent so Factum-created descendants
// are not dropped. No-op if none matches.
func deleteNetboxNode(db *gorm.DB, kind string, netboxID uint) (int, error) {
	var row models.Site
	err := db.Where("netbox_kind = ? AND netbox_id = ?", kind, netboxID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if err := db.Model(&models.Site{}).Where("parent_id = ?", row.ID).Update("parent_id", row.ParentID).Error; err != nil {
		return 0, err
	}
	if err := db.Delete(&row).Error; err != nil {
		return 0, err
	}
	return 1, nil
}

// DeleteSiteByNetboxID removes the synced dcim.site row for netboxID.
func DeleteSiteByNetboxID(db *gorm.DB, netboxID uint) (int, error) {
	return deleteNetboxNode(db, models.SiteNetboxKindSite, netboxID)
}

// DeleteSyncedSiteNode removes the synced row for a region, site, or location.
func DeleteSyncedSiteNode(db *gorm.DB, kind string, netboxID uint) (int, error) {
	return deleteNetboxNode(db, kind, netboxID)
}

// syncSites mirrors NetBox regions, sites and locations into models.Site as
// one parented tree. Factum-created rows (source=factum) are never deleted.
// An empty combined fetch does not wipe local synced rows.
func syncSites(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	items, err := listDCIMTree(nb)
	if err != nil {
		return err
	}

	seen := make(map[siteKey]bool, len(items))
	var countNew, countUpdated int
	// Insert/update names first so a child's parent row exists, then set parent_id.
	for _, item := range items {
		created, updated, err := upsertNetboxNode(db, item, false)
		if err != nil {
			return err
		}
		seen[item.key()] = true
		countNew += created
		countUpdated += updated
	}
	for _, item := range items {
		if _, _, err := upsertNetboxNode(db, item, true); err != nil {
			return err
		}
	}

	var existing []models.Site
	if err := db.Select("id", "netbox_kind", "netbox_id", "source").
		Where("source = ?", models.SiteSourceNetbox).
		Find(&existing).Error; err != nil {
		return err
	}

	var countDeleted int
	if len(items) > 0 {
		for _, row := range existing {
			if seen[siteKey{row.NetboxKind, row.NetboxID}] {
				continue
			}
			n, err := deleteNetboxNode(db, row.NetboxKind, row.NetboxID)
			if err != nil {
				return err
			}
			countDeleted += n
		}
	}

	reporter.Emit(jobevent.Info, "Netbox site sync: %d new, %d updated, %d deleted",
		countNew, countUpdated, countDeleted)
	return nil
}

// SyncDCIMTreeItem applies one NetBox region/site/location. A missing or
// Default object deletes the matching local synced row.
func SyncDCIMTreeItem(db *gorm.DB, kind string, netboxID uint, reporter jobevent.Reporter) error {
	nb, err := netboxFromSettings(db)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	item, err := fetchDCIMTreeItem(nb, kind, netboxID)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	var created, updated, deleted int
	if item == nil {
		deleted, err = deleteNetboxNode(db, kind, netboxID)
	} else {
		created, updated, err = upsertNetboxNode(db, *item, true)
	}
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Netbox site sync: %d new, %d updated, %d deleted", created, updated, deleted)
	return nil
}
