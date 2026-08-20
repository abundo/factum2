package optical

import (
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// CustomFieldValue picks the first non-empty NetBox custom-field value
// among names (optical_role before optical_kind). Selection extras may be
// a bare string or {"value": "..."}.
func CustomFieldValue(fields map[string]any, names ...string) any {
	if fields == nil {
		return nil
	}
	for _, name := range names {
		if v, ok := fields[name]; ok && v != nil && v != "" {
			return v
		}
	}
	return nil
}

func cfString(raw any) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		if s, ok := v["value"].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// NormalizeOpticalKindCF accepts a NetBox custom-field value: a bare
// string, or a map/struct with a "value" key (selection extras). Invalid
// or empty returns "". Aliases (transponder/muxponder) become wdm_shelf.
func NormalizeOpticalKindCF(raw any) string {
	return normalizeKind(cfString(raw))
}

// NormalizeOpticalPortRole accepts the same CF shapes as
// NormalizeOpticalKindCF for an interface optical_role. Invalid/empty → "".
func NormalizeOpticalPortRole(raw any) string {
	s := strings.ToLower(cfString(raw))
	if models.AllowedOpticalPortRoles[s] {
		return s
	}
	return ""
}

func normalizeKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if alias, ok := models.OpticalKindAliases[s]; ok {
		s = alias
	}
	if models.AllowedOpticalKinds[s] {
		return s
	}
	return ""
}

// ResolveOpticalKind is CF-then-map. maps keyed by lowercased role name.
func ResolveOpticalKind(cf, role string, maps map[string]string) string {
	if kind := normalizeKind(cf); kind != "" {
		return kind
	}
	if kind := maps[strings.ToLower(strings.TrimSpace(role))]; kind != "" {
		return normalizeKind(kind)
	}
	return ""
}

// LoadKindMaps returns netbox_role_name → optical_kind (names already lower).
func LoadKindMaps(db *gorm.DB) (map[string]string, error) {
	var rows []models.OpticalKindMap
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[strings.ToLower(r.NetboxRoleName)] = r.OpticalKind
	}
	return out, nil
}

// ReresolveAllKinds rewrites Device.OpticalKind from persisted CF then map.
func ReresolveAllKinds(db *gorm.DB) error {
	maps, err := LoadKindMaps(db)
	if err != nil {
		return err
	}
	var devices []models.Device
	if err := db.Select("id", "role", "optical_kind_cf").Find(&devices).Error; err != nil {
		return err
	}
	for _, d := range devices {
		kind := ResolveOpticalKind(d.OpticalKindCF, d.Role, maps)
		if err := db.Model(&models.Device{}).Where("id = ?", d.ID).
			Update("optical_kind", kind).Error; err != nil {
			return err
		}
	}
	return nil
}

// OpticalEnabled reports Settings.OpticalEnabled (nil/false = off).
func OpticalEnabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("optical_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.OpticalEnabled != nil && *s.OpticalEnabled
}
