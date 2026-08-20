package ipam

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const defaultVRFName = "default"

// StatusError is an application-level failure with an HTTP-ish status so
// handlers don't have to string-match error text.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string { return e.Message }

func statusErr(status int, msg string) *StatusError {
	return &StatusError{Status: status, Message: msg}
}

func normalizeName(s string) string {
	return strings.TrimSpace(s)
}

// NamespaceView is a namespace plus the bits the list/detail pages need
// without extra round-trips.
type NamespaceView struct {
	models.IpamNamespace
	Pools       []models.IpamNamespacePrefix `json:"pools"`
	VRFs        []models.IpamVRF             `json:"vrfs"`
	PrefixCount int64                        `json:"prefix_count"`
	PoolCount   int                          `json:"pool_count"`
	VRFCount    int                          `json:"vrf_count"`
}

func loadNamespace(tx *gorm.DB, id uint) (*models.IpamNamespace, error) {
	var ns models.IpamNamespace
	if err := tx.First(&ns, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "namespace not found")
		}
		return nil, err
	}
	return &ns, nil
}

func GetNamespace(db *gorm.DB, id uint) (*NamespaceView, error) {
	ns, err := loadNamespace(db, id)
	if err != nil {
		return nil, err
	}
	view := &NamespaceView{IpamNamespace: *ns}
	if err := db.Where("namespace_id = ?", id).Order("prefix").Find(&view.Pools).Error; err != nil {
		return nil, err
	}
	if err := db.Where("namespace_id = ?", id).Order("is_default desc, name").Find(&view.VRFs).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.IpamPrefix{}).Where("namespace_id = ?", id).Count(&view.PrefixCount).Error; err != nil {
		return nil, err
	}
	view.PoolCount = len(view.Pools)
	view.VRFCount = len(view.VRFs)
	return view, nil
}

func ListNamespaces(db *gorm.DB) ([]NamespaceView, error) {
	var rows []models.IpamNamespace
	if err := db.Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]NamespaceView, 0, len(rows))
	for _, ns := range rows {
		view, err := GetNamespace(db, ns.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return out, nil
}

func CreateNamespace(db *gorm.DB, name, description string) (*NamespaceView, error) {
	name = normalizeName(name)
	if name == "" {
		return nil, statusErr(400, "name is required")
	}
	var created *NamespaceView
	err := db.Transaction(func(tx *gorm.DB) error {
		ns := models.IpamNamespace{Name: name, Description: strings.TrimSpace(description)}
		if err := tx.Create(&ns).Error; err != nil {
			if isUniqueViolation(err) {
				return statusErr(409, "a namespace with that name already exists")
			}
			return err
		}
		vrf := models.IpamVRF{NamespaceID: ns.ID, Name: defaultVRFName, IsDefault: true}
		if err := tx.Create(&vrf).Error; err != nil {
			return err
		}
		view, err := GetNamespace(tx, ns.ID)
		if err != nil {
			return err
		}
		created = view
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func UpdateNamespace(db *gorm.DB, id uint, name, description string) (*NamespaceView, error) {
	name = normalizeName(name)
	if name == "" {
		return nil, statusErr(400, "name is required")
	}
	if _, err := loadNamespace(db, id); err != nil {
		return nil, err
	}
	res := db.Model(&models.IpamNamespace{}).Where("id = ?", id).Updates(map[string]any{
		"name":        name,
		"description": strings.TrimSpace(description),
	})
	if res.Error != nil {
		if isUniqueViolation(res.Error) {
			return nil, statusErr(409, "a namespace with that name already exists")
		}
		return nil, res.Error
	}
	return GetNamespace(db, id)
}

func DeleteNamespace(db *gorm.DB, id uint) error {
	if _, err := loadNamespace(db, id); err != nil {
		return err
	}
	var n int64
	if err := db.Model(&models.IpamPrefix{}).Where("namespace_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return statusErr(409, "namespace still has allocated prefixes")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("namespace_id = ?", id).Delete(&models.IpamNamespacePrefix{}).Error; err != nil {
			return err
		}
		if err := tx.Where("namespace_id = ?", id).Delete(&models.IpamVRF{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.IpamNamespace{}, id).Error
	})
}

func ListPools(db *gorm.DB, nsID uint) ([]models.IpamNamespacePrefix, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}
	var rows []models.IpamNamespacePrefix
	if err := db.Where("namespace_id = ?", nsID).Order("family, prefix").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func AddPool(db *gorm.DB, nsID uint, raw string) (*models.IpamNamespacePrefix, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}
	p, err := ParsePrefix(raw)
	if err != nil {
		return nil, statusErr(400, err.Error())
	}
	var existing []models.IpamNamespacePrefix
	if err := db.Where("namespace_id = ?", nsID).Find(&existing).Error; err != nil {
		return nil, err
	}
	got := make([]netip.Prefix, 0, len(existing))
	for _, row := range existing {
		pp, err := ParsePrefix(row.Prefix)
		if err != nil {
			continue
		}
		got = append(got, pp)
	}
	if err := checkAddPool(got, p); err != nil {
		return nil, statusErr(409, err.Error())
	}
	row := models.IpamNamespacePrefix{
		NamespaceID: nsID,
		Prefix:      p.String(),
		Family:      familyOf(p),
	}
	if err := db.Create(&row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, statusErr(409, errPoolDuplicate.Error())
		}
		return nil, err
	}
	return &row, nil
}

func DeletePool(db *gorm.DB, nsID, poolID uint) error {
	var pool models.IpamNamespacePrefix
	if err := db.Where("id = ? AND namespace_id = ?", poolID, nsID).First(&pool).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return statusErr(404, "allowed prefix not found")
		}
		return err
	}
	var pools []models.IpamNamespacePrefix
	if err := db.Where("namespace_id = ? AND id <> ?", nsID, poolID).Find(&pools).Error; err != nil {
		return err
	}
	remaining := make([]netip.Prefix, 0, len(pools))
	for _, row := range pools {
		pp, err := ParsePrefix(row.Prefix)
		if err != nil {
			continue
		}
		remaining = append(remaining, pp)
	}
	var allocs []models.IpamPrefix
	if err := db.Where("namespace_id = ?", nsID).Find(&allocs).Error; err != nil {
		return err
	}
	need := make([]netip.Prefix, 0, len(allocs))
	for _, a := range allocs {
		pp, err := ParsePrefix(a.Prefix)
		if err != nil {
			continue
		}
		need = append(need, pp)
	}
	if poolStillNeeded(remaining, need) {
		return statusErr(409, "allocated prefixes still depend on this allowed prefix")
	}
	return db.Delete(&pool).Error
}

func ListVRFs(db *gorm.DB, nsID uint) ([]models.IpamVRF, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}
	var rows []models.IpamVRF
	if err := db.Where("namespace_id = ?", nsID).Order("is_default desc, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func CreateVRF(db *gorm.DB, nsID uint, name, description string) (*models.IpamVRF, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}
	name = normalizeName(name)
	if name == "" {
		return nil, statusErr(400, "name is required")
	}
	row := models.IpamVRF{
		NamespaceID: nsID,
		Name:        name,
		Description: strings.TrimSpace(description),
	}
	if err := db.Create(&row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, statusErr(409, "a VRF with that name already exists in this namespace")
		}
		return nil, err
	}
	return &row, nil
}

func UpdateVRF(db *gorm.DB, nsID, vrfID uint, name, description string) (*models.IpamVRF, error) {
	var row models.IpamVRF
	if err := db.Where("id = ? AND namespace_id = ?", vrfID, nsID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "VRF not found")
		}
		return nil, err
	}
	name = normalizeName(name)
	if name == "" {
		return nil, statusErr(400, "name is required")
	}
	row.Name = name
	row.Description = strings.TrimSpace(description)
	if err := db.Save(&row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, statusErr(409, "a VRF with that name already exists in this namespace")
		}
		return nil, err
	}
	return &row, nil
}

func DeleteVRF(db *gorm.DB, nsID, vrfID uint) error {
	var row models.IpamVRF
	if err := db.Where("id = ? AND namespace_id = ?", vrfID, nsID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return statusErr(404, "VRF not found")
		}
		return err
	}
	if row.IsDefault {
		return statusErr(409, "the default VRF cannot be deleted")
	}
	var n int64
	if err := db.Model(&models.IpamPrefix{}).Where("vrf_id = ?", vrfID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return statusErr(409, "VRF still has allocated prefixes")
	}
	return db.Delete(&row).Error
}

func ListPrefixes(db *gorm.DB, nsID uint) ([]models.IpamPrefix, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}
	var rows []models.IpamPrefix
	if err := db.Where("namespace_id = ?", nsID).Order("prefix").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func Allocate(db *gorm.DB, nsID, vrfID uint, raw, description string) (*models.IpamPrefix, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}
	p, err := ParsePrefix(raw)
	if err != nil {
		return nil, statusErr(400, err.Error())
	}

	var created *models.IpamPrefix
	err = db.Transaction(func(tx *gorm.DB) error {
		vrf, err := loadVRF(tx, nsID, vrfID)
		if err != nil {
			return err
		}
		existing, err := loadAllocations(tx, nsID)
		if err != nil {
			return err
		}
		if err := checkAllocate(existing, p, vrf.ID); err != nil {
			return statusErr(409, err.Error())
		}
		row := models.IpamPrefix{
			NamespaceID: nsID,
			VRFID:       vrf.ID,
			Prefix:      p.String(),
			Family:      familyOf(p),
			Description: strings.TrimSpace(description),
		}
		if err := tx.Create(&row).Error; err != nil {
			if isUniqueViolation(err) {
				return statusErr(409, errDuplicate.Error())
			}
			return err
		}
		created = &row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func UpdatePrefix(db *gorm.DB, nsID, prefixID uint, description string) (*models.IpamPrefix, error) {
	var row models.IpamPrefix
	if err := db.Where("id = ? AND namespace_id = ?", prefixID, nsID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "prefix not found")
		}
		return nil, err
	}
	row.Description = strings.TrimSpace(description)
	if err := db.Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func DeletePrefix(db *gorm.DB, nsID, prefixID uint) error {
	var row models.IpamPrefix
	if err := db.Where("id = ? AND namespace_id = ?", prefixID, nsID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return statusErr(404, "prefix not found")
		}
		return err
	}
	parent, err := ParsePrefix(row.Prefix)
	if err != nil {
		return err
	}
	var siblings []models.IpamPrefix
	if err := db.Where("namespace_id = ? AND id <> ?", nsID, prefixID).Find(&siblings).Error; err != nil {
		return err
	}
	for _, s := range siblings {
		child, err := ParsePrefix(s.Prefix)
		if err != nil {
			continue
		}
		if strictlyContains(parent, child) {
			return statusErr(409, "prefix still has more-specific allocations")
		}
	}
	return db.Delete(&row).Error
}

func loadVRF(tx *gorm.DB, nsID, vrfID uint) (*models.IpamVRF, error) {
	var vrf models.IpamVRF
	q := tx.Where("namespace_id = ?", nsID)
	if vrfID == 0 {
		q = q.Where("is_default = ?", true)
	} else {
		q = q.Where("id = ?", vrfID)
	}
	if err := q.First(&vrf).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "VRF not found")
		}
		return nil, err
	}
	return &vrf, nil
}

func loadPoolPrefixes(tx *gorm.DB, nsID uint) ([]netip.Prefix, error) {
	var rows []models.IpamNamespacePrefix
	if err := tx.Where("namespace_id = ?", nsID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]netip.Prefix, 0, len(rows))
	for _, row := range rows {
		p, err := ParsePrefix(row.Prefix)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func loadAllocations(tx *gorm.DB, nsID uint) ([]allocation, error) {
	var rows []models.IpamPrefix
	if err := tx.Where("namespace_id = ?", nsID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]allocation, 0, len(rows))
	for _, row := range rows {
		p, err := ParsePrefix(row.Prefix)
		if err != nil {
			continue
		}
		out = append(out, allocation{Prefix: p, VRFID: row.VRFID})
	}
	return out, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate")
}
