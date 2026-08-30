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
