package netbox

import (
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// nbDeviceRoleREST is one NetBox dcim.device-role.
type nbDeviceRoleREST struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Color       string `json:"color"`
	Description string `json:"description"`
	VMRole      bool   `json:"vm_role"`
}

// syncDeviceRoles mirrors NetBox device roles into the Factum catalog.
// An empty list leaves the catalog unchanged. A role that disappears is
// removed only when no device still stores that NetBox role id. A rename
// updates devices that point at the id.
func syncDeviceRoles(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	if nb == nil {
		return nil
	}
	rows, err := restListAll[nbDeviceRoleREST](nb, "/api/dcim/device-roles/")
	if err != nil {
		return fmt.Errorf("device roles: %w", err)
	}
	if len(rows) == 0 {
		if reporter != nil {
			reporter.Emit(jobevent.Warning, "NetBox device roles: empty list, leaving catalog unchanged")
		}
		return nil
	}
	n, err := upsertDeviceRoles(db, rows)
	if err != nil {
		return err
	}
	if reporter != nil {
		reporter.Emit(jobevent.Info, "Netbox sync: %d device roles", n)
	}
	return nil
}

func upsertDeviceRoles(db *gorm.DB, rows []nbDeviceRoleREST) (int, error) {
	seen := make(map[uint]bool, len(rows))
	n := 0
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if row.ID == 0 || name == "" {
			continue
		}
		seen[row.ID] = true
		slug := strings.TrimSpace(row.Slug)
		if slug == "" {
			slug = models.Slugify(name)
		}
		if slug == "" {
			slug = fmt.Sprintf("role-%d", row.ID)
		}
		saved, err := saveDeviceRole(db, models.DeviceRole{
			Name:        name,
			Slug:        slug,
			Color:       strings.TrimSpace(row.Color),
			Description: row.Description,
			VMRole:      row.VMRole,
			Source:      "netbox",
			NetboxID:    row.ID,
		}, true)
		if err != nil {
			return 0, fmt.Errorf("device role %s: %w", name, err)
		}
		if err := renameDevicesForRole(db, saved.NetboxID, saved.Name); err != nil {
			return 0, err
		}
		n++
	}
	if err := deleteMissingDeviceRoles(db, seen); err != nil {
		return 0, err
	}
	return n, nil
}

// upsertDeviceRoleRef records the role attached to one synced device.
// Slug, color, and description already stored from the role list are kept.
func upsertDeviceRoleRef(db *gorm.DB, name string, netboxID uint) error {
	name = strings.TrimSpace(name)
	if name == "" || netboxID == 0 {
		return nil
	}
	slug := models.Slugify(name)
	if slug == "" {
		slug = fmt.Sprintf("role-%d", netboxID)
	}
	saved, err := saveDeviceRole(db, models.DeviceRole{
		Name:     name,
		Slug:     slug,
		Source:   "netbox",
		NetboxID: netboxID,
	}, false)
	if err != nil {
		return fmt.Errorf("device role %s: %w", name, err)
	}
	return renameDevicesForRole(db, saved.NetboxID, saved.Name)
}

// saveDeviceRole inserts or updates one catalog row. authoritative replaces
// slug, color, description, and vm_role. A non-authoritative save (a device
// payload that only has the name) leaves those fields alone on an existing row.
func saveDeviceRole(db *gorm.DB, in models.DeviceRole, authoritative bool) (models.DeviceRole, error) {
	var row models.DeviceRole
	// Match only the NetBox id. A Factum-only role with the same slug
	// stays local; the NetBox copy is a second row.
	err := db.Where("netbox_id = ? AND netbox_id != 0", in.NetboxID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slug, slugErr := freeDeviceRoleSlug(db, in.Slug, in.NetboxID, 0)
		if slugErr != nil {
			return models.DeviceRole{}, slugErr
		}
		in.Slug = slug
		if err := db.Create(&in).Error; err != nil {
			return models.DeviceRole{}, err
		}
		return in, nil
	}
	if err != nil {
		return models.DeviceRole{}, err
	}
	row.Name = in.Name
	row.Source = "netbox"
	if in.NetboxID != 0 {
		row.NetboxID = in.NetboxID
	}
	if authoritative {
		slug, slugErr := freeDeviceRoleSlug(db, in.Slug, row.NetboxID, row.ID)
		if slugErr != nil {
			return models.DeviceRole{}, slugErr
		}
		row.Slug = slug
		row.Color = in.Color
		row.Description = in.Description
		row.VMRole = in.VMRole
	}
	if err := db.Save(&row).Error; err != nil {
		return models.DeviceRole{}, err
	}
	return row, nil
}

// freeDeviceRoleSlug returns slug, or slug-<id> when another row already
// owns slug. selfID is the catalog row being updated, 0 on insert.
func freeDeviceRoleSlug(db *gorm.DB, slug string, netboxID, selfID uint) (string, error) {
	if slug == "" {
		if netboxID == 0 {
			return "", nil
		}
		slug = fmt.Sprintf("role-%d", netboxID)
	}
	q := db.Where("slug = ? AND source = ?", slug, "netbox")
	if selfID != 0 {
		q = q.Where("id <> ?", selfID)
	}
	var other models.DeviceRole
	err := q.Take(&other).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return slug, nil
	}
	if err != nil {
		return "", err
	}
	if netboxID != 0 && other.NetboxID == netboxID {
		return slug, nil
	}
	if netboxID == 0 {
		return slug, nil
	}
	return fmt.Sprintf("%s-%d", slug, netboxID), nil
}

func renameDevicesForRole(db *gorm.DB, netboxID uint, name string) error {
	if netboxID == 0 || name == "" {
		return nil
	}
	return db.Model(&models.Device{}).
		Where("role_id = ? AND role <> ?", netboxID, name).
		Update("role", name).Error
}

func deleteMissingDeviceRoles(db *gorm.DB, seen map[uint]bool) error {
	var existing []models.DeviceRole
	if err := db.Where("source = ? AND netbox_id != 0", "netbox").Find(&existing).Error; err != nil {
		return err
	}
	for _, row := range existing {
		if seen[row.NetboxID] {
			continue
		}
		var n int64
		if err := db.Model(&models.Device{}).Where("role_id = ?", row.NetboxID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if err := db.Delete(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
