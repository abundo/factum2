package ipam

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// TreeNode is one row of the prefix tree, shaped for Wunderbaum (key,
// title, lazy, data). Parent/children are computed from CIDR containment.
type TreeNode struct {
	Key      string       `json:"key"`
	Title    string       `json:"title"`
	Lazy     bool         `json:"lazy"`
	Expanded bool         `json:"expanded,omitempty"`
	Type     string       `json:"type"`
	Data     TreeNodeData `json:"data"`
	Children []TreeNode   `json:"children,omitempty"`
}

type TreeNodeData struct {
	Kind        string  `json:"kind"` // namespace | pool | vrf | allocated
	ID          uint    `json:"id"`
	NamespaceID uint    `json:"namespace_id,omitempty"`
	PoolID      uint    `json:"pool_id,omitempty"`
	PrefixID    uint    `json:"prefix_id,omitempty"`
	VRFID       uint    `json:"vrf_id,omitempty"`
	VRFName     string  `json:"vrf_name,omitempty"`
	IsDefault   bool    `json:"is_default,omitempty"`
	Family      int     `json:"family"`
	Description string  `json:"description,omitempty"`
	Used        string  `json:"used"`
	UsedFrac    float64 `json:"used_frac"`
	ChildCount  int     `json:"child_count"`
}

type treeEntry struct {
	prefix      netip.Prefix
	kind        string
	id          uint
	poolID      uint
	prefixID    uint
	vrfID       uint
	vrfName     string
	description string
}

func Tree(db *gorm.DB, nsID uint, parentRaw string) ([]TreeNode, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}

	var pools []models.IpamNamespacePrefix
	if err := db.Where("namespace_id = ?", nsID).Find(&pools).Error; err != nil {
		return nil, err
	}
	var allocs []models.IpamPrefix
	if err := db.Where("namespace_id = ?", nsID).Find(&allocs).Error; err != nil {
		return nil, err
	}
	var vrfs []models.IpamVRF
	if err := db.Where("namespace_id = ?", nsID).Find(&vrfs).Error; err != nil {
		return nil, err
	}
	vrfName := map[uint]string{}
	for _, v := range vrfs {
		vrfName[v.ID] = v.Name
	}

	// Merge pool + allocation on the same CIDR into one entry. An
	// allocation wins (kind=allocated) but we keep the pool id so the UI
	// can still tell it was a pool boundary.
	byKey := map[string]*treeEntry{}
	for _, p := range pools {
		pp, err := ParsePrefix(p.Prefix)
		if err != nil {
			continue
		}
		byKey[pp.String()] = &treeEntry{
			prefix: pp,
			kind:   "pool",
			id:     p.ID,
			poolID: p.ID,
		}
	}
	for _, a := range allocs {
		pp, err := ParsePrefix(a.Prefix)
		if err != nil {
			continue
		}
		key := pp.String()
		if existing, ok := byKey[key]; ok {
			existing.kind = "allocated"
			existing.prefixID = a.ID
			existing.id = a.ID
			existing.vrfID = a.VRFID
			existing.vrfName = vrfName[a.VRFID]
			existing.description = a.Description
			continue
		}
		byKey[key] = &treeEntry{
			prefix:      pp,
			kind:        "allocated",
			id:          a.ID,
			prefixID:    a.ID,
			vrfID:       a.VRFID,
			vrfName:     vrfName[a.VRFID],
			description: a.Description,
		}
	}

	all := make([]*treeEntry, 0, len(byKey))
	for _, e := range byKey {
		all = append(all, e)
	}

	var parent netip.Prefix
	var hasParent bool
	if parentRaw != "" {
		p, err := ParsePrefix(parentRaw)
		if err != nil {
			return nil, statusErr(400, err.Error())
		}
		parent = p
		hasParent = true
	}

	// Immediate children of parent (or top-level pools when no parent):
	// an entry is a child if it is strictly inside the parent (or, at the
	// root, is a pool not contained in any other pool) and no other entry
	// sits strictly between it and the parent.
	var candidates []*treeEntry
	if !hasParent {
		// Roots are allowed prefixes that are not nested inside another
		// allowed prefix. Allocations appear under the covering pool.
		for _, e := range all {
			if e.poolID == 0 {
				continue
			}
			nested := false
			for _, o := range all {
				if o == e || o.poolID == 0 {
					continue
				}
				if strictlyContains(o.prefix, e.prefix) {
					nested = true
					break
				}
			}
			if !nested {
				candidates = append(candidates, e)
			}
		}
	} else {
		for _, e := range all {
			if !strictlyContains(parent, e.prefix) {
				continue
			}
			between := false
			for _, o := range all {
				if o == e {
					continue
				}
				if strictlyContains(parent, o.prefix) && strictlyContains(o.prefix, e.prefix) {
					between = true
					break
				}
			}
			if !between {
				candidates = append(candidates, e)
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if familyOf(candidates[i].prefix) != familyOf(candidates[j].prefix) {
			return familyOf(candidates[i].prefix) < familyOf(candidates[j].prefix)
		}
		return candidates[i].prefix.Addr().Compare(candidates[j].prefix.Addr()) < 0 ||
			(candidates[i].prefix.Addr() == candidates[j].prefix.Addr() &&
				candidates[i].prefix.Bits() < candidates[j].prefix.Bits())
	})

	nodes := make([]TreeNode, 0, len(candidates))
	for _, e := range candidates {
		childPrefixes := immediateAllocatedChildren(e.prefix, all)
		// child_count / lazy: any more-specific entry, not just allocated.
		nChildren := 0
		for _, o := range all {
			if strictlyContains(e.prefix, o.prefix) {
				nChildren++
			}
		}
		frac := coverageFrac(e.prefix, childPrefixes)
		nodes = append(nodes, TreeNode{
			Key:   e.prefix.String(),
			Title: e.prefix.String(),
			Lazy:  nChildren > 0,
			Type:  e.kind,
			Data: TreeNodeData{
				Kind:        e.kind,
				ID:          e.id,
				PoolID:      e.poolID,
				PrefixID:    e.prefixID,
				VRFID:       e.vrfID,
				VRFName:     e.vrfName,
				Family:      familyOf(e.prefix),
				Description: e.description,
				Used:        formatUsed(frac, len(childPrefixes) > 0),
				UsedFrac:    frac,
				ChildCount:  nChildren,
			},
		})
	}
	return nodes, nil
}

// Node keys are "kind:id" so a single Wunderbaum tree can hold namespaces,
// VRFs, pools and allocations without colliding.

func nodeKey(kind string, id uint) string {
	return fmt.Sprintf("%s:%d", kind, id)
}

// ParseNodeKey splits "ns:3" into ("ns", 3).
func ParseNodeKey(s string) (string, uint, error) {
	kind, rest, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || kind == "" || rest == "" {
		return "", 0, statusErr(400, "invalid tree node")
	}
	id, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return "", 0, statusErr(400, "invalid tree node")
	}
	return kind, uint(id), nil
}

// Roots returns every namespace as a top-level tree node, with the first
// level of children already loaded so a refresh shows root prefixes and
// extra VRFs without an extra expand click.
func Roots(db *gorm.DB) ([]TreeNode, error) {
	var rows []models.IpamNamespace
	if err := db.Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TreeNode, 0, len(rows))
	for _, ns := range rows {
		kids, err := namespaceChildren(db, ns.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, TreeNode{
			Key:      nodeKey("ns", ns.ID),
			Title:    ns.Name,
			Lazy:     false,
			Expanded: true,
			Type:     "namespace",
			Children: kids,
			Data: TreeNodeData{
				Kind:        "namespace",
				ID:          ns.ID,
				NamespaceID: ns.ID,
				Description: ns.Description,
				ChildCount:  len(kids),
			},
		})
	}
	return out, nil
}

// Children returns the next tree level under parentKey (ns:1, vrf:2, …).
func Children(db *gorm.DB, parentKey string) ([]TreeNode, error) {
	kind, id, err := ParseNodeKey(parentKey)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "ns":
		return namespaceChildren(db, id)
	case "pool":
		return poolChildren(db, id)
	case "vrf":
		return vrfPrefixChildren(db, id, 0)
	case "pfx":
		return prefixChildren(db, id)
	default:
		return nil, statusErr(400, "invalid tree node")
	}
}

func namespaceChildren(db *gorm.DB, nsID uint) ([]TreeNode, error) {
	if _, err := loadNamespace(db, nsID); err != nil {
		return nil, err
	}
	vrfs, err := ListVRFs(db, nsID)
	if err != nil {
		return nil, err
	}

	allocCount, err := prefixCountByVRF(db, nsID)
	if err != nil {
		return nil, err
	}

	var defaultID uint
	var extra []models.IpamVRF
	for _, v := range vrfs {
		if v.IsDefault {
			defaultID = v.ID
			continue
		}
		extra = append(extra, v)
	}

	// Root prefixes first so a namespace without extra VRFs is just a
	// prefix tree. Extra VRFs follow as optional branches.
	var out []TreeNode
	if defaultID != 0 {
		roots, err := vrfPrefixChildren(db, defaultID, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, roots...)
	}
	for _, v := range extra {
		n := allocCount[v.ID]
		out = append(out, TreeNode{
			Key:   nodeKey("vrf", v.ID),
			Title: v.Name,
			Lazy:  n > 0,
			Type:  "vrf",
			Data: TreeNodeData{
				Kind:        "vrf",
				ID:          v.ID,
				NamespaceID: nsID,
				VRFID:       v.ID,
				VRFName:     v.Name,
				IsDefault:   v.IsDefault,
				Description: v.Description,
				ChildCount:  int(n),
			},
		})
	}
	return out, nil
}

func poolChildren(db *gorm.DB, poolID uint) ([]TreeNode, error) {
	var pool models.IpamNamespacePrefix
	if err := db.First(&pool, poolID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "allowed prefix not found")
		}
		return nil, err
	}
	parent, err := ParsePrefix(pool.Prefix)
	if err != nil {
		return nil, statusErr(400, err.Error())
	}
	var pools []models.IpamNamespacePrefix
	if err := db.Where("namespace_id = ? AND id <> ?", pool.NamespaceID, poolID).Find(&pools).Error; err != nil {
		return nil, err
	}
	var out []TreeNode
	for _, p := range pools {
		pp, err := ParsePrefix(p.Prefix)
		if err != nil || !strictlyContains(parent, pp) {
			continue
		}
		between := false
		for _, o := range pools {
			if o.ID == p.ID {
				continue
			}
			op, err := ParsePrefix(o.Prefix)
			if err != nil {
				continue
			}
			if strictlyContains(parent, op) && strictlyContains(op, pp) {
				between = true
				break
			}
		}
		if !between {
			nested := false
			for _, o := range pools {
				if o.ID == p.ID {
					continue
				}
				op, err := ParsePrefix(o.Prefix)
				if err != nil {
					continue
				}
				if strictlyContains(pp, op) {
					nested = true
					break
				}
			}
			out = append(out, poolNode(p, pp, pool.NamespaceID, nested))
		}
	}
	sortTreePrefixes(out)
	return out, nil
}

func vrfPrefixChildren(db *gorm.DB, vrfID, parentPrefixID uint) ([]TreeNode, error) {
	var vrf models.IpamVRF
	if err := db.First(&vrf, vrfID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "VRF not found")
		}
		return nil, err
	}
	var allocs []models.IpamPrefix
	if err := db.Where("vrf_id = ?", vrfID).Find(&allocs).Error; err != nil {
		return nil, err
	}

	var parent netip.Prefix
	var hasParent bool
	if parentPrefixID != 0 {
		var row models.IpamPrefix
		if err := db.First(&row, parentPrefixID).Error; err != nil {
			return nil, statusErr(404, "prefix not found")
		}
		p, err := ParsePrefix(row.Prefix)
		if err != nil {
			return nil, statusErr(400, err.Error())
		}
		parent = p
		hasParent = true
	}

	type item struct {
		row models.IpamPrefix
		p   netip.Prefix
	}
	var all []item
	for _, a := range allocs {
		p, err := ParsePrefix(a.Prefix)
		if err != nil {
			continue
		}
		all = append(all, item{row: a, p: p})
	}

	var out []TreeNode
	for _, e := range all {
		if hasParent {
			if !strictlyContains(parent, e.p) {
				continue
			}
		} else {
			// Top-level in this VRF: not contained in another allocation here.
			contained := false
			for _, o := range all {
				if o.row.ID == e.row.ID {
					continue
				}
				if strictlyContains(o.p, e.p) {
					contained = true
					break
				}
			}
			if contained {
				continue
			}
		}
		if hasParent {
			between := false
			for _, o := range all {
				if o.row.ID == e.row.ID {
					continue
				}
				if strictlyContains(parent, o.p) && strictlyContains(o.p, e.p) {
					between = true
					break
				}
			}
			if between {
				continue
			}
		}
		hasChild := false
		for _, o := range all {
			if o.row.ID == e.row.ID {
				continue
			}
			if strictlyContains(e.p, o.p) {
				hasChild = true
				break
			}
		}
		vrfLabel := vrf.Name
		if vrf.IsDefault {
			vrfLabel = ""
		}
		out = append(out, allocNode(e.row, e.p, vrfLabel, hasChild))
	}
	sortTreePrefixes(out)
	return out, nil
}

func prefixChildren(db *gorm.DB, prefixID uint) ([]TreeNode, error) {
	var row models.IpamPrefix
	if err := db.First(&row, prefixID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "prefix not found")
		}
		return nil, err
	}
	return vrfPrefixChildren(db, row.VRFID, prefixID)
}

func poolNode(p models.IpamNamespacePrefix, pp netip.Prefix, nsID uint, hasChild bool) TreeNode {
	return TreeNode{
		Key:   nodeKey("pool", p.ID),
		Title: p.Prefix,
		Lazy:  hasChild,
		Type:  "pool",
		Data: TreeNodeData{
			Kind:        "pool",
			ID:          p.ID,
			NamespaceID: nsID,
			PoolID:      p.ID,
			Family:      familyOf(pp),
			ChildCount:  boolCount(hasChild),
		},
	}
}

func allocNode(a models.IpamPrefix, p netip.Prefix, vrfName string, hasChild bool) TreeNode {
	return TreeNode{
		Key:   nodeKey("pfx", a.ID),
		Title: a.Prefix,
		Lazy:  hasChild,
		Type:  "allocated",
		Data: TreeNodeData{
			Kind:        "allocated",
			ID:          a.ID,
			NamespaceID: a.NamespaceID,
			PrefixID:    a.ID,
			VRFID:       a.VRFID,
			VRFName:     vrfName,
			Family:      familyOf(p),
			Description: a.Description,
			ChildCount:  boolCount(hasChild),
		},
	}
}

func prefixCountByVRF(db *gorm.DB, nsID uint) (map[uint]int64, error) {
	type row struct {
		VRFID uint  `gorm:"column:vrf_id"`
		N     int64 `gorm:"column:n"`
	}
	var rows []row
	if err := db.Model(&models.IpamPrefix{}).
		Select("vrf_id, count(*) as n").
		Where("namespace_id = ?", nsID).
		Group("vrf_id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint]int64{}
	for _, r := range rows {
		out[r.VRFID] = r.N
	}
	return out, nil
}

func poolHasNested(parsed []netip.Prefix, i int) bool {
	parent := parsed[i]
	if !parent.IsValid() {
		return false
	}
	for j, child := range parsed {
		if i == j || !child.IsValid() {
			continue
		}
		if strictlyContains(parent, child) {
			return true
		}
	}
	return false
}

func boolCount(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

func sortTreePrefixes(nodes []TreeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Data.Family != nodes[j].Data.Family {
			return nodes[i].Data.Family < nodes[j].Data.Family
		}
		return nodes[i].Title < nodes[j].Title
	})
}

func immediateAllocatedChildren(parent netip.Prefix, all []*treeEntry) []netip.Prefix {
	var out []netip.Prefix
	for _, e := range all {
		if e.kind != "allocated" || !strictlyContains(parent, e.prefix) {
			continue
		}
		between := false
		for _, o := range all {
			if o == e || o.kind != "allocated" {
				continue
			}
			if strictlyContains(parent, o.prefix) && strictlyContains(o.prefix, e.prefix) {
				between = true
				break
			}
		}
		if !between {
			out = append(out, e.prefix)
		}
	}
	return out
}
