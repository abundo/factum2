package cfgmgmt

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// AssertPacksHaveCLITwins errors if platform_packs still exists and any row
// has no kind=cli translator for the same (service_type_id, platform).
// MigrateDatabase must not DROP those tables until every pack was copied.
func AssertPacksHaveCLITwins(db *gorm.DB) error {
	if !db.Migrator().HasTable("platform_packs") {
		return nil
	}
	type leftover struct {
		ServiceTypeID uint   `gorm:"column:service_type_id"`
		Platform      string `gorm:"column:platform"`
	}
	var rows []leftover
	if err := db.Table("platform_packs").Select("service_type_id", "platform").Find(&rows).Error; err != nil {
		return err
	}
	var missing []string
	for _, r := range rows {
		cli, err := lookupCLIObjectByTypeID(db, r.ServiceTypeID, r.Platform, false)
		if err != nil {
			return err
		}
		if cli == nil {
			missing = append(missing, fmt.Sprintf("service_type_id=%d platform=%s", r.ServiceTypeID, r.Platform))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("cannot drop platform_packs: %d pack(s) have no CLI object twin (%s); run the previous migrate before this version",
		len(missing), strings.Join(missing, ", "))
}

// DropPackAndTemplateTables removes leftover platform_packs and
// config_templates. Call AssertPacksHaveCLITwins first.
func DropPackAndTemplateTables(db *gorm.DB) error {
	if db.Migrator().HasTable("platform_packs") {
		if err := db.Migrator().DropTable("platform_packs"); err != nil {
			return fmt.Errorf("drop platform_packs: %w", err)
		}
	}
	if db.Migrator().HasTable("config_templates") {
		if err := db.Migrator().DropTable("config_templates"); err != nil {
			return fmt.Errorf("drop config_templates: %w", err)
		}
	}
	return nil
}
