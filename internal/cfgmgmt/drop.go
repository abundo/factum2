package cfgmgmt

import (
	"fmt"
	"strings"

	"github.com/abundo/factum2/models"
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

// AssertTemplatesHaveCLITwins errors if config_templates still exists and any
// row has no baseline kind=cli twin (same name, parent = scope_id or global
// if null, platform-compatible). Translation CLI does not count.
func AssertTemplatesHaveCLITwins(db *gorm.DB) error {
	if !db.Migrator().HasTable("config_templates") {
		return nil
	}
	type leftover struct {
		Name     string `gorm:"column:name"`
		Platform string `gorm:"column:platform"`
		ScopeID  *uint  `gorm:"column:scope_id"`
	}
	var rows []leftover
	if err := db.Table("config_templates").Select("name", "platform", "scope_id").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	var globalID *uint
	root, err := RootScope(db)
	if err == nil {
		globalID = &root.ID
	} else if AsStatusError(err) == nil {
		return err
	}
	var missing []string
	for _, r := range rows {
		parentID := globalID
		if r.ScopeID != nil {
			parentID = r.ScopeID
		}
		if parentID == nil {
			missing = append(missing, fmt.Sprintf("name=%s platform=%s parent=global", r.Name, r.Platform))
			continue
		}
		twins, err := findBaselineCLITwins(db, r.Name, *parentID, r.Platform)
		if err != nil {
			return err
		}
		if len(twins) == 0 {
			missing = append(missing, fmt.Sprintf("name=%s platform=%s parent_id=%d", r.Name, r.Platform, *parentID))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("cannot drop config_templates: %d template(s) have no CLI object twin (%s); run the previous migrate before this version",
		len(missing), strings.Join(missing, ", "))
}

func findBaselineCLITwins(db *gorm.DB, name string, parentID uint, tmplPlatform string) ([]models.ConfigScope, error) {
	var rows []models.ConfigScope
	if err := db.Where("kind = ? AND name = ? AND parent_id = ?", models.ConfigScopeKindCLI, name, parentID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]models.ConfigScope, 0, len(rows))
	for i := range rows {
		s := &rows[i]
		if isTranslationCLI(s) {
			continue
		}
		if !cliTemplatePlatformsMatch(s.Platform, tmplPlatform) {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
}

func cliTemplatePlatformsMatch(cliPlat, tmplPlat string) bool {
	if strings.TrimSpace(cliPlat) == "" || strings.TrimSpace(tmplPlat) == "" {
		return true
	}
	return NormalizePlatform(cliPlat) == NormalizePlatform(tmplPlat)
}

// DropPackAndTemplateTables removes leftover platform_packs and
// config_templates. Call AssertPacksHaveCLITwins and
// AssertTemplatesHaveCLITwins first.
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
