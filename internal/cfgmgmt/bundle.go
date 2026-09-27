package cfgmgmt

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const (
	BundleFormat  = "factum2-config"
	BundleVersion = 1

	// maxBundleImage matches the connection-type image route (512KiB).
	maxBundleImage = 512 * 1024
)

// Config bundles are portable JSON. Identity is the service-definition name,
// macro name, variable name, CLI path (or service type + platform), and
// parameter path — never database ids. Import upserts those objects and
// replaces that object's CLI features, connection types, and parameter
// assignments. Rows that are not in the file are left in place.

// BundleExportRequest is POST /api/config/bundle/export.
// Paths are slash-separated scope names starting at global
// ("global/_catalog/cli/ELINE/eos").
type BundleExportRequest struct {
	ServiceDefinitions []string `json:"service_definitions"`
	CLIObjects         []string `json:"cli_objects"`
	Macros             []string `json:"macros"`
	ParameterObjects   []string `json:"parameter_objects"`
	// Related adds translation CLI for selected definitions (and the
	// definition for a selected CLI), macros those templates include,
	// variable definitions the templates read, and parameter objects
	// that assign those variables.
	Related bool `json:"related"`
	// IncludeSecrets copies secret defaults and assignment values.
	// Otherwise those values are omitted and import keeps what is stored.
	IncludeSecrets bool `json:"include_secrets"`
}

// ConfigBundle is the file written by Export and read by Import.
type ConfigBundle struct {
	Format             string                    `json:"format"`
	Version            int                       `json:"version"`
	ServiceDefinitions []BundleServiceDefinition `json:"service_definitions,omitempty"`
	Variables          []BundleVariable          `json:"variables,omitempty"`
	Macros             []BundleMacro             `json:"macros,omitempty"`
	CLIObjects         []BundleCLIObject         `json:"cli_objects,omitempty"`
	ParameterObjects   []BundleParameterObject   `json:"parameter_objects,omitempty"`
}

type BundleServiceDefinition struct {
	Name            string                       `json:"name"`
	Description     string                       `json:"description,omitempty"`
	Schema          []models.FieldSchema         `json:"schema"`
	Interfaces      models.ServiceInterfacesSpec `json:"interfaces"`
	SyncSource      string                       `json:"sync_source,omitempty"`
	NetboxType      string                       `json:"netbox_type,omitempty"`
	ConnectionTypes []BundleConnectionType       `json:"connection_types"`
}

type BundleConnectionType struct {
	Name        string `json:"name"`
	SortOrder   int    `json:"sort_order,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	ImageBase64 string `json:"image_base64,omitempty"`
}

type BundleVariable struct {
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Description  string          `json:"description,omitempty"`
	DefaultValue json.RawMessage `json:"default_value,omitempty"`
	Constraints  json.RawMessage `json:"constraints,omitempty"`
	Secret       bool            `json:"secret,omitempty"`
	Required     bool            `json:"required,omitempty"`
	Platforms    json.RawMessage `json:"platforms,omitempty"`
}

type BundleMacro struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

type BundleCLIObject struct {
	Path        string             `json:"path"`
	Name        string             `json:"name"`
	Platform    string             `json:"platform,omitempty"`
	PayloadKind string             `json:"payload_kind,omitempty"`
	ServiceType string             `json:"service_type,omitempty"`
	Enabled     *bool              `json:"enabled,omitempty"`
	SortOrder   int                `json:"sort_order,omitempty"`
	Description string             `json:"description,omitempty"`
	Context     *models.CLIContext `json:"context,omitempty"`
	Features    []BundleCLIFeature `json:"features"`
}

type BundleCLIFeature struct {
	Name           string `json:"name"`
	SortOrder      int    `json:"sort_order,omitempty"`
	AddCommands    string `json:"add_commands,omitempty"`
	UpdateCommands string `json:"update_commands,omitempty"`
	RemoveCommands string `json:"remove_commands,omitempty"`
	RemoveAtRoot   bool   `json:"remove_at_root,omitempty"`
}

type BundleParameterObject struct {
	Path        string             `json:"path"`
	Name        string             `json:"name"`
	Enabled     *bool              `json:"enabled,omitempty"`
	SortOrder   int                `json:"sort_order,omitempty"`
	Description string             `json:"description,omitempty"`
	Platforms   []string           `json:"platforms,omitempty"`
	Assignments []BundleAssignment `json:"assignments"`
}

type BundleAssignment struct {
	Variable string          `json:"variable"`
	Value    json.RawMessage `json:"value,omitempty"`
}

type BundleImportResult struct {
	Created  []string `json:"created,omitempty"`
	Updated  []string `json:"updated,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

var (
	macroIncludeRe = regexp.MustCompile(`(?i)include\s*(?:\(\s*)?["']([^"']+)["']`)
	varsDotRe      = regexp.MustCompile(`\.Vars\.([A-Za-z_][A-Za-z0-9_]*)`)
	varsIndexRe    = regexp.MustCompile(`\.Vars\s*\[\s*["']([^"']+)["']\s*\]`)
	pngMagicBundle = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
)

type scopeMem struct {
	byID         map[uint]models.ConfigScope
	byParentName map[uint]map[string][]uint
	rootID       uint
}

func loadScopeMem(db *gorm.DB) (scopeMem, error) {
	rows, err := ListScopes(db)
	if err != nil {
		return scopeMem{}, err
	}
	m := scopeMem{
		byID:         make(map[uint]models.ConfigScope, len(rows)),
		byParentName: map[uint]map[string][]uint{},
	}
	for _, s := range rows {
		m.byID[s.ID] = s
		if s.ParentID == nil && s.Name == models.ConfigRootName {
			m.rootID = s.ID
		}
		if s.ParentID == nil {
			continue
		}
		kids := m.byParentName[*s.ParentID]
		if kids == nil {
			kids = map[string][]uint{}
			m.byParentName[*s.ParentID] = kids
		}
		kids[s.Name] = append(kids[s.Name], s.ID)
	}
	if m.rootID == 0 {
		return scopeMem{}, statusErr(500, "global config scope is missing")
	}
	return m, nil
}

func (m scopeMem) path(id uint) (string, error) {
	var parts []string
	seen := map[uint]bool{}
	cur := id
	for {
		if seen[cur] {
			return "", statusErr(400, "scope parent cycle")
		}
		seen[cur] = true
		s, ok := m.byID[cur]
		if !ok {
			return "", statusErr(404, "scope not found")
		}
		if strings.Contains(s.Name, "/") {
			return "", statusErrf(400, "scope name %q contains \"/\" and cannot be exported", s.Name)
		}
		if s.ParentID != nil {
			ids := m.byParentName[*s.ParentID][s.Name]
			if len(ids) > 1 {
				return "", statusErrf(400, "scope name %q is ambiguous under its parent", s.Name)
			}
		}
		parts = append(parts, s.Name)
		if s.ParentID == nil || *s.ParentID == 0 {
			break
		}
		cur = *s.ParentID
		if len(parts) > 64 {
			return "", statusErr(400, "scope path is too deep")
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "/"), nil
}

func (m scopeMem) idByPath(path string) (uint, error) {
	parts, err := splitScopePath(path)
	if err != nil {
		return 0, err
	}
	root := m.byID[m.rootID]
	if parts[0] != root.Name {
		return 0, statusErrf(400, "path %q must start with %s", path, root.Name)
	}
	id := m.rootID
	for _, name := range parts[1:] {
		ids := m.byParentName[id][name]
		if len(ids) == 0 {
			return 0, statusErrf(400, "path %q not found", path)
		}
		if len(ids) > 1 {
			return 0, statusErrf(400, "path %q is ambiguous", path)
		}
		id = ids[0]
	}
	return id, nil
}

func splitScopePath(path string) ([]string, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return nil, statusErr(400, "path is required")
	}
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" || p != strings.TrimSpace(p) {
			return nil, statusErrf(400, "invalid path %q", path)
		}
	}
	if parts[0] != models.ConfigRootName {
		return nil, statusErrf(400, "path %q must start with global", path)
	}
	return parts, nil
}

func ExportBundle(db *gorm.DB, req BundleExportRequest) (*ConfigBundle, error) {
	if len(req.ServiceDefinitions) == 0 && len(req.CLIObjects) == 0 &&
		len(req.Macros) == 0 && len(req.ParameterObjects) == 0 {
		return nil, statusErr(400, "select at least one service definition, CLI object, macro, or parameter object")
	}
	mem, err := loadScopeMem(db)
	if err != nil {
		return nil, err
	}
	types, err := ListServiceTypes(db)
	if err != nil {
		return nil, err
	}
	typeByName := map[string]*models.ServiceType{}
	typeByID := map[uint]*models.ServiceType{}
	for i := range types {
		typeByName[types[i].Name] = &types[i]
		typeByID[types[i].ID] = &types[i]
	}
	var macros []models.ConfigMacro
	if err := db.Order("name").Find(&macros).Error; err != nil {
		return nil, err
	}
	macroByName := map[string]models.ConfigMacro{}
	for _, row := range macros {
		macroByName[row.Name] = row
	}
	var defs []models.ConfigVariableDef
	if err := db.Find(&defs).Error; err != nil {
		return nil, err
	}
	defByID := map[uint]models.ConfigVariableDef{}
	defByName := map[string]models.ConfigVariableDef{}
	for _, d := range defs {
		defByID[d.ID] = d
		defByName[d.Name] = d
	}

	typeIDs := map[uint]struct{}{}
	for _, name := range uniqueStrings(req.ServiceDefinitions) {
		st := typeByName[name]
		if st == nil {
			return nil, statusErrf(400, "service definition %q not found", name)
		}
		typeIDs[st.ID] = struct{}{}
	}
	cliIDs := map[uint]struct{}{}
	for _, path := range uniqueStrings(req.CLIObjects) {
		id, err := mem.idByPath(path)
		if err != nil {
			return nil, err
		}
		s := mem.byID[id]
		if s.Kind != models.ConfigScopeKindCLI {
			return nil, statusErrf(400, "path %q is not a CLI object", path)
		}
		cliIDs[id] = struct{}{}
	}
	macroNames := map[string]struct{}{}
	for _, name := range uniqueStrings(req.Macros) {
		if _, ok := macroByName[name]; !ok {
			return nil, statusErrf(400, "macro %q not found", name)
		}
		macroNames[name] = struct{}{}
	}
	paramIDs := map[uint]struct{}{}
	for _, path := range uniqueStrings(req.ParameterObjects) {
		id, err := mem.idByPath(path)
		if err != nil {
			return nil, err
		}
		s := mem.byID[id]
		if s.Kind != models.ConfigScopeKindParameter {
			return nil, statusErrf(400, "path %q is not a parameter object", path)
		}
		paramIDs[id] = struct{}{}
	}

	if req.Related {
		addTranslationCLIs(mem, typeByID, typeIDs, cliIDs)
		addTypesForCLIs(mem, typeByID, cliIDs, typeIDs)
		addTranslationCLIs(mem, typeByID, typeIDs, cliIDs)
		if err := addMacroClosure(db, mem, macroByName, cliIDs, macroNames); err != nil {
			return nil, err
		}
		varNames := map[string]struct{}{}
		if err := collectTemplateVars(db, mem, macroByName, cliIDs, macroNames, varNames); err != nil {
			return nil, err
		}
		if err := addParametersForVars(db, mem, defByID, varNames, paramIDs); err != nil {
			return nil, err
		}
	}

	out := &ConfigBundle{Format: BundleFormat, Version: BundleVersion}
	if err := fillServiceDefinitions(db, typeByID, typeIDs, out); err != nil {
		return nil, err
	}
	if err := fillMacros(macroByName, macroNames, out); err != nil {
		return nil, err
	}
	if err := fillCLIObjects(db, mem, typeByID, cliIDs, out); err != nil {
		return nil, err
	}
	if err := fillParameters(db, mem, defByID, defByName, paramIDs, req.IncludeSecrets, out); err != nil {
		return nil, err
	}
	varNames := map[string]struct{}{}
	for _, p := range out.ParameterObjects {
		for _, a := range p.Assignments {
			varNames[a.Variable] = struct{}{}
		}
	}
	if req.Related {
		if err := collectTemplateVars(db, mem, macroByName, cliIDs, macroNames, varNames); err != nil {
			return nil, err
		}
	}
	fillVariables(defByName, varNames, req.IncludeSecrets, out)
	return out, nil
}

func addTranslationCLIs(mem scopeMem, typeByID map[uint]*models.ServiceType, typeIDs, cliIDs map[uint]struct{}) {
	for id, s := range mem.byID {
		if s.Kind != models.ConfigScopeKindCLI {
			continue
		}
		if s.ServiceTypeID != nil {
			if _, ok := typeIDs[*s.ServiceTypeID]; ok {
				cliIDs[id] = struct{}{}
			}
		}
		path, err := mem.path(id)
		if err != nil {
			continue
		}
		parts := strings.Split(path, "/")
		// global/_catalog/cli/<type>/<object>
		if len(parts) >= 5 && parts[1] == models.ConfigCatalogName && parts[2] == models.ConfigCatalogCLIName {
			for tid, st := range typeByID {
				if _, ok := typeIDs[tid]; ok && st.Name == parts[3] {
					cliIDs[id] = struct{}{}
				}
			}
		}
	}
}

func addTypesForCLIs(mem scopeMem, typeByID map[uint]*models.ServiceType, cliIDs, typeIDs map[uint]struct{}) {
	for id := range cliIDs {
		s := mem.byID[id]
		if s.ServiceTypeID != nil {
			if _, ok := typeByID[*s.ServiceTypeID]; ok {
				typeIDs[*s.ServiceTypeID] = struct{}{}
			}
		}
	}
}

func addMacroClosure(db *gorm.DB, mem scopeMem, macros map[string]models.ConfigMacro, cliIDs map[uint]struct{}, names map[string]struct{}) error {
	pending := mapKeys(names)
	for id := range cliIDs {
		text, err := loadCLIText(db, mem.byID[id])
		if err != nil {
			return err
		}
		pending = append(pending, macroNamesIn(text)...)
	}
	seen := map[string]struct{}{}
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		row, ok := macros[name]
		if !ok {
			continue
		}
		names[name] = struct{}{}
		if len(names) > 256 {
			return statusErr(400, "macro include closure is too large")
		}
		pending = append(pending, macroNamesIn(row.Body)...)
	}
	return nil
}

func collectTemplateVars(db *gorm.DB, mem scopeMem, macros map[string]models.ConfigMacro, cliIDs map[uint]struct{}, macroNames, dst map[string]struct{}) error {
	for id := range cliIDs {
		text, err := loadCLIText(db, mem.byID[id])
		if err != nil {
			return err
		}
		addVarNames(text, dst)
	}
	for name := range macroNames {
		addVarNames(macros[name].Body, dst)
	}
	return nil
}

func addParametersForVars(db *gorm.DB, mem scopeMem, defByID map[uint]models.ConfigVariableDef, varNames map[string]struct{}, paramIDs map[uint]struct{}) error {
	if len(varNames) == 0 {
		return nil
	}
	want := map[string]struct{}{}
	for name := range varNames {
		want[name] = struct{}{}
	}
	for id, s := range mem.byID {
		if s.Kind != models.ConfigScopeKindParameter {
			continue
		}
		var rows []models.ConfigAssignment
		if err := db.Where("scope_id = ?", id).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			def, ok := defByID[row.VariableDefID]
			if ok {
				if _, hit := want[def.Name]; hit {
					paramIDs[id] = struct{}{}
					break
				}
			}
		}
	}
	return nil
}

func fillServiceDefinitions(db *gorm.DB, typeByID map[uint]*models.ServiceType, ids map[uint]struct{}, out *ConfigBundle) error {
	var rows []BundleServiceDefinition
	for id := range ids {
		st := typeByID[id]
		if st == nil {
			return statusErr(400, "service definition not found")
		}
		schema := st.Schema
		if schema == nil {
			schema = []models.FieldSchema{}
		}
		item := BundleServiceDefinition{
			Name:            st.Name,
			Description:     st.Description,
			Schema:          schema,
			Interfaces:      st.Interfaces,
			SyncSource:      st.SyncSource,
			NetboxType:      st.NetboxType,
			ConnectionTypes: []BundleConnectionType{},
		}
		cts := append([]models.ServiceConnectionType(nil), st.ConnectionTypes...)
		sort.Slice(cts, func(i, j int) bool {
			if cts[i].SortOrder != cts[j].SortOrder {
				return cts[i].SortOrder < cts[j].SortOrder
			}
			return cts[i].Name < cts[j].Name
		})
		for _, ct := range cts {
			b := BundleConnectionType{Name: ct.Name, SortOrder: ct.SortOrder}
			if ct.HasImage {
				var full models.ServiceConnectionType
				if err := db.First(&full, ct.ID).Error; err != nil {
					return err
				}
				if len(full.Image) > 0 {
					b.ImageBase64 = base64.StdEncoding.EncodeToString(full.Image)
					b.ContentType = full.ContentType
				}
			}
			item.ConnectionTypes = append(item.ConnectionTypes, b)
		}
		rows = append(rows, item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	out.ServiceDefinitions = rows
	return nil
}

func fillMacros(macros map[string]models.ConfigMacro, names map[string]struct{}, out *ConfigBundle) error {
	var rows []BundleMacro
	for name := range names {
		row, ok := macros[name]
		if !ok {
			return statusErrf(400, "macro %q not found", name)
		}
		rows = append(rows, BundleMacro{Name: row.Name, Body: row.Body})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	out.Macros = rows
	return nil
}

func fillCLIObjects(db *gorm.DB, mem scopeMem, typeByID map[uint]*models.ServiceType, ids map[uint]struct{}, out *ConfigBundle) error {
	var rows []BundleCLIObject
	for id := range ids {
		s := mem.byID[id]
		path, err := mem.path(id)
		if err != nil {
			return err
		}
		feats, err := ListCLIFeatures(db, id)
		if err != nil {
			return err
		}
		enabled := s.Enabled
		item := BundleCLIObject{
			Path:        path,
			Name:        s.Name,
			Platform:    s.Platform,
			PayloadKind: s.PayloadKind,
			Enabled:     &enabled,
			SortOrder:   s.SortOrder,
			Description: s.Payload.Description,
			Context:     contextOrNil(s.Payload.Context),
			Features:    []BundleCLIFeature{},
		}
		if s.ServiceTypeID != nil {
			if st := typeByID[*s.ServiceTypeID]; st != nil {
				item.ServiceType = st.Name
			}
		}
		if item.ServiceType == "" {
			item.ServiceType = serviceTypeFromCatalogPath(path, typeByID)
		}
		for _, f := range feats {
			item.Features = append(item.Features, BundleCLIFeature{
				Name:           f.Name,
				SortOrder:      f.SortOrder,
				AddCommands:    f.AddCommands,
				UpdateCommands: f.UpdateCommands,
				RemoveCommands: f.RemoveCommands,
				RemoveAtRoot:   f.RemoveAtRoot,
			})
		}
		rows = append(rows, item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	out.CLIObjects = rows
	return nil
}

func serviceTypeFromCatalogPath(path string, typeByID map[uint]*models.ServiceType) string {
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[1] != models.ConfigCatalogName || parts[2] != models.ConfigCatalogCLIName {
		return ""
	}
	for _, st := range typeByID {
		if st.Name == parts[3] {
			return st.Name
		}
	}
	return ""
}

func fillParameters(db *gorm.DB, mem scopeMem, defByID map[uint]models.ConfigVariableDef, defByName map[string]models.ConfigVariableDef, ids map[uint]struct{}, includeSecrets bool, out *ConfigBundle) error {
	var rows []BundleParameterObject
	for id := range ids {
		s := mem.byID[id]
		path, err := mem.path(id)
		if err != nil {
			return err
		}
		var assigns []models.ConfigAssignment
		if err := db.Where("scope_id = ?", id).Find(&assigns).Error; err != nil {
			return err
		}
		enabled := s.Enabled
		item := BundleParameterObject{
			Path:        path,
			Name:        s.Name,
			Enabled:     &enabled,
			SortOrder:   s.SortOrder,
			Description: s.Payload.Description,
			Platforms:   s.Payload.Platforms,
			Assignments: []BundleAssignment{},
		}
		for _, a := range assigns {
			def, ok := defByID[a.VariableDefID]
			if !ok {
				continue
			}
			ba := BundleAssignment{Variable: def.Name}
			if isSecretDef(&def) && !includeSecrets {
				ba.Value = nil
			} else {
				ba.Value = append(json.RawMessage(nil), a.Value...)
			}
			item.Assignments = append(item.Assignments, ba)
			defByName[def.Name] = def
		}
		sort.Slice(item.Assignments, func(i, j int) bool {
			return item.Assignments[i].Variable < item.Assignments[j].Variable
		})
		rows = append(rows, item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	out.ParameterObjects = rows
	return nil
}

func fillVariables(defByName map[string]models.ConfigVariableDef, names map[string]struct{}, includeSecrets bool, out *ConfigBundle) {
	var rows []BundleVariable
	for name := range names {
		def, ok := defByName[name]
		if !ok {
			continue
		}
		item := BundleVariable{
			Name:        def.Name,
			Type:        def.Type,
			Description: def.Description,
			Constraints: append(json.RawMessage(nil), def.Constraints...),
			Secret:      def.Secret || def.Type == models.VarTypeSecret,
			Required:    def.Required,
			Platforms:   append(json.RawMessage(nil), def.Platforms...),
		}
		if isSecretDef(&def) && !includeSecrets {
			item.DefaultValue = nil
		} else {
			item.DefaultValue = append(json.RawMessage(nil), def.DefaultValue...)
		}
		rows = append(rows, item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	out.Variables = rows
}

func ImportBundle(db *gorm.DB, b ConfigBundle) (*BundleImportResult, error) {
	if b.Format != BundleFormat {
		return nil, statusErr(400, "unsupported config bundle")
	}
	if b.Version != BundleVersion {
		return nil, statusErrf(400, "unsupported config bundle version %d", b.Version)
	}
	if len(b.ServiceDefinitions) == 0 && len(b.Variables) == 0 && len(b.Macros) == 0 &&
		len(b.CLIObjects) == 0 && len(b.ParameterObjects) == 0 {
		return nil, statusErr(400, "bundle has nothing to import")
	}
	if err := rejectDuplicateBundleKeys(b); err != nil {
		return nil, err
	}
	res := &BundleImportResult{}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := importVariables(tx, b.Variables, res); err != nil {
			return err
		}
		if err := importServiceDefinitions(tx, b.ServiceDefinitions, res); err != nil {
			return err
		}
		if err := importMacros(tx, b.Macros, res); err != nil {
			return err
		}
		if err := importCLIObjects(tx, b.CLIObjects, res); err != nil {
			return err
		}
		if err := importParameters(tx, b.ParameterObjects, res); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(res.Created)
	sort.Strings(res.Updated)
	sort.Strings(res.Warnings)
	return res, nil
}

func rejectDuplicateBundleKeys(b ConfigBundle) error {
	seen := map[string]struct{}{}
	for _, row := range b.ServiceDefinitions {
		if !markKey(seen, "service:"+row.Name) {
			return statusErrf(400, "duplicate service definition %q", row.Name)
		}
	}
	for _, row := range b.Variables {
		if !markKey(seen, "var:"+row.Name) {
			return statusErrf(400, "duplicate variable %q", row.Name)
		}
	}
	for _, row := range b.Macros {
		if !markKey(seen, "macro:"+row.Name) {
			return statusErrf(400, "duplicate macro %q", row.Name)
		}
	}
	for _, row := range b.CLIObjects {
		if !markKey(seen, "cli:"+row.Path) {
			return statusErrf(400, "duplicate CLI object path %q", row.Path)
		}
		if row.ServiceType != "" {
			key := "cli-type:" + row.ServiceType + "\x00" + NormalizePlatform(row.Platform)
			if !markKey(seen, key) {
				return statusErrf(400, "duplicate CLI object for %s/%s", row.ServiceType, NormalizePlatform(row.Platform))
			}
		}
	}
	for _, row := range b.ParameterObjects {
		if !markKey(seen, "param:"+row.Path) {
			return statusErrf(400, "duplicate parameter object path %q", row.Path)
		}
	}
	return nil
}

func importVariables(tx *gorm.DB, rows []BundleVariable, res *BundleImportResult) error {
	for _, row := range rows {
		row.Name = strings.TrimSpace(row.Name)
		row.Type = strings.TrimSpace(row.Type)
		if row.Name == "" {
			return statusErr(400, "variable name is required")
		}
		label := "variable " + row.Name
		def := models.ConfigVariableDef{
			Name: row.Name, Type: row.Type, Description: row.Description,
			DefaultValue: emptyJSONNull(row.DefaultValue),
			Constraints:  emptyJSONNull(row.Constraints),
			Secret:       row.Secret || row.Type == models.VarTypeSecret,
			Required:     row.Required,
			Platforms:    emptyJSONNull(row.Platforms),
		}
		var existing models.ConfigVariableDef
		err := tx.Where("name = ?", row.Name).First(&existing).Error
		if err != nil && !isNotFound(err) {
			return err
		}
		if err == nil {
			if existing.Type != def.Type {
				return statusErrf(400, "variable %q is %s, file has %s", row.Name, existing.Type, def.Type)
			}
			if isSecretDef(&def) && SecretDefaultUnchanged(row.DefaultValue) {
				def.DefaultValue = existing.DefaultValue
			}
			def.ID = existing.ID
			def.CreatedAt = existing.CreatedAt
			if err := ValidateVariableDef(&def); err != nil {
				return wrapBundle(label, err)
			}
			if err := tx.Save(&def).Error; err != nil {
				return wrapBundle(label, err)
			}
			res.Updated = append(res.Updated, label)
			continue
		}
		if err := ValidateVariableDef(&def); err != nil {
			return wrapBundle(label, err)
		}
		if err := tx.Create(&def).Error; err != nil {
			return wrapBundle(label, err)
		}
		res.Created = append(res.Created, label)
	}
	return nil
}

func importServiceDefinitions(tx *gorm.DB, rows []BundleServiceDefinition, res *BundleImportResult) error {
	for _, row := range rows {
		row.Name = strings.TrimSpace(row.Name)
		if row.Name == "" {
			return statusErr(400, "service definition name is required")
		}
		label := "service definition " + row.Name
		schema := row.Schema
		if schema == nil {
			schema = []models.FieldSchema{}
		}
		cts := row.ConnectionTypes
		if cts == nil {
			cts = []BundleConnectionType{}
		}
		var existing models.ServiceType
		err := tx.Where("name = ?", row.Name).First(&existing).Error
		if err != nil && !isNotFound(err) {
			return err
		}
		created := isNotFound(err)
		st := existing
		st.Name = row.Name
		st.Description = row.Description
		st.Schema = schema
		st.Interfaces = row.Interfaces
		st.SyncSource = row.SyncSource
		st.NetboxType = row.NetboxType
		if err := ValidateServiceType(&st); err != nil {
			return wrapBundle(label, err)
		}
		if created {
			st = models.ServiceType{
				Name: row.Name, Description: row.Description,
				Schema: schema, Interfaces: row.Interfaces,
				SyncSource: row.SyncSource, NetboxType: row.NetboxType,
			}
			if err := ValidateServiceType(&st); err != nil {
				return wrapBundle(label, err)
			}
			if err := tx.Create(&st).Error; err != nil {
				return wrapBundle(label, err)
			}
			res.Created = append(res.Created, label)
		} else {
			if err := tx.Save(&st).Error; err != nil {
				return wrapBundle(label, err)
			}
			res.Updated = append(res.Updated, label)
		}
		if err := applyBundleConnectionTypes(tx, st.ID, cts); err != nil {
			return wrapBundle(label, err)
		}
		if _, err := CatalogCLITypeFolder(tx, st.Name); err != nil {
			return wrapBundle(label, err)
		}
	}
	return nil
}

func applyBundleConnectionTypes(tx *gorm.DB, typeID uint, rows []BundleConnectionType) error {
	var existing []models.ServiceConnectionType
	if err := tx.Where("service_type_id = ?", typeID).Find(&existing).Error; err != nil {
		return err
	}
	byName := map[string]models.ServiceConnectionType{}
	for _, row := range existing {
		byName[row.Name] = row
	}
	dtos := make([]models.ServiceConnectionTypeDTO, len(rows))
	for i, row := range rows {
		name := strings.TrimSpace(row.Name)
		dtos[i] = models.ServiceConnectionTypeDTO{Name: name, SortOrder: i}
		if prev, ok := byName[name]; ok {
			dtos[i].ID = prev.ID
		}
	}
	if err := ReplaceConnectionTypes(tx, typeID, dtos); err != nil {
		return err
	}
	for _, row := range rows {
		if strings.TrimSpace(row.ImageBase64) == "" {
			continue
		}
		data, contentType, err := decodeBundleImage(row.ImageBase64, row.ContentType)
		if err != nil {
			return err
		}
		var saved models.ServiceConnectionType
		if err := tx.Where("service_type_id = ? AND name = ?", typeID, strings.TrimSpace(row.Name)).First(&saved).Error; err != nil {
			return err
		}
		saved.Image = data
		saved.ContentType = contentType
		if err := tx.Select("Image", "ContentType").Save(&saved).Error; err != nil {
			return err
		}
	}
	return nil
}

func importMacros(tx *gorm.DB, rows []BundleMacro, res *BundleImportResult) error {
	for _, row := range rows {
		row.Name = strings.TrimSpace(row.Name)
		if row.Name == "" {
			return statusErr(400, "macro name is required")
		}
		label := "macro " + row.Name
		var existing models.ConfigMacro
		err := tx.Where("name = ?", row.Name).First(&existing).Error
		if err != nil && !isNotFound(err) {
			return err
		}
		if err == nil {
			existing.Body = row.Body
			if err := tx.Save(&existing).Error; err != nil {
				return wrapBundle(label, err)
			}
			res.Updated = append(res.Updated, label)
			continue
		}
		if err := tx.Create(&models.ConfigMacro{Name: row.Name, Body: row.Body}).Error; err != nil {
			return wrapBundle(label, err)
		}
		res.Created = append(res.Created, label)
	}
	return nil
}

func importCLIObjects(tx *gorm.DB, rows []BundleCLIObject, res *BundleImportResult) error {
	for _, row := range rows {
		name, err := objectName(row.Path, row.Name)
		if err != nil {
			return err
		}
		row.Name = name
		label := "CLI object " + strings.Trim(strings.TrimSpace(row.Path), "/")
		parent, err := ensureParent(tx, row.Path)
		if err != nil {
			return wrapBundle(label, err)
		}
		var typeID *uint
		if strings.TrimSpace(row.ServiceType) != "" {
			st, err := LookupServiceType(tx, strings.TrimSpace(row.ServiceType))
			if err != nil {
				return wrapBundle(label, err)
			}
			typeID = &st.ID
		}
		existing, err := findCLIForImport(tx, parent.ID, row.Name, typeID, row.Platform)
		if err != nil {
			return wrapBundle(label, err)
		}
		payload := models.ConfigScopePayload{Description: strings.TrimSpace(row.Description)}
		if existing != nil {
			payload = existing.Payload
			payload.Description = strings.TrimSpace(row.Description)
		}
		payload.Context = contextOrNil(row.Context)
		kind := strings.TrimSpace(row.PayloadKind)
		if kind == "" {
			kind = models.PayloadKindCLI
		}
		enabled := true
		if row.Enabled != nil {
			enabled = *row.Enabled
		}
		obj := models.ConfigScope{
			ParentID:      &parent.ID,
			Name:          row.Name,
			Kind:          models.ConfigScopeKindCLI,
			ServiceTypeID: typeID,
			Platform:      row.Platform,
			PayloadKind:   kind,
			Enabled:       enabled,
			SortOrder:     row.SortOrder,
			Payload:       payload,
		}
		if existing == nil {
			created, err := CreateScope(tx, &obj)
			if err != nil {
				return wrapBundle(label, err)
			}
			created.Enabled = enabled
			created.SortOrder = row.SortOrder
			if err := tx.Save(created).Error; err != nil {
				return wrapBundle(label, err)
			}
			obj = *created
			res.Created = append(res.Created, label)
		} else {
			obj.ID = existing.ID
			obj.CreatedAt = existing.CreatedAt
			obj.SeedChecksum = existing.SeedChecksum
			if err := assertParentKind(&obj, parent); err != nil {
				return wrapBundle(label, err)
			}
			if err := normalizeCLIScope(&obj); err != nil {
				return wrapBundle(label, err)
			}
			if err := assertCLIUnique(tx, &obj, obj.ID); err != nil {
				return wrapBundle(label, err)
			}
			if err := tx.Save(&obj).Error; err != nil {
				return wrapBundle(label, err)
			}
			res.Updated = append(res.Updated, label)
		}
		if err := replaceCLIFeatures(tx, obj.ID, row.Features); err != nil {
			return wrapBundle(label, err)
		}
	}
	return nil
}

func findCLIForImport(tx *gorm.DB, parentID uint, name string, typeID *uint, platform string) (*models.ConfigScope, error) {
	var byPath models.ConfigScope
	err := tx.Where("parent_id = ? AND name = ?", parentID, name).First(&byPath).Error
	var pathHit *models.ConfigScope
	if err == nil {
		if byPath.Kind != models.ConfigScopeKindCLI {
			return nil, statusErrf(400, "%q exists and is a %s", name, byPath.Kind)
		}
		pathHit = &byPath
	} else if !isNotFound(err) {
		return nil, err
	}
	var typeHit *models.ConfigScope
	if typeID != nil && *typeID != 0 {
		hit, err := lookupCLIObjectByTypeID(tx, *typeID, platform, false)
		if err != nil {
			return nil, err
		}
		typeHit = hit
	}
	if pathHit != nil && typeHit != nil && pathHit.ID != typeHit.ID {
		return nil, statusErr(409, "CLI path and service type/platform point at different objects")
	}
	if typeHit != nil {
		return typeHit, nil
	}
	return pathHit, nil
}

func replaceCLIFeatures(tx *gorm.DB, scopeID uint, rows []BundleCLIFeature) error {
	if len(rows) > maxCLIFeatures {
		return statusErrf(400, "at most %d features per CLI object", maxCLIFeatures)
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			return statusErr(400, "feature name is required")
		}
		if _, ok := seen[name]; ok {
			return statusErrf(400, "duplicate feature %q", name)
		}
		seen[name] = struct{}{}
	}
	if err := tx.Where("scope_id = ?", scopeID).Delete(&models.ConfigCLIFeature{}).Error; err != nil {
		return err
	}
	for _, row := range rows {
		feat := models.ConfigCLIFeature{
			Name:           strings.TrimSpace(row.Name),
			SortOrder:      row.SortOrder,
			AddCommands:    row.AddCommands,
			UpdateCommands: row.UpdateCommands,
			RemoveCommands: row.RemoveCommands,
			RemoveAtRoot:   row.RemoveAtRoot,
		}
		if _, err := CreateCLIFeature(tx, scopeID, &feat); err != nil {
			return err
		}
	}
	return nil
}

func importParameters(tx *gorm.DB, rows []BundleParameterObject, res *BundleImportResult) error {
	for _, row := range rows {
		name, err := objectName(row.Path, row.Name)
		if err != nil {
			return err
		}
		row.Name = name
		label := "parameter object " + strings.Trim(strings.TrimSpace(row.Path), "/")
		parent, err := ensureParent(tx, row.Path)
		if err != nil {
			return wrapBundle(label, err)
		}
		var existing models.ConfigScope
		err = tx.Where("parent_id = ? AND name = ? AND kind = ?", parent.ID, row.Name, models.ConfigScopeKindParameter).First(&existing).Error
		if err != nil && !isNotFound(err) {
			return wrapBundle(label, err)
		}
		var other int64
		if err := tx.Model(&models.ConfigScope{}).
			Where("parent_id = ? AND name = ? AND kind <> ?", parent.ID, row.Name, models.ConfigScopeKindParameter).
			Count(&other).Error; err != nil {
			return err
		}
		if other > 0 {
			return statusErrf(400, "%s: %q exists and is not a parameter object", label, row.Name)
		}
		enabled := true
		if row.Enabled != nil {
			enabled = *row.Enabled
		}
		payload := models.ConfigScopePayload{
			Description: strings.TrimSpace(row.Description),
			Platforms:   row.Platforms,
		}
		var scope models.ConfigScope
		if isNotFound(err) {
			created, err := CreateScope(tx, &models.ConfigScope{
				ParentID:  &parent.ID,
				Name:      row.Name,
				Kind:      models.ConfigScopeKindParameter,
				SortOrder: row.SortOrder,
				Payload:   payload,
			})
			if err != nil {
				return wrapBundle(label, err)
			}
			created.Enabled = enabled
			created.SortOrder = row.SortOrder
			created.Payload = payload
			if err := tx.Save(created).Error; err != nil {
				return wrapBundle(label, err)
			}
			scope = *created
			res.Created = append(res.Created, label)
		} else {
			probe := models.ConfigScope{Kind: models.ConfigScopeKindParameter, ParentID: &parent.ID}
			if err := assertParentKind(&probe, parent); err != nil {
				return wrapBundle(label, err)
			}
			existing.Enabled = enabled
			existing.SortOrder = row.SortOrder
			existing.Payload.Description = payload.Description
			existing.Payload.Platforms = payload.Platforms
			if err := tx.Save(&existing).Error; err != nil {
				return wrapBundle(label, err)
			}
			scope = existing
			res.Updated = append(res.Updated, label)
		}
		if err := replaceParameterAssignments(tx, &scope, row.Assignments, label, res); err != nil {
			return err
		}
	}
	return nil
}

func replaceParameterAssignments(tx *gorm.DB, scope *models.ConfigScope, rows []BundleAssignment, label string, res *BundleImportResult) error {
	seen := map[string]struct{}{}
	keep := map[uint]struct{}{}
	for _, row := range rows {
		name := strings.TrimSpace(row.Variable)
		if name == "" {
			return statusErrf(400, "%s: assignment variable is required", label)
		}
		if _, ok := seen[name]; ok {
			return statusErrf(400, "%s: duplicate assignment %q", label, name)
		}
		seen[name] = struct{}{}
		var def models.ConfigVariableDef
		if err := tx.Where("name = ?", name).First(&def).Error; err != nil {
			if isNotFound(err) {
				return statusErrf(400, "%s: variable %q not found", label, name)
			}
			return err
		}
		if isSecretDef(&def) && SecretDefaultUnchanged(row.Value) {
			existing, err := assignmentAt(tx, def.ID, scope.ID)
			if err != nil {
				return err
			}
			if existing == nil {
				if def.Required {
					return statusErrf(400, "%s: secret value is required for %q", label, name)
				}
				res.Warnings = append(res.Warnings, label+": skipped secret variable "+name+" (no value in the file)")
				continue
			}
			keep[def.ID] = struct{}{}
			continue
		}
		if _, err := UpsertAssignment(tx, def.ID, scope.ID, row.Value); err != nil {
			return wrapBundle(label, err)
		}
		keep[def.ID] = struct{}{}
	}
	var current []models.ConfigAssignment
	if err := tx.Where("scope_id = ?", scope.ID).Find(&current).Error; err != nil {
		return err
	}
	for _, row := range current {
		if _, ok := keep[row.VariableDefID]; ok {
			continue
		}
		if err := DeleteAssignment(tx, row.ID); err != nil {
			return wrapBundle(label, err)
		}
	}
	return nil
}

func ensureParent(db *gorm.DB, objectPath string) (*models.ConfigScope, error) {
	parts, err := splitScopePath(objectPath)
	if err != nil {
		return nil, err
	}
	if len(parts) < 2 {
		return nil, statusErrf(400, "path %q must include the object name", objectPath)
	}
	return ensurePath(db, parts[:len(parts)-1], strings.Trim(strings.TrimSpace(objectPath), "/"))
}

func ensurePath(db *gorm.DB, parts []string, full string) (*models.ConfigScope, error) {
	root, err := RootScope(db)
	if err != nil {
		return nil, err
	}
	if parts[0] != root.Name {
		return nil, statusErrf(400, "path %q must start with %s", full, root.Name)
	}
	node := root
	for _, name := range parts[1:] {
		rows, err := childrenNamed(db, node.ID, name)
		if err != nil {
			return nil, err
		}
		if len(rows) > 1 {
			return nil, statusErrf(400, "path %q is ambiguous at %q", full, name)
		}
		if len(rows) == 1 {
			s := rows[0]
			node = &s
			continue
		}
		created, err := createMissingFolder(db, node, name, full)
		if err != nil {
			return nil, err
		}
		node = created
	}
	return node, nil
}

func createMissingFolder(db *gorm.DB, parent *models.ConfigScope, name, full string) (*models.ConfigScope, error) {
	if !organizationalParentKind(parent.Kind) {
		return nil, statusErrf(400, "path %q: %q is not in the config tree", full, name)
	}
	var devices int64
	if err := db.Model(&models.Device{}).Where("name = ?", name).Count(&devices).Error; err != nil {
		return nil, err
	}
	if devices > 0 {
		return nil, statusErrf(400, "device %q is not attached in the config tree", name)
	}
	var services int64
	if err := db.Model(&models.Service{}).Where("service_id = ?", name).Count(&services).Error; err != nil {
		return nil, err
	}
	if services > 0 {
		return nil, statusErrf(400, "service %q is not in the config tree", name)
	}
	return CreateScope(db, &models.ConfigScope{
		ParentID: &parent.ID,
		Name:     name,
		Kind:     models.ConfigScopeKindFolder,
	})
}

func childrenNamed(db *gorm.DB, parentID uint, name string) ([]models.ConfigScope, error) {
	var rows []models.ConfigScope
	err := db.Where("parent_id = ? AND name = ?", parentID, name).Find(&rows).Error
	return rows, err
}

func objectName(path, name string) (string, error) {
	parts, err := splitScopePath(path)
	if err != nil {
		return "", err
	}
	if len(parts) < 2 {
		return "", statusErrf(400, "path %q must include the object name", path)
	}
	last := parts[len(parts)-1]
	name = strings.TrimSpace(name)
	if name == "" {
		return last, nil
	}
	if name != last {
		return "", statusErrf(400, "path %q ends with %q, name is %q", strings.Trim(path, "/"), last, name)
	}
	return name, nil
}

func loadCLIText(db *gorm.DB, s models.ConfigScope) (string, error) {
	feats, err := ListCLIFeatures(db, s.ID)
	if err != nil {
		return "", err
	}
	return cliTextFrom(s, feats), nil
}

func cliTextFrom(s models.ConfigScope, feats []models.ConfigCLIFeature) string {
	var b strings.Builder
	if s.Payload.Context != nil {
		b.WriteString(s.Payload.Context.Pattern)
		b.WriteString("\n")
		b.WriteString(s.Payload.Context.Enter)
		b.WriteString("\n")
		b.WriteString(s.Payload.Context.Exit)
		b.WriteString("\n")
	}
	for _, f := range feats {
		b.WriteString(f.AddCommands)
		b.WriteString("\n")
		b.WriteString(f.UpdateCommands)
		b.WriteString("\n")
		b.WriteString(f.RemoveCommands)
		b.WriteString("\n")
	}
	return b.String()
}

func macroNamesIn(text string) []string {
	matches := macroIncludeRe.FindAllStringSubmatch(text, -1)
	var out []string
	for _, m := range matches {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			out = append(out, m[1])
		}
	}
	return out
}

func addVarNames(text string, dst map[string]struct{}) {
	for _, re := range []*regexp.Regexp{varsDotRe, varsIndexRe} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 && m[1] != "" {
				dst[m[1]] = struct{}{}
			}
		}
	}
}

func contextOrNil(c *models.CLIContext) *models.CLIContext {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(c.Pattern) == "" && strings.TrimSpace(c.Enter) == "" &&
		strings.TrimSpace(c.Exit) == "" && len(c.Captures) == 0 {
		return nil
	}
	cp := *c
	return &cp
}

func decodeBundleImage(encoded, declared string) ([]byte, string, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		default:
			return r
		}
	}, encoded)
	data, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, "", statusErr(400, "connection type image is not base64")
	}
	contentType, err := validateBundleImage(data, declared)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

func validateBundleImage(data []byte, declared string) (string, error) {
	if len(data) > maxBundleImage {
		return "", statusErr(400, "image exceeds 512KiB")
	}
	declared = strings.ToLower(strings.TrimSpace(declared))
	if i := strings.Index(declared, ";"); i >= 0 {
		declared = strings.TrimSpace(declared[:i])
	}
	if strings.Contains(declared, "svg") || bundleLooksLikeSVG(data) {
		return "", statusErr(400, "SVG images are not allowed")
	}
	var detected string
	switch {
	case bytes.HasPrefix(data, pngMagicBundle):
		detected = "image/png"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		detected = "image/webp"
	default:
		return "", statusErr(400, "image must be PNG or WebP")
	}
	if declared != "" && declared != "application/octet-stream" && declared != detected {
		return "", statusErr(400, "content type does not match image data")
	}
	return detected, nil
}

func bundleLooksLikeSVG(data []byte) bool {
	s := strings.TrimSpace(string(data))
	if len(s) > 256 {
		s = s[:256]
	}
	ls := strings.ToLower(s)
	return strings.Contains(ls, "<svg") || strings.HasPrefix(ls, "<?xml")
}

func emptyJSONNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func markKey(seen map[string]struct{}, key string) bool {
	if _, ok := seen[key]; ok {
		return false
	}
	seen[key] = struct{}{}
	return true
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func wrapBundle(label string, err error) error {
	if err == nil {
		return nil
	}
	if se := AsStatusError(err); se != nil {
		return statusErr(se.Status, label+": "+se.Message)
	}
	return statusErr(400, label+": "+err.Error())
}
