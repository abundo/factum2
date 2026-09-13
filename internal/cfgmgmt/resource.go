package cfgmgmt

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const maxResourceCIDRs = 256

// ResourceCIDRStatus is one pool prefix and whether occupancy found it in use.
type ResourceCIDRStatus struct {
	Prefix string `json:"prefix"`
	Free   bool   `json:"free"`
}

// AllocatedResource is the winning kind=resource node for a walk.
type AllocatedResource struct {
	ScopeID uint                 `json:"scope_id"`
	CIDRs   []ResourceCIDRStatus `json:"cidrs"`
}

func normalizeResourceScope(s *models.ConfigScope) error {
	if s == nil || s.Kind != models.ConfigScopeKindResource {
		return nil
	}
	cidrs, err := canonicalizeResourceCIDRs(s.Payload.CIDRs)
	if err != nil {
		return err
	}
	s.Payload.CIDRs = cidrs
	return nil
}

func canonicalizeResourceCIDRs(in []string) ([]string, error) {
	if len(in) > maxResourceCIDRs {
		return nil, statusErrf(400, "resource may have at most %d CIDRs", maxResourceCIDRs)
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for i, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			return nil, statusErrf(400, "cidrs[%d] must not be empty", i)
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, statusErrf(400, "cidrs[%d] is not a valid prefix", i)
		}
		canon := p.Masked().String()
		if seen[canon] {
			return nil, statusErrf(400, "duplicate CIDR %s", canon)
		}
		seen[canon] = true
		out = append(out, canon)
	}
	return out, nil
}

func allocateStart(db *gorm.DB, interfaceID, deviceID uint) (*models.ConfigScope, error) {
	if interfaceID != 0 {
		return StartScope(db, interfaceID)
	}
	if deviceID != 0 {
		if s, err := scopeByDeviceID(db, deviceID); err != nil {
			return nil, err
		} else if s != nil {
			return s, nil
		}
		var dev models.Device
		if err := db.First(&dev, deviceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, statusErr(404, "device not found")
			}
			return nil, err
		}
		return RootScope(db)
	}
	return RootScope(db)
}

func resourceChildren(db *gorm.DB, parentID uint, name string) ([]models.ConfigScope, error) {
	var rows []models.ConfigScope
	err := db.Where("parent_id = ? AND kind = ? AND name = ? AND enabled = ?",
		parentID, models.ConfigScopeKindResource, name, true).
		Order("sort_order DESC, name DESC").
		Find(&rows).Error
	return rows, err
}

func prefixFamily(p string) int {
	pref, err := netip.ParsePrefix(p)
	if err != nil {
		return 0
	}
	if pref.Addr().Is4() {
		return 4
	}
	if pref.Addr().Is6() {
		return 6
	}
	return 0
}

func familyOK(family int, prefix string) bool {
	if family == 0 {
		return true
	}
	return prefixFamily(prefix) == family
}

func collectOccupiedPrefixes(fields map[string]any, schema []models.FieldSchema, resourceName string, occ map[string]bool) {
	for _, f := range schema {
		collectOccupiedValue(f, fields[f.Name], resourceName, occ)
	}
}

func collectOccupiedValue(f models.FieldSchema, v any, resourceName string, occ map[string]bool) {
	typ := NormalizeFieldType(f.Type)
	if typ == models.FieldTypeList {
		if f.Items == nil {
			return
		}
		items, ok := asList(v)
		if !ok {
			return
		}
		for _, item := range items {
			collectOccupiedValue(*f.Items, item, resourceName, occ)
		}
		return
	}
	if !isPrefixFieldType(typ) || f.Resource != resourceName {
		return
	}
	s, ok := asString(v)
	if ok && s != "" {
		occ[s] = true
	}
}

func occupiedPrefixes(db *gorm.DB, resourceName string) (map[string]bool, error) {
	types, err := ListServiceTypes(db)
	if err != nil {
		return nil, err
	}
	stByName := make(map[string]*models.ServiceType, len(types))
	for i := range types {
		stByName[types[i].Name] = &types[i]
	}
	var svcs []models.Service
	if err := db.Find(&svcs).Error; err != nil {
		return nil, err
	}
	svcType := make(map[uint]string, len(svcs))
	occ := map[string]bool{}
	for i := range svcs {
		svc := &svcs[i]
		svcType[svc.ID] = svc.ServiceType
		st := stByName[svc.ServiceType]
		if st == nil {
			continue
		}
		collectOccupiedPrefixes(fieldsMap(svc.Fields), st.Schema, resourceName, occ)
	}
	var eps []models.ServiceEndpoint
	if err := db.Find(&eps).Error; err != nil {
		return nil, err
	}
	for i := range eps {
		ep := &eps[i]
		st := stByName[svcType[ep.ServiceID]]
		if st == nil {
			continue
		}
		collectOccupiedPrefixes(fieldsMap(ep.Fields), st.Interfaces.Fields, resourceName, occ)
	}
	return occ, nil
}

// AllocateResource walks interface → device → ancestors → global and returns
// the closest enabled resource child whose name matches. Occupancy is a
// read-only scan; this does not write.
func AllocateResource(db *gorm.DB, interfaceID, deviceID uint, resourceName string, family int) (*AllocatedResource, error) {
	if resourceName == "" {
		return nil, statusErr(400, "name is required")
	}
	if family != 0 && family != 4 && family != 6 {
		return nil, statusErr(400, "family must be 0, 4, or 6")
	}
	start, err := allocateStart(db, interfaceID, deviceID)
	if err != nil {
		return nil, err
	}
	chain, err := WalkParents(db, start)
	if err != nil {
		return nil, err
	}
	var winner *models.ConfigScope
	for i := range chain {
		kids, err := resourceChildren(db, chain[i].ID, resourceName)
		if err != nil {
			return nil, err
		}
		if len(kids) > 0 {
			winner = &kids[0]
			break
		}
	}
	if winner == nil {
		return nil, statusErrf(400, "no resource named %q on the ancestor chain", resourceName)
	}
	occ, err := occupiedPrefixes(db, resourceName)
	if err != nil {
		return nil, err
	}
	out := &AllocatedResource{ScopeID: winner.ID, CIDRs: []ResourceCIDRStatus{}}
	for _, p := range winner.Payload.CIDRs {
		if !familyOK(family, p) {
			continue
		}
		out.CIDRs = append(out.CIDRs, ResourceCIDRStatus{Prefix: p, Free: !occ[p]})
	}
	return out, nil
}
