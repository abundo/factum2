package netbox

import (
	"errors"
	"strings"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/factum2/internal/netboxtool"
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
	ImportTargets []nbRouteTargetRef `json:"import_targets"`
	ExportTargets []nbRouteTargetRef `json:"export_targets"`
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

// syncVRFs mirrors NetBox ipam.VRF rows into the empty (default) namespace
// as extra VRFs. Factum-created VRFs are never deleted. An empty fetch does
// not wipe already-synced rows.
func syncVRFs(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbVRFREST](nb, "/api/ipam/vrfs/")
	if err != nil {
		return err
	}

	ns, err := ipam.EnsureEmptyNamespace(db)
	if err != nil {
		return err
	}

	seen := make(map[uint]bool, len(rows))
	var countNew, countUpdated, countSkipped int
	for _, row := range rows {
		created, updated, skipped, err := upsertNetboxVRF(db, ns.ID, row, reporter)
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

func upsertNetboxVRF(db *gorm.DB, nsID uint, row nbVRFREST, reporter jobevent.Reporter) (created, updated, skipped int, err error) {
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
		changes := map[string]any{
			"name":        name,
			"description": desc,
			"rd":          rd,
			"import_rt":   imp,
			"export_rt":   exp,
			"source":      models.VRFSourceNetbox,
		}
		if err := db.Model(&existing).Updates(changes).Error; err != nil {
			return 0, 0, 0, err
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
		NamespaceID: nsID,
		Name:        name,
		Description: desc,
		RD:          rd,
		ImportRT:    imp,
		ExportRT:    exp,
		Source:      models.VRFSourceNetbox,
		NetboxID:    row.ID,
	}
	if err := db.Create(&createdRow).Error; err != nil {
		return 0, 0, 0, err
	}
	return 1, 0, 0, nil
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
	if err := db.Delete(&row).Error; err != nil {
		return 0, err
	}
	return 1, nil
}
