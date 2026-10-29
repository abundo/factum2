package netbox

import (
	"errors"
	"strings"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// nbRouteTargetRef is the nested route-target object NetBox returns on a VRF.
type nbRouteTargetRef struct {
	Name string `json:"name"`
}

type nbVRFREST struct {
	ID            uint               `json:"id"`
	Name          string             `json:"name"`
	RD            *string            `json:"rd"`
	Description   string             `json:"description"`
	EnforceUnique *bool              `json:"enforce_unique"`
	ImportTargets []nbRouteTargetRef `json:"import_targets"`
	ExportTargets []nbRouteTargetRef `json:"export_targets"`
}

// uniqueIPSpace reports NetBox Unique IP Space. A missing field is the
// NetBox default, which is on.
func (v nbVRFREST) uniqueIPSpace() bool {
	if v.EnforceUnique == nil {
		return true
	}
	return *v.EnforceUnique
}

func joinRouteTargetNames(targets []nbRouteTargetRef) string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		n := strings.TrimSpace(t.Name)
		if n == "" {
			continue
		}
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func rdString(rd *string) string {
	if rd == nil {
		return ""
	}
	return strings.TrimSpace(*rd)
}

// netboxNamespaceName is the single Factum namespace that holds every
// VRF and prefix synced from NetBox.
const netboxNamespaceName = "Netbox"

// syncVRFs mirrors NetBox ipam.VRF rows into the Netbox namespace, so
// prefixes may overlap the global table and each other. Factum-created
// VRFs are never deleted. An empty fetch does not wipe already-synced rows.
func syncVRFs(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbVRFREST](nb, "/api/ipam/vrfs/")
	if err != nil {
		return err
	}

	if _, err := netboxNamespaceID(db); err != nil {
		return err
	}

	seen := make(map[uint]bool, len(rows))
	var countNew, countUpdated, countSkipped int
	for _, row := range rows {
		created, updated, skipped, err := upsertNetboxVRF(db, row, reporter)
		if err != nil {
			return err
		}
		if row.ID != 0 {
			seen[row.ID] = true
		}
		countNew += created
		countUpdated += updated
		countSkipped += skipped
	}

	var existing []models.IpamVRF
	if err := db.Where("source = ? AND netbox_id != 0", models.VRFSourceNetbox).Find(&existing).Error; err != nil {
		return err
	}

	var countDeleted int
	if len(rows) > 0 {
		for _, row := range existing {
			if seen[row.NetboxID] {
				continue
			}
			n, err := deleteNetboxVRF(db, row, reporter)
			if err != nil {
				return err
			}
			countDeleted += n
		}
	}

	reporter.Emit(jobevent.Info, "Netbox VRF sync: %d new, %d updated, %d deleted, %d skipped",
		countNew, countUpdated, countDeleted, countSkipped)
	return nil
}

func upsertNetboxVRF(db *gorm.DB, row nbVRFREST, reporter jobevent.Reporter) (created, updated, skipped int, err error) {
	name := strings.TrimSpace(row.Name)
	if name == "" {
		reporter.Emit(jobevent.Warning, "Netbox VRF sync: skipping unnamed VRF (netbox_id=%d)", row.ID)
		return 0, 0, 1, nil
	}
	if strings.EqualFold(name, "default") {
		reporter.Emit(jobevent.Warning, "Netbox VRF sync: skipping %q (netbox_id=%d); name collides with the default VRF", name, row.ID)
		return 0, 0, 1, nil
	}

	rd := rdString(row.RD)
	imp := joinRouteTargetNames(row.ImportTargets)
	exp := joinRouteTargetNames(row.ExportTargets)
	desc := strings.TrimSpace(row.Description)
	unique := row.uniqueIPSpace()

	nsID, err := netboxNamespaceID(db)
	if err != nil {
		return 0, 0, 0, err
	}

	var existing models.IpamVRF
	lookupErr := db.Where("netbox_id = ? AND netbox_id != 0", row.ID).First(&existing).Error
	if lookupErr == nil {
		if existing.Name != name {
			var clash models.IpamVRF
			clashErr := db.Where("name = ? AND NOT is_default AND id <> ?", name, existing.ID).First(&clash).Error
			if clashErr == nil {
				reporter.Emit(jobevent.Warning, "Netbox VRF sync: skipping rename of %q → %q (netbox_id=%d); name already used", existing.Name, name, row.ID)
				return 0, 0, 1, nil
			}
			if !errors.Is(clashErr, gorm.ErrRecordNotFound) {
				return 0, 0, 0, clashErr
			}
		}
		oldNS := existing.NamespaceID
		if nsID != oldNS {
			if err := db.Model(&models.IpamPrefix{}).Where("vrf_id = ?", existing.ID).Update("namespace_id", nsID).Error; err != nil {
				return 0, 0, 0, err
			}
		}
		changes := map[string]any{
			"namespace_id":   nsID,
			"name":           name,
			"description":    desc,
			"rd":             rd,
			"import_rt":      imp,
			"export_rt":      exp,
			"enforce_unique": unique,
			"source":         models.VRFSourceNetbox,
		}
		if err := db.Model(&existing).Updates(changes).Error; err != nil {
			return 0, 0, 0, err
		}
		if nsID != oldNS {
			if err := deleteNamespaceIfUnused(db, oldNS); err != nil {
				return 0, 0, 0, err
			}
		}
		return 0, 1, 0, nil
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, 0, lookupErr
	}

	var clash models.IpamVRF
	clashErr := db.Where("name = ? AND NOT is_default", name).First(&clash).Error
	if clashErr == nil {
		reporter.Emit(jobevent.Warning, "Netbox VRF sync: skipping %q (netbox_id=%d); a Factum VRF already has that name", name, row.ID)
		return 0, 0, 1, nil
	}
	if !errors.Is(clashErr, gorm.ErrRecordNotFound) {
		return 0, 0, 0, clashErr
	}

	createdRow := models.IpamVRF{
		NamespaceID:   nsID,
		Name:          name,
		Description:   desc,
		RD:            rd,
		ImportRT:      imp,
		ExportRT:      exp,
		EnforceUnique: unique,
		Source:        models.VRFSourceNetbox,
		NetboxID:      row.ID,
	}
	if err := db.Create(&createdRow).Error; err != nil {
		return 0, 0, 0, err
	}
	return 1, 0, 0, nil
}

// netboxNamespaceID returns the namespace that holds every NetBox VRF and
// prefix, creating it when this is the first sync.
func netboxNamespaceID(db *gorm.DB) (uint, error) {
	var ns models.IpamNamespace
	err := db.Where("name = ?", netboxNamespaceName).First(&ns).Error
	if err == nil {
		return ns.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	view, err := ipam.CreateNamespace(db, netboxNamespaceName, "Synced from NetBox")
	if err == nil {
		return view.ID, nil
	}
	var status *ipam.StatusError
	if errors.As(err, &status) && status.Status == 409 {
		if err := db.Where("name = ?", netboxNamespaceName).First(&ns).Error; err != nil {
			return 0, err
		}
		return ns.ID, nil
	}
	return 0, err
}

func deleteNetboxVRF(db *gorm.DB, row models.IpamVRF, reporter jobevent.Reporter) (int, error) {
	var n int64
	if err := db.Model(&models.IpamPrefix{}).Where("vrf_id = ?", row.ID).Count(&n).Error; err != nil {
		return 0, err
	}
	if n > 0 {
		reporter.Emit(jobevent.Warning, "Netbox VRF sync: keeping %q (netbox_id=%d); still has allocated prefixes but is missing from NetBox", row.Name, row.NetboxID)
		return 0, nil
	}
	nsID := row.NamespaceID
	if err := db.Delete(&row).Error; err != nil {
		return 0, err
	}
	if err := deleteNamespaceIfUnused(db, nsID); err != nil {
		return 0, err
	}
	return 1, nil
}

// deleteNamespaceIfUnused removes a named namespace that only still has its
// default VRF. The empty namespace is the global table and is kept.
func deleteNamespaceIfUnused(db *gorm.DB, nsID uint) error {
	if nsID == 0 {
		return nil
	}
	var ns models.IpamNamespace
	if err := db.First(&ns, nsID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if ns.Name == "" {
		return nil
	}
	var n int64
	if err := db.Model(&models.IpamPrefix{}).Where("namespace_id = ?", nsID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if err := db.Model(&models.IpamNamespacePrefix{}).Where("namespace_id = ?", nsID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if err := db.Model(&models.IpamVRF{}).Where("namespace_id = ? AND NOT is_default", nsID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if err := db.Where("namespace_id = ?", nsID).Delete(&models.IpamVRF{}).Error; err != nil {
		return err
	}
	return db.Delete(&ns).Error
}
