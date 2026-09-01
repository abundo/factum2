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

func schemaHas(fields []models.FieldSchema, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func seedServiceTypes(db *gorm.DB) (*models.ServiceType, error) {
	vlanField := models.FieldSchema{Name: "vlan", Type: models.VarTypeVLAN, Required: true, Description: "Customer VLAN / SAP tag"}
	elineRoles := []models.EndpointRole{
		{Name: "a", Min: 1, Max: 1, Fields: []models.FieldSchema{vlanField}},
		{Name: "b", Min: 1, Max: 1, Fields: []models.FieldSchema{vlanField}},
	}
	elanRole := []models.EndpointRole{
		{Name: "endpoint", Min: 1, Max: 0, Fields: []models.FieldSchema{vlanField}},
	}
	l3Role := []models.EndpointRole{
		{Name: "endpoint", Min: 1, Max: 0},
	}
	elanSchema := []models.FieldSchema{
		{Name: "max_mac_addresses", Type: models.VarTypeInt, Required: true, Description: "Max number of MAC addresses"},
	}
	seeds := []models.ServiceType{
		{Name: "ELINE", Description: "L2VPN point to point", EndpointRoles: elineRoles, Builtin: true, SyncSource: models.SyncSourceELINE, NetboxType: models.NetboxTypeEVPL},
		{Name: "ELAN", Description: "L2VPN multipoint", Schema: elanSchema, EndpointRoles: elanRole, Builtin: true, SyncSource: models.SyncSourceELAN, NetboxType: models.NetboxTypeVPLS},
		{Name: "L3VPN", Description: "L3 multipoint", EndpointRoles: l3Role, Builtin: true, SyncSource: models.SyncSourceL3VPN, NetboxType: models.NetboxTypeVRF},
		{Name: "POLARIX", Description: "Internet", EndpointRoles: l3Role, Builtin: true},
	}
	var eline models.ServiceType
	for _, s := range seeds {
		var existing models.ServiceType
		err := db.Where("name = ?", s.Name).First(&existing).Error
		if err == nil {
			if s.Name == "ELINE" {
				eline = existing
			}
			changed := false
			if existing.SyncSource == "" && s.SyncSource != "" {
				existing.SyncSource = s.SyncSource
				existing.NetboxType = s.NetboxType
				changed = true
			}
			if len(existing.EndpointRoles) == 0 && len(s.EndpointRoles) > 0 {
				existing.EndpointRoles = s.EndpointRoles
				changed = true
			} else if s.Name == "ELAN" && len(existing.EndpointRoles) == 1 && existing.EndpointRoles[0].Name == "endpoint" && len(existing.EndpointRoles[0].Fields) == 0 {
				existing.EndpointRoles = s.EndpointRoles
				changed = true
			}
			if !schemaHas(existing.Schema, "max_mac_addresses") && schemaHas(s.Schema, "max_mac_addresses") {
				existing.Schema = append(existing.Schema, s.Schema...)
				changed = true
			}
			if changed {
				if err := db.Save(&existing).Error; err != nil {
					return nil, err
				}
				if s.Name == "ELINE" {
					eline = existing
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
