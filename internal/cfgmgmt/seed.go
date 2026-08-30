package cfgmgmt

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/abundo/factum2/internal/drivers/templates"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func packChecksum(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Seed creates the global root scope, built-in service types, and ELINE
// platform packs when they are missing. Operator-edited packs are left
// alone; checksum-matching rows are refreshed from the embed files.
func Seed(db *gorm.DB) error {
	if err := seedRootScope(db); err != nil {
		return err
	}
	eline, err := seedServiceTypes(db)
	if err != nil {
		return err
	}
	if err := seedELINEPacks(db, eline.ID); err != nil {
		return err
	}
	return ensureScopeUniqueIndexes(db)
}

func ensureScopeUniqueIndexes(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	stmts := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_config_scopes_one_device ON config_scopes (device_id) WHERE kind = 'device' AND device_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_config_scopes_one_interface ON config_scopes (interface_id) WHERE kind = 'interface' AND interface_id IS NOT NULL`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}

func seedRootScope(db *gorm.DB) error {
	var n int64
	if err := db.Model(&models.ConfigScope{}).Where("parent_id IS NULL AND name = ?", models.ConfigRootName).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	root := models.ConfigScope{
		Name: models.ConfigRootName,
		Kind: models.ConfigScopeKindFolder,
	}
	return db.Create(&root).Error
}

func seedServiceTypes(db *gorm.DB) (*models.ServiceType, error) {
	elineRoles := []models.EndpointRole{
		{Name: "a", Min: 1, Max: 1, Fields: []models.FieldSchema{{Name: "vlan", Type: models.VarTypeVLAN, Required: true}}},
		{Name: "b", Min: 1, Max: 1, Fields: []models.FieldSchema{{Name: "vlan", Type: models.VarTypeVLAN, Required: true}}},
	}
	endpointRole := []models.EndpointRole{
		{Name: "endpoint", Min: 1, Max: 0},
	}
	seeds := []models.ServiceType{
		{Name: "ELINE", Description: "L2VPN point to point", EndpointRoles: elineRoles, Builtin: true},
		{Name: "ELAN", Description: "L2VPN multipoint", EndpointRoles: endpointRole, Builtin: true},
		{Name: "L3VPN", Description: "L3 multipoint", EndpointRoles: endpointRole, Builtin: true},
		{Name: "POLARIX", Description: "Internet", EndpointRoles: endpointRole, Builtin: true},
	}
	var eline models.ServiceType
	for _, s := range seeds {
		var existing models.ServiceType
		err := db.Where("name = ?", s.Name).First(&existing).Error
		if err == nil {
			if s.Name == "ELINE" {
				eline = existing
			}
			if len(existing.EndpointRoles) == 0 && len(s.EndpointRoles) > 0 {
				existing.EndpointRoles = s.EndpointRoles
				if err := db.Save(&existing).Error; err != nil {
					return nil, err
				}
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err := db.Create(&s).Error; err != nil {
			return nil, err
		}
		if s.Name == "ELINE" {
			eline = s
		}
	}
	if eline.ID == 0 {
		if err := db.Where("name = ?", "ELINE").First(&eline).Error; err != nil {
			return nil, err
		}
	}
	return &eline, nil
}

func seedELINEPacks(db *gorm.DB, elineTypeID uint) error {
	packs := []struct {
		platform string
		body     string
	}{
		{"eos", templates.EOSEline},
		{"ios-xr", templates.IOSXREline},
		{"sros", templates.SROSEline},
		{"sros-md", templates.SROSEline},
	}
	for _, p := range packs {
		sum := packChecksum(p.body)
		var row models.PlatformPack
		err := db.Where("service_type_id = ? AND platform = ?", elineTypeID, p.platform).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = models.PlatformPack{
				ServiceTypeID: elineTypeID,
				Platform:      p.platform,
				PayloadKind:   models.PayloadKindCLI,
				ApplyTemplate: p.body,
				SeedChecksum:  sum,
			}
			if err := db.Create(&row).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		untouched := row.SeedChecksum != "" && row.SeedChecksum == packChecksum(row.ApplyTemplate)
		if row.SeedChecksum == "" && row.ApplyTemplate == p.body {
			row.SeedChecksum = sum
			if err := db.Save(&row).Error; err != nil {
				return err
			}
			continue
		}
		if !untouched {
			continue
		}
		if row.ApplyTemplate == p.body && row.SeedChecksum == sum {
			continue
		}
		row.ApplyTemplate = p.body
		row.SeedChecksum = sum
		if err := db.Save(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// RootScope returns the global root folder.
func RootScope(db *gorm.DB) (*models.ConfigScope, error) {
	var root models.ConfigScope
	err := db.Where("parent_id IS NULL AND name = ?", models.ConfigRootName).First(&root).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(500, "global config scope is missing")
		}
		return nil, err
	}
	return &root, nil
}
