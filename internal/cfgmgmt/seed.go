package cfgmgmt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/drivers/templates"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func packChecksum(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Seed creates the global root scope, reserved _catalog/_services folders,
// built-in service types, and ELINE translation CLI objects when they are
// missing. Operator-edited CLI objects are left alone; checksum-matching
// rows are refreshed from the embed files under
// internal/drivers/templates. Assignments on non-parameter scopes are
// copied onto a reserved parameters child and the originals are deleted.
// Typed CN/CI services without a tree node are placed under _services.
func Seed(db *gorm.DB) error {
	if err := seedRootScope(db); err != nil {
		return err
	}
	if err := seedReservedFolders(db); err != nil {
		return err
	}
	eline, err := seedServiceTypes(db)
	if err != nil {
		return err
	}
	if err := seedELINECLI(db, eline.ID); err != nil {
		return err
	}
	if err := copyAssignmentsOntoParameterChildren(db); err != nil {
		return err
	}
	if err := moveAssignmentsOntoParameterChildren(db); err != nil {
		return err
	}
	if err := migrateServicesToTree(db); err != nil {
		return err
	}
	return ensureScopeUniqueIndexes(db)
}

// migrateServicesToTree places each typed CN/CI inventory row under _services
// when it has no kind=service scope yet. VL/VI/LF/LI and untyped rows are
// skipped. Later Lime/NetBox sync does not call this; operators attach new
// rows themselves.
func migrateServicesToTree(db *gorm.DB) error {
	types, err := ListServiceTypes(db)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(types))
	for _, t := range types {
		known[t.Name] = true
	}
	var svcs []models.Service
	if err := db.Find(&svcs).Error; err != nil {
		return err
	}
	if len(svcs) == 0 {
		return nil
	}
	folder, err := servicesFolder(db)
	if err != nil {
		return err
	}
	for i := range svcs {
		svc := &svcs[i]
		if !known[svc.ServiceType] {
			continue
		}
		if models.OpticalServiceCategories[models.CategoryFromServiceID(svc.ServiceID)] {
			continue
		}
		existing, err := scopeByServiceID(db, svc.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if _, err := insertCanonicalService(tx, folder.ID, svc); err != nil {
				return err
			}
			return projectEndpointScopes(tx, svc.ID)
		}); err != nil {
			return err
		}
	}
	return nil
}

// copyAssignmentsOntoParameterChildren copies assignments on non-parameter
// scopes onto a reserved kind=parameter child named "parameters". Originals
// are deleted by moveAssignmentsOntoParameterChildren immediately after.
func copyAssignmentsOntoParameterChildren(db *gorm.DB) error {
	var scopeIDs []uint
	if err := db.Model(&models.ConfigAssignment{}).Distinct("scope_id").Pluck("scope_id", &scopeIDs).Error; err != nil {
		return err
	}
	if len(scopeIDs) == 0 {
		return nil
	}
	var scopes []models.ConfigScope
	if err := db.Where("id IN ?", scopeIDs).Find(&scopes).Error; err != nil {
		return err
	}
	for i := range scopes {
		s := &scopes[i]
		if s.Kind == models.ConfigScopeKindParameter {
			continue
		}
		child, err := ensureParametersChild(db, s.ID)
		if err != nil {
			return err
		}
		var rows []models.ConfigAssignment
		if err := db.Where("scope_id = ?", s.ID).Find(&rows).Error; err != nil {
			return err
		}
		for _, a := range rows {
			var n int64
			if err := db.Model(&models.ConfigAssignment{}).
				Where("variable_def_id = ? AND scope_id = ?", a.VariableDefID, child.ID).
				Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			cp := models.ConfigAssignment{
				VariableDefID: a.VariableDefID,
				ScopeID:       child.ID,
				Value:         a.Value,
			}
			if err := db.Create(&cp).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// moveAssignmentsOntoParameterChildren deletes original assignment rows on
// non-parameter scopes that already have a matching copy on the reserved
// parameters child. Copied rows stay. Rolling back this binary without a
// DB restore loses assignment values on old resolveDefAt (which only
// reads the walked organizational scope). PUT still remaps folder/device/
// interface writes onto the child until the tree-first GUI PR.
func moveAssignmentsOntoParameterChildren(db *gorm.DB) error {
	var scopeIDs []uint
	if err := db.Model(&models.ConfigAssignment{}).Distinct("scope_id").Pluck("scope_id", &scopeIDs).Error; err != nil {
		return err
	}
	if len(scopeIDs) == 0 {
		return nil
	}
	var scopes []models.ConfigScope
	if err := db.Where("id IN ?", scopeIDs).Find(&scopes).Error; err != nil {
		return err
	}
	for i := range scopes {
		s := &scopes[i]
		if s.Kind == models.ConfigScopeKindParameter {
			continue
		}
		child, err := findParametersChild(db, s.ID)
		if err != nil {
			return err
		}
		if child == nil {
			continue
		}
		var rows []models.ConfigAssignment
		if err := db.Where("scope_id = ?", s.ID).Find(&rows).Error; err != nil {
			return err
		}
		for _, a := range rows {
			var n int64
			if err := db.Model(&models.ConfigAssignment{}).
				Where("variable_def_id = ? AND scope_id = ?", a.VariableDefID, child.ID).
				Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			if err := db.Where("id = ?", a.ID).Delete(&models.ConfigAssignment{}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}


func ensureScopeUniqueIndexes(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	stmts := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_config_scopes_one_device ON config_scopes (device_id) WHERE kind = 'device' AND device_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_config_scopes_one_interface ON config_scopes (interface_id) WHERE kind = 'interface' AND interface_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_config_scopes_one_service ON config_scopes (service_id) WHERE kind = 'service' AND service_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_config_scopes_cli_type_plat ON config_scopes (service_type_id, platform) WHERE kind = 'cli' AND service_type_id IS NOT NULL`,
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

func seedReservedFolders(db *gorm.DB) error {
	root, err := RootScope(db)
	if err != nil {
		return err
	}
	catalog, err := ensureChildFolder(db, root.ID, models.ConfigCatalogName)
	if err != nil {
		return err
	}
	if _, err := ensureChildFolder(db, catalog.ID, models.ConfigCatalogCLIName); err != nil {
		return err
	}
	_, err = ensureChildFolder(db, root.ID, models.ConfigServicesFolderName)
	return err
}

func ensureChildFolder(db *gorm.DB, parentID uint, name string) (*models.ConfigScope, error) {
	var s models.ConfigScope
	err := db.Where("parent_id = ? AND name = ?", parentID, name).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	s = models.ConfigScope{
		ParentID: &parentID,
		Name:     name,
		Kind:     models.ConfigScopeKindFolder,
	}
	if err := db.Create(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func schemaHas(fields []models.FieldSchema, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// mergeMissingSchema appends seeded fields that the stored type does not
// already have. Operator-added extras are left in place.
func mergeMissingSchema(existing *[]models.FieldSchema, seed []models.FieldSchema) bool {
	if existing == nil {
		return false
	}
	changed := false
	for _, f := range seed {
		if schemaHas(*existing, f.Name) {
			continue
		}
		*existing = append(*existing, f)
		changed = true
	}
	return changed
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
	bandwidthField := models.FieldSchema{Name: models.SchemaFieldBandwidthMbps, Type: models.VarTypeInt, Required: true, Description: "Bandwidth (Mbps)"}
	capacitySchema := []models.FieldSchema{bandwidthField}
	elanSchema := []models.FieldSchema{
		bandwidthField,
		{Name: models.SchemaFieldMaxMacAddresses, Type: models.VarTypeInt, Required: true, Description: "Max number of MAC addresses"},
	}
	seeds := []models.ServiceType{
		{Name: "ELINE", Description: "L2VPN point to point", Schema: capacitySchema, EndpointRoles: elineRoles, Builtin: true, SyncSource: models.SyncSourceELINE, NetboxType: models.NetboxTypeEVPL},
		{Name: "ELAN", Description: "L2VPN multipoint", Schema: elanSchema, EndpointRoles: elanRole, Builtin: true, SyncSource: models.SyncSourceELAN, NetboxType: models.NetboxTypeVPLS},
		{Name: "L3VPN", Description: "L3 multipoint", Schema: capacitySchema, EndpointRoles: l3Role, Builtin: true, SyncSource: models.SyncSourceL3VPN, NetboxType: models.NetboxTypeVRF},
		{Name: "POLARIX", Description: "Internet", Schema: capacitySchema, EndpointRoles: l3Role, Builtin: true},
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
			if mergeMissingSchema(&existing.Schema, s.Schema) {
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

func seedELINECLI(db *gorm.DB, elineTypeID uint) error {
	for _, p := range []struct {
		platform string
		body     string
	}{
		{"eos", templates.EOSEline},
		{"ios-xr", templates.IOSXREline},
		{"sros", templates.SROSEline},
		{"sros-md", templates.SROSEline},
	} {
		if err := syncELINECLI(db, elineTypeID, p.platform, p.body); err != nil {
			return err
		}
	}
	return nil
}

func catalogCLITypeFolder(db *gorm.DB, typeName string) (*models.ConfigScope, error) {
	root, err := RootScope(db)
	if err != nil {
		return nil, err
	}
	catalog, err := ensureChildFolder(db, root.ID, models.ConfigCatalogName)
	if err != nil {
		return nil, err
	}
	cliFolder, err := ensureChildFolder(db, catalog.ID, models.ConfigCatalogCLIName)
	if err != nil {
		return nil, err
	}
	return ensureChildFolder(db, cliFolder.ID, typeName)
}

// CLIObjectChecksum is the canonical seed hash for a translation CLI object:
//
//	sha256(platform + "\n" + payload_kind + "\n" + canonicalJSON(context) + "\n" +
//	  for each feature in sort_order:
//	    name + "\nremove_at_root=" + bool + "\nadd\n" + AddCommands +
//	    "\nupdate\n" + UpdateCommands + "\nremove\n" + RemoveCommands + "\n")
//
// Empty UpdateCommands still contributes "\nupdate\n". canonicalJSON uses
// deterministic key order for pattern/enter/exit/captures; nil context is
// "null" so setting enter counts as an operator edit.
func CLIObjectChecksum(platform, payloadKind string, ctx *models.CLIContext, feats []models.ConfigCLIFeature) string {
	var b strings.Builder
	b.WriteString(platform)
	b.WriteByte('\n')
	b.WriteString(payloadKind)
	b.WriteByte('\n')
	b.WriteString(canonicalJSONContext(ctx))
	b.WriteByte('\n')
	for _, f := range feats {
		b.WriteString(f.Name)
		b.WriteByte('\n')
		b.WriteString("remove_at_root=")
		b.WriteString(fmt.Sprintf("%v", f.RemoveAtRoot))
		b.WriteByte('\n')
		b.WriteString("add\n")
		b.WriteString(f.AddCommands)
		b.WriteString("\nupdate\n")
		b.WriteString(f.UpdateCommands)
		b.WriteString("\nremove\n")
		b.WriteString(f.RemoveCommands)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func canonicalJSONContext(ctx *models.CLIContext) string {
	if ctx == nil {
		return "null"
	}
	type canon struct {
		Pattern  string            `json:"pattern"`
		Enter    string            `json:"enter"`
		Exit     string            `json:"exit"`
		Captures map[string]string `json:"captures,omitempty"`
	}
	b, err := json.Marshal(canon{
		Pattern: ctx.Pattern, Enter: ctx.Enter, Exit: ctx.Exit, Captures: ctx.Captures,
	})
	if err != nil {
		return "null"
	}
	return string(b)
}

func currentCLIChecksum(obj *models.ConfigScope, feats []models.ConfigCLIFeature) string {
	kind := obj.PayloadKind
	if kind == "" {
		kind = models.PayloadKindCLI
	}
	return CLIObjectChecksum(obj.Platform, kind, obj.Payload.Context, feats)
}

func syncELINECLI(db *gorm.DB, typeID uint, platform, embed string) error {
	cli, err := lookupCLIObjectByTypeID(db, typeID, platform, false)
	if err != nil {
		return err
	}
	if cli != nil {
		feats, err := ListCLIFeatures(db, cli.ID)
		if err != nil {
			return err
		}
		if cli.SeedChecksum != currentCLIChecksum(cli, feats) {
			return nil
		}
	}
	add, remove := packToCLIBlobs(embed, "")
	return writeTranslationCLI(db, typeID, "ELINE", platform, models.PayloadKindCLI, add, remove)
}

func writeTranslationCLI(db *gorm.DB, typeID uint, typeName, platform, kind, add, remove string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		cli, err := lookupCLIObjectByTypeID(tx, typeID, platform, false)
		if err != nil {
			return err
		}
		if cli == nil {
			parent, err := catalogCLITypeFolder(tx, typeName)
			if err != nil {
				return err
			}
			stID := typeID
			obj := models.ConfigScope{
				ParentID:      &parent.ID,
				Name:          platform,
				Kind:          models.ConfigScopeKindCLI,
				ServiceTypeID: &stID,
				Platform:      platform,
				PayloadKind:   kind,
				Enabled:       true,
			}
			created, err := CreateScope(tx, &obj)
			if err != nil {
				return err
			}
			cli = created
		} else {
			cli.PayloadKind = kind
			cli.Platform = NormalizePlatform(platform)
			cli.Payload.Context = nil
			if err := tx.Save(cli).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("scope_id = ?", cli.ID).Delete(&models.ConfigCLIFeature{}).Error; err != nil {
			return err
		}
		feat := models.ConfigCLIFeature{
			ScopeID:        cli.ID,
			Name:           "apply",
			SortOrder:      0,
			AddCommands:    add,
			RemoveCommands: remove,
		}
		if err := tx.Create(&feat).Error; err != nil {
			return err
		}
		sum := CLIObjectChecksum(cli.Platform, kind, cli.Payload.Context, []models.ConfigCLIFeature{feat})
		return tx.Model(cli).Update("seed_checksum", sum).Error
	})
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
