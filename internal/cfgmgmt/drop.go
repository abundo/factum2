package cfgmgmt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// leftoverPack is the dropped platform_packs row shape, used only to copy
// remaining rows onto kind=cli translators before DROP.
type leftoverPack struct {
	ServiceTypeID   uint   `gorm:"column:service_type_id"`
	Platform        string `gorm:"column:platform"`
	PayloadKind     string `gorm:"column:payload_kind"`
	ApplyTemplate   string `gorm:"column:apply_template"`
	CleanupTemplate string `gorm:"column:cleanup_template"`
	SeedChecksum    string `gorm:"column:seed_checksum"`
}

func (leftoverPack) TableName() string { return "platform_packs" }

type leftoverTemplate struct {
	Name        string `gorm:"column:name"`
	Platform    string `gorm:"column:platform"`
	PayloadKind string `gorm:"column:payload_kind"`
	Body        string `gorm:"column:body"`
	ScopeID     *uint  `gorm:"column:scope_id"`
	Enabled     bool   `gorm:"column:enabled"`
}

func (leftoverTemplate) TableName() string { return "config_templates" }

func leftoverSelect(db *gorm.DB, model any, cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if db.Migrator().HasColumn(model, c) {
			out = append(out, c)
		}
	}
	return out
}

// migrateLeftoverPacksToCLI copies each leftover platform_packs row onto a
// kind=cli translator if one does not already exist. Operator-edited pack
// bodies keep a checksum that will not match the CLI object hash so
// seedELINECLI will not refresh them from embed.
func migrateLeftoverPacksToCLI(db *gorm.DB) error {
	if !db.Migrator().HasTable("platform_packs") {
		return nil
	}
	cols := leftoverSelect(db, leftoverPack{}, []string{
		"service_type_id", "platform", "payload_kind",
		"apply_template", "cleanup_template", "seed_checksum",
	})
	if len(cols) == 0 {
		return nil
	}
	var rows []leftoverPack
	if err := db.Table("platform_packs").Select(cols).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		cli, err := lookupCLIObjectByTypeID(db, r.ServiceTypeID, r.Platform, false)
		if err != nil {
			return err
		}
		if cli != nil {
			continue
		}
		if strings.TrimSpace(r.ApplyTemplate) == "" && strings.TrimSpace(r.CleanupTemplate) == "" {
			continue
		}
		var st models.ServiceType
		if err := db.First(&st, r.ServiceTypeID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		kind := r.PayloadKind
		if kind == "" {
			kind = models.PayloadKindCLI
		}
		add, remove := packToCLIBlobs(r.ApplyTemplate, r.CleanupTemplate)
		if err := writeTranslationCLI(db, r.ServiceTypeID, st.Name, r.Platform, kind, add, remove); err != nil {
			return fmt.Errorf("migrate pack service_type_id=%d platform=%s: %w", r.ServiceTypeID, r.Platform, err)
		}
		untouched := r.SeedChecksum != "" && r.SeedChecksum == packChecksum(r.ApplyTemplate)
		if untouched {
			continue
		}
		cli, err = lookupCLIObjectByTypeID(db, r.ServiceTypeID, r.Platform, false)
		if err != nil {
			return err
		}
		if cli == nil {
			continue
		}
		if err := db.Model(cli).UpdateColumn("seed_checksum", r.SeedChecksum).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateLeftoverTemplatesToCLI copies each leftover config_templates row
// onto a baseline kind=cli object (same name, parent = scope_id or global).
func migrateLeftoverTemplatesToCLI(db *gorm.DB) error {
	if !db.Migrator().HasTable("config_templates") {
		return nil
	}
	cols := leftoverSelect(db, leftoverTemplate{}, []string{
		"name", "platform", "payload_kind", "body", "scope_id", "enabled",
	})
	if len(cols) == 0 {
		return nil
	}
	var rows []leftoverTemplate
	if err := db.Table("config_templates").Select(cols).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	root, err := RootScope(db)
	if err != nil {
		return err
	}
	hasEnabled := db.Migrator().HasColumn(leftoverTemplate{}, "enabled")
	for _, r := range rows {
		parentID := root.ID
		if r.ScopeID != nil {
			parentID = *r.ScopeID
		}
		twins, err := findBaselineCLITwins(db, r.Name, parentID, r.Platform)
		if err != nil {
			return err
		}
		if len(twins) > 0 {
			continue
		}
		if _, err := GetScope(db, parentID); err != nil {
			continue
		}
		kind := r.PayloadKind
		if kind == "" {
			kind = models.PayloadKindCLI
		}
		obj := models.ConfigScope{
			ParentID:    &parentID,
			Name:        r.Name,
			Kind:        models.ConfigScopeKindCLI,
			Platform:    r.Platform,
			PayloadKind: kind,
			Enabled:     true,
		}
		created, err := CreateScope(db, &obj)
		if err != nil {
			return fmt.Errorf("migrate template name=%s platform=%s: %w", r.Name, r.Platform, err)
		}
		if hasEnabled && !r.Enabled {
			if err := db.Model(created).Update("enabled", false).Error; err != nil {
				return err
			}
		}
		feat := models.ConfigCLIFeature{
			ScopeID:     created.ID,
			Name:        "body",
			AddCommands: r.Body,
		}
		if err := db.Create(&feat).Error; err != nil {
			return err
		}
	}
	return nil
}

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
