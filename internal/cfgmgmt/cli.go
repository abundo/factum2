package cfgmgmt

import (
	"errors"
	"regexp"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const maxCLIFeatures = 64

// BaselineRenderData is the template context for baseline (non-service) CLI
// objects. Interface/LocalIface are set only when the object's parent is an
// interface; they are zero at device/folder level.
type BaselineRenderData struct {
	Name       string
	Device     DCIMDevice
	Interface  DCIMInterface
	LocalIface string
	Vars       map[string]any
}

var captureIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CompileContextPattern turns the GUI placeholder language into an anchored
// RE2 regex. Tokens are whitespace-separated; `<ident>` becomes a named
// `\S+` capture. Empty and `global` mean the configure root (no wrap).
func CompileContextPattern(pattern string) (*regexp.Regexp, error) {
	p := strings.TrimSpace(pattern)
	if p == "" || strings.EqualFold(p, "global") {
		return nil, nil
	}
	tokens := strings.Fields(p)
	if len(tokens) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteByte('^')
	for i, tok := range tokens {
		if i > 0 {
			b.WriteString(`\s+`)
		}
		if strings.HasPrefix(tok, "<") {
			ident, ok := parseCaptureToken(tok)
			if !ok {
				return nil, statusErrf(400, "invalid context capture %q", tok)
			}
			b.WriteString(`(?P<`)
			b.WriteString(ident)
			b.WriteString(`>\S+)`)
			continue
		}
		b.WriteString(regexp.QuoteMeta(tok))
	}
	b.WriteByte('$')
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, statusErrf(400, "invalid context pattern: %v", err)
	}
	return re, nil
}

func parseCaptureToken(tok string) (string, bool) {
	if len(tok) < 3 || tok[0] != '<' || tok[len(tok)-1] != '>' {
		return "", false
	}
	ident := tok[1 : len(tok)-1]
	if !captureIdentRe.MatchString(ident) {
		return "", false
	}
	return ident, true
}

func ValidPayloadKind(k string) bool {
	switch k {
	case "", models.PayloadKindCLI, models.PayloadKindNETCONF, models.PayloadKindRESTCONF:
		return true
	}
	return false
}

func isTranslationCLI(s *models.ConfigScope) bool {
	return s != nil && s.ServiceTypeID != nil && *s.ServiceTypeID != 0
}

func normalizeCLIScope(s *models.ConfigScope) error {
	if s == nil || s.Kind != models.ConfigScopeKindCLI {
		return nil
	}
	s.Platform = NormalizePlatform(s.Platform)
	if s.PayloadKind == "" {
		s.PayloadKind = models.PayloadKindCLI
	}
	if !ValidPayloadKind(s.PayloadKind) {
		return statusErrf(400, "invalid payload kind %q", s.PayloadKind)
	}
	if isTranslationCLI(s) && s.Platform == "" {
		return statusErr(400, "cli translation object requires platform")
	}
	if s.Payload.Context != nil {
		if _, err := CompileContextPattern(s.Payload.Context.Pattern); err != nil {
			return err
		}
	}
	return nil
}

func assertCLIUnique(db *gorm.DB, s *models.ConfigScope, excludeID uint) error {
	if !isTranslationCLI(s) {
		return nil
	}
	q := db.Model(&models.ConfigScope{}).
		Where("kind = ? AND service_type_id = ? AND platform = ?",
			models.ConfigScopeKindCLI, *s.ServiceTypeID, s.Platform)
	if excludeID != 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return statusErr(409, "a CLI object already exists for this service type and platform")
	}
	return nil
}

// LookupCLIObject returns the kind=cli translator for a service type and
// platform. sros-md falls back to sros when no dedicated object exists.
func LookupCLIObject(db *gorm.DB, serviceTypeID uint, platform string) (*models.ConfigScope, error) {
	if serviceTypeID == 0 {
		return nil, nil
	}
	plat := NormalizePlatform(platform)
	var obj models.ConfigScope
	err := db.Where("kind = ? AND service_type_id = ? AND platform = ?",
		models.ConfigScopeKindCLI, serviceTypeID, plat).First(&obj).Error
	if err == nil {
		return &obj, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if plat == "sros-md" {
		err = db.Where("kind = ? AND service_type_id = ? AND platform = ?",
			models.ConfigScopeKindCLI, serviceTypeID, "sros").First(&obj).Error
		if err == nil {
			return &obj, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

func ListCLIFeatures(db *gorm.DB, scopeID uint) ([]models.ConfigCLIFeature, error) {
	var rows []models.ConfigCLIFeature
	if err := db.Where("scope_id = ?", scopeID).Order("sort_order, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func GetCLIFeature(db *gorm.DB, id uint) (*models.ConfigCLIFeature, error) {
	var row models.ConfigCLIFeature
	if err := db.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "feature not found")
		}
		return nil, err
	}
	return &row, nil
}

func CreateCLIFeature(db *gorm.DB, scopeID uint, feat *models.ConfigCLIFeature) (*models.ConfigCLIFeature, error) {
	scope, err := GetScope(db, scopeID)
	if err != nil {
		return nil, err
	}
	if scope.Kind != models.ConfigScopeKindCLI {
		return nil, statusErr(400, "features are only valid on cli scopes")
	}
	feat.ScopeID = scopeID
	feat.Name = strings.TrimSpace(feat.Name)
	if feat.Name == "" {
		return nil, statusErr(400, "name is required")
	}
	if err := assertFeatureNameUnique(db, scopeID, feat.Name, 0); err != nil {
		return nil, err
	}
	var n int64
	if err := db.Model(&models.ConfigCLIFeature{}).Where("scope_id = ?", scopeID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n >= maxCLIFeatures {
		return nil, statusErrf(400, "at most %d features per CLI object", maxCLIFeatures)
	}
	if err := db.Create(feat).Error; err != nil {
		return nil, err
	}
	return feat, nil
}

func UpdateCLIFeature(db *gorm.DB, id uint, dto *models.ConfigCLIFeatureDTO) (*models.ConfigCLIFeature, error) {
	existing, err := GetCLIFeature(db, id)
	if err != nil {
		return nil, err
	}
	if dto == nil {
		return existing, nil
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return nil, statusErr(400, "name is required")
	}
	if err := assertFeatureNameUnique(db, existing.ScopeID, name, existing.ID); err != nil {
		return nil, err
	}
	existing.Name = name
	existing.SortOrder = dto.SortOrder
	existing.AddCommands = dto.AddCommands
	existing.UpdateCommands = dto.UpdateCommands
	existing.RemoveCommands = dto.RemoveCommands
	existing.RemoveAtRoot = dto.RemoveAtRoot
	if err := db.Save(existing).Error; err != nil {
		return nil, err
	}
	return existing, nil
}

func DeleteCLIFeature(db *gorm.DB, id uint) error {
	existing, err := GetCLIFeature(db, id)
	if err != nil {
		return err
	}
	return db.Delete(existing).Error
}

func assertFeatureNameUnique(db *gorm.DB, scopeID uint, name string, excludeID uint) error {
	q := db.Model(&models.ConfigCLIFeature{}).Where("scope_id = ? AND name = ?", scopeID, name)
	if excludeID != 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return statusErr(409, "a feature with this name already exists")
	}
	return nil
}

func contextEnter(ctx *models.CLIContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Enter)
}

func renderBlob(db *gorm.DB, body string, data any) ([]string, error) {
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	return Render(db, body, "", data)
}

// RenderCLIFeature executes remove then add for one feature and applies the
// opt-in wrap policy. UpdateCommands is unused in v1.
func RenderCLIFeature(db *gorm.DB, ctx *models.CLIContext, feat *models.ConfigCLIFeature, data any) ([]string, error) {
	if feat == nil {
		return nil, nil
	}
	if ctx != nil {
		if _, err := CompileContextPattern(ctx.Pattern); err != nil {
			return nil, err
		}
	}
	removeCmds, err := renderBlob(db, feat.RemoveCommands, data)
	if err != nil {
		return nil, err
	}
	addCmds, err := renderBlob(db, feat.AddCommands, data)
	if err != nil {
		return nil, err
	}
	if len(removeCmds) == 0 && len(addCmds) == 0 {
		return nil, nil
	}
	enter := contextEnter(ctx)
	if enter == "" {
		return append(removeCmds, addCmds...), nil
	}
	enterCmds, err := renderBlob(db, ctx.Enter, data)
	if err != nil {
		return nil, err
	}
	var exitCmds []string
	if ctx != nil && strings.TrimSpace(ctx.Exit) != "" {
		exitCmds, err = renderBlob(db, ctx.Exit, data)
		if err != nil {
			return nil, err
		}
	}
	if feat.RemoveAtRoot {
		out := make([]string, 0, len(removeCmds)+len(enterCmds)+len(addCmds)+len(exitCmds))
		out = append(out, removeCmds...)
		out = append(out, enterCmds...)
		out = append(out, addCmds...)
		out = append(out, exitCmds...)
		return out, nil
	}
	out := make([]string, 0, len(enterCmds)+len(removeCmds)+len(addCmds)+len(exitCmds))
	out = append(out, enterCmds...)
	out = append(out, removeCmds...)
	out = append(out, addCmds...)
	out = append(out, exitCmds...)
	return out, nil
}

func RenderCLIObject(db *gorm.DB, obj *models.ConfigScope, data any) ([]string, error) {
	if obj == nil {
		return nil, nil
	}
	feats, err := ListCLIFeatures(db, obj.ID)
	if err != nil {
		return nil, err
	}
	var out []string
	ctx := obj.Payload.Context
	for i := range feats {
		cmds, err := RenderCLIFeature(db, ctx, &feats[i], data)
		if err != nil {
			return nil, err
		}
		out = append(out, cmds...)
	}
	return out, nil
}

func hasCLITwin(db *gorm.DB, name string, scopeID *uint) (bool, error) {
	parentID := scopeID
	if parentID == nil {
		root, err := RootScope(db)
		if err != nil {
			return false, err
		}
		parentID = &root.ID
	}
	var n int64
	if err := db.Model(&models.ConfigScope{}).
		Where("kind = ? AND name = ? AND parent_id = ?", models.ConfigScopeKindCLI, name, *parentID).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func reverseScopes(in []models.ConfigScope) []models.ConfigScope {
	out := make([]models.ConfigScope, len(in))
	for i := range in {
		out[len(in)-1-i] = in[i]
	}
	return out
}

func baselineParents(db *gorm.DB, deviceID uint) ([]models.ConfigScope, error) {
	deviceScope, err := scopeByDeviceID(db, deviceID)
	if err != nil {
		return nil, err
	}
	if deviceScope == nil {
		root, err := RootScope(db)
		if err != nil {
			return nil, err
		}
		deviceScope = root
	}
	chain, err := WalkParents(db, deviceScope)
	if err != nil {
		return nil, err
	}
	parents := reverseScopes(chain)
	if deviceScope.Kind == models.ConfigScopeKindDevice {
		var ifaces []models.ConfigScope
		if err := db.Where("parent_id = ? AND kind = ?", deviceScope.ID, models.ConfigScopeKindInterface).
			Order("name").Find(&ifaces).Error; err != nil {
			return nil, err
		}
		parents = append(parents, ifaces...)
	}
	return parents, nil
}

func cliPlatformMatch(objPlatform, devicePlatform string, dedicatedSRSOMD bool) bool {
	if strings.TrimSpace(objPlatform) == "" {
		return true
	}
	o := NormalizePlatform(objPlatform)
	d := NormalizePlatform(devicePlatform)
	if o == d {
		return true
	}
	if d == "sros-md" && o == "sros" && !dedicatedSRSOMD {
		return true
	}
	return false
}

func baselineCLIChildren(db *gorm.DB, parentID uint, devicePlatform string) ([]models.ConfigScope, error) {
	var kids []models.ConfigScope
	if err := db.Where("parent_id = ? AND kind = ? AND enabled = ?", parentID, models.ConfigScopeKindCLI, true).
		Order("sort_order, name").Find(&kids).Error; err != nil {
		return nil, err
	}
	dedicated := false
	devPlat := NormalizePlatform(devicePlatform)
	if devPlat == "sros-md" {
		for i := range kids {
			if isTranslationCLI(&kids[i]) {
				continue
			}
			if NormalizePlatform(kids[i].Platform) == "sros-md" {
				dedicated = true
				break
			}
		}
	}
	out := make([]models.ConfigScope, 0, len(kids))
	for i := range kids {
		k := kids[i]
		if isTranslationCLI(&k) {
			continue
		}
		if !cliPlatformMatch(k.Platform, devicePlatform, dedicated) {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

func baselineData(db *gorm.DB, device *models.Device, parent *models.ConfigScope, deviceVars map[string]any) BaselineRenderData {
	data := BaselineRenderData{
		Name:   device.Name,
		Device: DCIMFromDevice(device),
		Vars:   deviceVars,
	}
	if parent == nil || parent.Kind != models.ConfigScopeKindInterface || parent.InterfaceID == nil {
		return data
	}
	iface, err := loadInterface(db, *parent.InterfaceID)
	if err != nil || iface == nil {
		return data
	}
	data.Interface = DCIMFromInterface(iface)
	data.LocalIface = iface.Name
	if m, err := ResolveMap(db, iface.ID); err == nil {
		data.Vars = m
	}
	return data
}

func renderBaselineCLI(db *gorm.DB, device *models.Device, vars map[string]any) ([]RenderedSource, error) {
	parents, err := baselineParents(db, device.ID)
	if err != nil {
		return nil, err
	}
	var out []RenderedSource
	for i := range parents {
		parent := &parents[i]
		kids, err := baselineCLIChildren(db, parent.ID, device.Platform)
		if err != nil {
			return nil, err
		}
		data := baselineData(db, device, parent, vars)
		for j := range kids {
			obj := &kids[j]
			kind := obj.PayloadKind
			if kind == "" {
				kind = models.PayloadKindCLI
			}
			cmds, err := RenderCLIObject(db, obj, data)
			src := RenderedSource{
				Source:      "cli:" + obj.Name,
				Kind:        "cli",
				Platform:    obj.Platform,
				PayloadKind: kind,
				Commands:    cmds,
			}
			if err != nil {
				src.Error = err.Error()
			}
			out = append(out, src)
		}
	}
	return out, nil
}

func nearestMatrixScope(db *gorm.DB, start *models.ConfigScope) (*models.ConfigScope, error) {
	if start == nil {
		return nil, statusErr(400, "missing scope")
	}
	if start.Kind != models.ConfigScopeKindParameter && start.Kind != models.ConfigScopeKindCLI {
		return start, nil
	}
	chain, err := WalkParents(db, start)
	if err != nil {
		return nil, err
	}
	for i := range chain {
		switch chain[i].Kind {
		case models.ConfigScopeKindFolder, models.ConfigScopeKindSite,
			models.ConfigScopeKindLocation, models.ConfigScopeKindDevice:
			cp := chain[i]
			return &cp, nil
		}
	}
	return start, nil
}
