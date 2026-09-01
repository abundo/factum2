package cfgmgmt

import (
	"errors"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// LookupServiceType finds a service type by name.
func LookupServiceType(db *gorm.DB, name string) (*models.ServiceType, error) {
	var st models.ServiceType
	err := db.Where("name = ?", name).First(&st).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErrf(400, "unknown service type %q", name)
		}
		return nil, err
	}
	return &st, nil
}

func ListServiceTypes(db *gorm.DB) ([]models.ServiceType, error) {
	var rows []models.ServiceType
	if err := db.Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// InventoryMaps returns sync_source → netbox_type for types that have both
// set. When two types share a source, the first in name order wins.
func InventoryMaps(types []models.ServiceType) map[string]string {
	out := map[string]string{}
	for _, st := range types {
		if st.SyncSource == "" || st.NetboxType == "" {
			continue
		}
		if _, ok := out[st.SyncSource]; ok {
			continue
		}
		out[st.SyncSource] = st.NetboxType
	}
	return out
}

// TypeForNetboxKind returns the first service type whose NetboxType matches.
func TypeForNetboxKind(types []models.ServiceType, netboxType string) *models.ServiceType {
	if netboxType == "" {
		return nil
	}
	for i := range types {
		if types[i].NetboxType == netboxType {
			return &types[i]
		}
	}
	return nil
}

// EndpointRolesForCount expands st's roles into n role names for reverse-
// import: bounded roles are filled in order, then any unlimited role
// (Max==0) absorbs the rest. ELINE a/b → ["a","b"]; ELAN endpoint → n copies.
func EndpointRolesForCount(st *models.ServiceType, n int) []string {
	if st == nil || n <= 0 {
		return nil
	}
	out := make([]string, 0, n)
	remaining := n
	unlimited := ""
	for _, role := range st.EndpointRoles {
		if role.Max == 0 {
			if unlimited == "" {
				unlimited = role.Name
			}
			continue
		}
		take := role.Max
		if take > remaining {
			take = remaining
		}
		for i := 0; i < take; i++ {
			out = append(out, role.Name)
		}
		remaining -= take
		if remaining == 0 {
			return out
		}
	}
	if unlimited != "" {
		for remaining > 0 {
			out = append(out, unlimited)
			remaining--
		}
	}
	return out
}

func ServiceTypeExists(db *gorm.DB, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var n int64
	if err := db.Model(&models.ServiceType{}).Where("name = ?", name).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// LookupPlatformPack returns the pack for typeName+platform. sros-md falls
// back to sros when no dedicated pack exists.
func LookupPlatformPack(db *gorm.DB, typeName, platform string) (*models.PlatformPack, error) {
	st, err := LookupServiceType(db, typeName)
	if err != nil {
		return nil, err
	}
	plat := NormalizePlatform(platform)
	var pack models.PlatformPack
	err = db.Where("service_type_id = ? AND platform = ?", st.ID, plat).First(&pack).Error
	if err == nil {
		return &pack, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if plat == "sros-md" {
		err = db.Where("service_type_id = ? AND platform = ?", st.ID, "sros").First(&pack).Error
		if err == nil {
			return &pack, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

func RequireCLIPack(pack *models.PlatformPack) error {
	if pack == nil {
		return statusErr(400, "no platform pack for this service type and device platform")
	}
	kind := pack.PayloadKind
	if kind == "" {
		kind = models.PayloadKindCLI
	}
	if kind != models.PayloadKindCLI {
		return statusErrf(400, "payload kind %q cannot be applied as a CLI session yet", kind)
	}
	return nil
}
