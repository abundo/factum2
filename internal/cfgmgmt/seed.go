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
// built-in service types, and ELINE platform packs when they are missing.
// Operator-edited packs are left alone; checksum-matching rows are
// refreshed from the embed files. Assignments on non-parameter scopes are
// copied onto a reserved parameters child and the originals are deleted.
// ConfigTemplate rows are copied onto kind=cli children (null scope →
// global); original template rows stay. Packs are upserted as translation
// CLI objects under _catalog/cli/<type>/; the CLI checksum (features +
// context) is the writer that decides whether embed refresh is allowed.
func Seed(db *gorm.DB) error {
	if err := seedRootScope(db); err != nil {
		return err
	}
	if err := seedReservedFolders(db); err != nil {
		return err
	}
	if err := migrateTemplatesToCLI(db); err != nil {
		return err
	}
	eline, err := seedServiceTypes(db)
	if err != nil {
		return err
	}
	if err := seedELINEPacks(db, eline.ID); err != nil {
		return err
	}
	if err := seedPacksToCLI(db); err != nil {
		return err
	}
	if err := copyAssignmentsOntoParameterChildren(db); err != nil {
		return err
	}
	if err := moveAssignmentsOntoParameterChildren(db); err != nil {
		return err
	}
	return ensureScopeUniqueIndexes(db)
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

// migrateTemplatesToCLI copies each ConfigTemplate onto a kind=cli child of
// its scope (or of global when ScopeID is nil). A true twin — baseline CLI
// with the same name+parent and matching platform — is left alone.
// Translation CLI and a different platform do not block the copy.
// Template rows are not deleted.
func migrateTemplatesToCLI(db *gorm.DB) error {
	root, err := RootScope(db)
	if err != nil {
		return err
	}
	var tmpls []models.ConfigTemplate
	if err := db.Find(&tmpls).Error; err != nil {
		return err
	}
	for i := range tmpls {
		t := &tmpls[i]
		parentID := root.ID
		if t.ScopeID != nil {
			parentID = *t.ScopeID
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			twins, err := findBaselineCLITwins(tx, t.Name, parentID, t.Platform)
			if err != nil {
				return err
			}
			if len(twins) > 0 {
				return nil
			}
			pid := parentID
			obj := models.ConfigScope{
				ParentID:    &pid,
				Name:        t.Name,
				Kind:        models.ConfigScopeKindCLI,
				Platform:    t.Platform,
				PayloadKind: t.PayloadKind,
				Enabled:     t.Enabled,
			}
			if err := normalizeCLIScope(&obj); err != nil {
				return err
			}
			if err := tx.Create(&obj).Error; err != nil {
				return err
			}
			// Gorm skips the zero value for a bool with default:true, so a
			// disabled template would be stored enabled unless we write it.
			if !t.Enabled {
				if err := tx.Model(&obj).Update("enabled", false).Error; err != nil {
					return err
				}
			}
			feat := models.ConfigCLIFeature{
				ScopeID:     obj.ID,
				Name:        "body",
				AddCommands: t.Body,
			}
			return tx.Create(&feat).Error
		}); err != nil {
			return err
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
		if row.SeedChecksum == "" && row.ApplyTemplate == p.body {
			row.SeedChecksum = sum
			if err := db.Save(&row).Error; err != nil {
				return err
			}
		}
		// Embed refresh is owned by seedPacksToCLI (CLI is the checksum writer).
	}
	return nil
}

func elineEmbedBody(platform string) string {
	switch NormalizePlatform(platform) {
	case "eos":
		return templates.EOSEline
	case "ios-xr":
		return templates.IOSXREline
	case "sros", "sros-md":
		return templates.SROSEline
	}
	return ""
}

func packEmbedBody(typeName, platform string) string {
	if typeName == "ELINE" {
		return elineEmbedBody(platform)
	}
	return ""
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

func packUntouched(row *models.PlatformPack) bool {
	if row == nil {
		return false
	}
	if row.SeedChecksum == "" {
		return false
	}
	return row.SeedChecksum == packChecksum(row.ApplyTemplate)
}

func cliPayloadKind(row *models.PlatformPack) string {
	if row != nil && row.PayloadKind != "" {
		return row.PayloadKind
	}
	return models.PayloadKindCLI
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

// seedPacksToCLI upserts a translation CLI object per platform_packs row
// under _catalog/cli/<ServiceType.Name>/. CLI is the checksum writer: an
// operator edit of features or context skips embed refresh for both pack
// and CLI. Untouched packs (or a missing CLI) copy embed/current pack onto
// CLI features; an old-UI pack edit with an untouched CLI copies pack → CLI
// once, then CLI is source.
func seedPacksToCLI(db *gorm.DB) error {
	var packs []models.PlatformPack
	if err := db.Find(&packs).Error; err != nil {
		return err
	}
	for i := range packs {
		if err := syncPackCLI(db, &packs[i]); err != nil {
			return err
		}
	}
	return nil
}

func syncPackCLI(db *gorm.DB, pack *models.PlatformPack) error {
	var st models.ServiceType
	if err := db.First(&st, pack.ServiceTypeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	cli, err := lookupCLIObjectByTypeID(db, pack.ServiceTypeID, pack.Platform, false)
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
	embed := packEmbedBody(st.Name, pack.Platform)
	kind := cliPayloadKind(pack)
	apply, cleanup := pack.ApplyTemplate, pack.CleanupTemplate
	mirrorPack := false
	if packUntouched(pack) && embed != "" {
		apply = embed
		cleanup = ""
		mirrorPack = true
	}
	add, remove := packToCLIBlobs(apply, cleanup)
	return writePackCLI(db, pack, st.Name, kind, add, remove, mirrorPack, apply)
}

func writePackCLI(db *gorm.DB, pack *models.PlatformPack, typeName, kind, add, remove string, mirrorPack bool, applyBody string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		cli, err := lookupCLIObjectByTypeID(tx, pack.ServiceTypeID, pack.Platform, false)
		if err != nil {
			return err
		}
		if cli == nil {
			parent, err := catalogCLITypeFolder(tx, typeName)
			if err != nil {
				return err
			}
			stID := pack.ServiceTypeID
			obj := models.ConfigScope{
				ParentID:      &parent.ID,
				Name:          pack.Platform,
				Kind:          models.ConfigScopeKindCLI,
				ServiceTypeID: &stID,
				Platform:      pack.Platform,
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
			cli.Platform = NormalizePlatform(pack.Platform)
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
		feats := []models.ConfigCLIFeature{feat}
		sum := CLIObjectChecksum(cli.Platform, kind, cli.Payload.Context, feats)
		if err := tx.Model(cli).Update("seed_checksum", sum).Error; err != nil {
			return err
		}
		if mirrorPack {
			updates := map[string]any{
				"apply_template":   applyBody,
				"cleanup_template": "",
				"seed_checksum":    packChecksum(applyBody),
				"payload_kind":     kind,
			}
			if err := tx.Model(pack).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
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
