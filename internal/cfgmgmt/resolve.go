package cfgmgmt

import (
	"encoding/json"
	"errors"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// ResolvedVar is one variable's winning value and the scope it came from.
type ResolvedVar struct {
	Name        string
	Value       any
	Source      *models.ConfigScope
	FromDefault bool
	Secret      bool
	Required    bool
	Type        string
	Err         error
}

const maxScopeDepth = 64

// WalkParents returns start then each ancestor up to root. A cycle is an error.
func WalkParents(db *gorm.DB, start *models.ConfigScope) ([]models.ConfigScope, error) {
	if start == nil {
		return nil, statusErr(400, "missing scope")
	}
	seen := map[uint]bool{}
	var chain []models.ConfigScope
	cur := *start
	for {
		if seen[cur.ID] {
			return nil, statusErr(400, "scope parent cycle")
		}
		seen[cur.ID] = true
		chain = append(chain, cur)
		if len(chain) > maxScopeDepth {
			return nil, statusErr(400, "scope parent chain too deep")
		}
		if cur.ParentID == nil {
			return chain, nil
		}
		var next models.ConfigScope
		if err := db.First(&next, *cur.ParentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, statusErrf(400, "scope %d parent %d not found", cur.ID, *cur.ParentID)
			}
			return nil, err
		}
		cur = next
	}
}

// WouldCycle reports whether setting node's parent to newParentID would loop.
func WouldCycle(db *gorm.DB, nodeID uint, newParentID uint) (bool, error) {
	if newParentID == 0 {
		return false, nil
	}
	if newParentID == nodeID {
		return true, nil
	}
	var parent models.ConfigScope
	if err := db.First(&parent, newParentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, statusErr(400, "parent scope not found")
		}
		return false, err
	}
	chain, err := WalkParents(db, &parent)
	if err != nil {
		return false, err
	}
	for _, s := range chain {
		if s.ID == nodeID {
			return true, nil
		}
	}
	return false, nil
}

func scopeByInterfaceID(db *gorm.DB, interfaceID uint) (*models.ConfigScope, error) {
	var s models.ConfigScope
	err := db.Where("kind = ? AND interface_id = ?", models.ConfigScopeKindInterface, interfaceID).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return nil, nil
}

func scopeByDeviceID(db *gorm.DB, deviceID uint) (*models.ConfigScope, error) {
	var s models.ConfigScope
	err := db.Where("kind = ? AND device_id = ?", models.ConfigScopeKindDevice, deviceID).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return nil, nil
}

// StartScope for an interface: interface node, else device node, else global.
func StartScope(db *gorm.DB, interfaceID uint) (*models.ConfigScope, error) {
	if s, err := scopeByInterfaceID(db, interfaceID); err != nil {
		return nil, err
	} else if s != nil {
		return s, nil
	}
	var iface models.Interface
	if err := db.First(&iface, interfaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "interface not found")
		}
		return nil, err
	}
	if s, err := scopeByDeviceID(db, iface.DeviceID); err != nil {
		return nil, err
	} else if s != nil {
		return s, nil
	}
	return RootScope(db)
}

func loadVarDef(db *gorm.DB, name string) (*models.ConfigVariableDef, error) {
	var def models.ConfigVariableDef
	err := db.Where("name = ?", name).First(&def).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErrf(404, "unknown variable %q", name)
		}
		return nil, err
	}
	return &def, nil
}

func assignmentAt(db *gorm.DB, defID, scopeID uint) (*models.ConfigAssignment, error) {
	var a models.ConfigAssignment
	err := db.Where("variable_def_id = ? AND scope_id = ?", defID, scopeID).First(&a).Error
	if err == nil {
		return &a, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, err
}

func interfacePlatform(db *gorm.DB, interfaceID uint) string {
	var iface models.Interface
	if err := db.First(&iface, interfaceID).Error; err != nil {
		return ""
	}
	var dev models.Device
	if err := db.First(&dev, iface.DeviceID).Error; err != nil {
		return ""
	}
	return dev.Platform
}

func assignmentValueAt(db *gorm.DB, def *models.ConfigVariableDef, scope *models.ConfigScope) (any, bool, error) {
	a, err := assignmentAt(db, def.ID, scope.ID)
	if err != nil {
		return nil, false, err
	}
	if a == nil {
		return nil, false, nil
	}
	v, err := TypeCheckRaw(def, a.Value)
	if err != nil {
		return nil, false, statusErr(400, err.Error())
	}
	if v == nil {
		return nil, false, nil
	}
	return v, true, nil
}

func parameterChildren(db *gorm.DB, parentID uint) ([]models.ConfigScope, error) {
	var rows []models.ConfigScope
	err := db.Where("parent_id = ? AND kind = ?", parentID, models.ConfigScopeKindParameter).
		Order("sort_order DESC, name DESC").
		Find(&rows).Error
	return rows, err
}

func parameterObjectAllowed(p *models.ConfigScope, platform string) bool {
	if p == nil || !p.Enabled {
		return false
	}
	return objectPlatformsAllowed(p.Payload.Platforms, platform)
}

// objectPlatformsAllowed is the parameter-object platform filter. An empty
// list matches every device. sros-md also matches an object that lists sros.
func objectPlatformsAllowed(plats []string, platform string) bool {
	if len(plats) == 0 {
		return true
	}
	if platform == "" {
		return true
	}
	p := NormalizePlatform(platform)
	for _, x := range plats {
		n := NormalizePlatform(x)
		if n == p {
			return true
		}
		if p == "sros-md" && n == "sros" {
			return true
		}
	}
	return false
}

func resolveDefAt(db *gorm.DB, def *models.ConfigVariableDef, start *models.ConfigScope, platform string) (any, *models.ConfigScope, error) {
	chain, err := WalkParents(db, start)
	if err != nil {
		return nil, nil, err
	}
	for i := range chain {
		node := &chain[i]
		params, err := parameterChildren(db, node.ID)
		if err != nil {
			return nil, nil, err
		}
		for j := range params {
			p := &params[j]
			if !parameterObjectAllowed(p, platform) {
				continue
			}
			v, ok, err := assignmentValueAt(db, def, p)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				return v, p, nil
			}
		}
		v, ok, err := assignmentValueAt(db, def, node)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			return v, node, nil
		}
	}
	if len(def.DefaultValue) > 0 && string(def.DefaultValue) != "null" {
		v, err := TypeCheckRaw(def, def.DefaultValue)
		if err != nil {
			return nil, nil, statusErr(400, err.Error())
		}
		if v != nil {
			return v, nil, nil
		}
	}
	if def.Required {
		return nil, nil, statusErrf(400, "required variable %q has no value", def.Name)
	}
	return nil, nil, nil
}

// Resolve walks from the interface's start scope to root and returns the
// first assignment of varName. Falls back to the def's DefaultValue.
func Resolve(db *gorm.DB, interfaceID uint, varName string) (any, *models.ConfigScope, error) {
	def, err := loadVarDef(db, varName)
	if err != nil {
		return nil, nil, err
	}
	if !platformAllowed(def, interfacePlatform(db, interfaceID)) {
		return nil, nil, nil
	}
	start, err := StartScope(db, interfaceID)
	if err != nil {
		return nil, nil, err
	}
	return resolveDefAt(db, def, start, interfacePlatform(db, interfaceID))
}

// ResolveAll returns every variable def resolved at interfaceID. Required
// vars with no value are included with Err set rather than failing the batch.
func ResolveAll(db *gorm.DB, interfaceID uint) ([]ResolvedVar, error) {
	var defs []models.ConfigVariableDef
	if err := db.Order("name").Find(&defs).Error; err != nil {
		return nil, err
	}
	out := make([]ResolvedVar, 0, len(defs))
	for i := range defs {
		def := &defs[i]
		if !platformAllowed(def, interfacePlatform(db, interfaceID)) {
			continue
		}
		rv := ResolvedVar{Name: def.Name, Secret: def.Secret || def.Type == models.VarTypeSecret, Required: def.Required, Type: def.Type}
		v, src, err := Resolve(db, interfaceID, def.Name)
		if err != nil {
			rv.Err = err
			out = append(out, rv)
			continue
		}
		rv.Value = v
		rv.Source = src
		rv.FromDefault = src == nil && v != nil
		out = append(out, rv)
	}
	return out, nil
}

// ResolveMap is ResolveAll as name → value, skipping vars that failed.
func ResolveMap(db *gorm.DB, interfaceID uint) (map[string]any, error) {
	all, err := ResolveAll(db, interfaceID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any, len(all))
	for _, rv := range all {
		if rv.Err != nil {
			continue
		}
		if rv.Value != nil {
			m[rv.Name] = rv.Value
		}
	}
	return m, nil
}

func RedactSecrets(vars []ResolvedVar) []ResolvedVar {
	out := make([]ResolvedVar, len(vars))
	copy(out, vars)
	for i := range out {
		if out[i].Secret && out[i].Value != nil {
			out[i].Value = "***"
		}
	}
	return out
}

func isSecretDef(def *models.ConfigVariableDef) bool {
	return def.Secret || def.Type == models.VarTypeSecret
}

func RedactVariableSecrets(def *models.ConfigVariableDef) {
	if def == nil || !isSecretDef(def) {
		return
	}
	// Omit rather than `"***"` so a subsequent PUT cannot persist the placeholder.
	def.DefaultValue = nil
}

// SecretDefaultUnchanged reports that a write should keep the stored secret
// default: the client omitted it, sent JSON null, or echoed the redaction
// placeholder.
func SecretDefaultUnchanged(raw []byte) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	s, ok := v.(string)
	return ok && s == "***"
}

func RedactAssignmentSecrets(db *gorm.DB, rows []models.ConfigAssignment) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(rows))
	seen := map[uint]bool{}
	for _, r := range rows {
		if !seen[r.VariableDefID] {
			seen[r.VariableDefID] = true
			ids = append(ids, r.VariableDefID)
		}
	}
	var defs []models.ConfigVariableDef
	if err := db.Where("id IN ?", ids).Find(&defs).Error; err != nil {
		return err
	}
	secret := map[uint]bool{}
	for i := range defs {
		if isSecretDef(&defs[i]) {
			secret[defs[i].ID] = true
		}
	}
	for i := range rows {
		if secret[rows[i].VariableDefID] && len(rows[i].Value) > 0 && string(rows[i].Value) != "null" {
			rows[i].Value = []byte(`"***"`)
		}
	}
	return nil
}

// ResolveMapForDevice resolves variables from the device scope (not a
// particular interface), filtered by the device's platform.
func ResolveMapForDevice(db *gorm.DB, deviceID uint) (map[string]any, error) {
	start, err := scopeByDeviceID(db, deviceID)
	if err != nil {
		return nil, err
	}
	if start == nil {
		start, err = RootScope(db)
		if err != nil {
			return nil, err
		}
	}
	var device models.Device
	if err := db.First(&device, deviceID).Error; err != nil {
		return nil, err
	}
	var defs []models.ConfigVariableDef
	if err := db.Order("name").Find(&defs).Error; err != nil {
		return nil, err
	}
	m := map[string]any{}
	for i := range defs {
		def := &defs[i]
		if !platformAllowed(def, device.Platform) {
			continue
		}
		v, _, err := resolveDefAt(db, def, start, device.Platform)
		if err != nil || v == nil {
			continue
		}
		m[def.Name] = v
	}
	return m, nil
}
