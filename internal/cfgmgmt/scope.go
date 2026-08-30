package cfgmgmt

import (
	"errors"
	"sort"
	"strconv"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// ScopeTreeNode is one nested scope for GET /api/config/scopes/tree.
type ScopeTreeNode struct {
	Key      string          `json:"key"`
	Title    string          `json:"title"`
	Type     string          `json:"type"`
	Data     ScopeTreeData   `json:"data"`
	Children []ScopeTreeNode `json:"children,omitempty"`
}

type ScopeTreeData struct {
	ID          uint   `json:"id"`
	Kind        string `json:"kind"`
	ParentID    *uint  `json:"parent_id,omitempty"`
	SiteID      *uint  `json:"site_id,omitempty"`
	DeviceID    *uint  `json:"device_id,omitempty"`
	InterfaceID *uint  `json:"interface_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

func GetScope(db *gorm.DB, id uint) (*models.ConfigScope, error) {
	var s models.ConfigScope
	if err := db.First(&s, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "scope not found")
		}
		return nil, err
	}
	return &s, nil
}

func ListScopes(db *gorm.DB) ([]models.ConfigScope, error) {
	var rows []models.ConfigScope
	if err := db.Order("sort_order, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func CreateScope(db *gorm.DB, s *models.ConfigScope) (*models.ConfigScope, error) {
	if s.Name == "" {
		return nil, statusErr(400, "name is required")
	}
	if !ValidScopeKind(s.Kind) {
		return nil, statusErr(400, "invalid kind")
	}
	if s.ParentID != nil {
		if _, err := GetScope(db, *s.ParentID); err != nil {
			return nil, err
		}
	} else {
		root, err := RootScope(db)
		if err != nil {
			return nil, err
		}
		s.ParentID = &root.ID
	}
	if err := assertScopeKindIDs(s); err != nil {
		return nil, err
	}
	if err := assertScopeUnique(db, s, 0); err != nil {
		return nil, err
	}
	if err := db.Create(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

func assertScopeKindIDs(s *models.ConfigScope) error {
	switch s.Kind {
	case models.ConfigScopeKindDevice:
		if s.DeviceID == nil || *s.DeviceID == 0 {
			return statusErr(400, "device scope requires device_id")
		}
	case models.ConfigScopeKindInterface:
		if s.InterfaceID == nil || *s.InterfaceID == 0 {
			return statusErr(400, "interface scope requires interface_id")
		}
	}
	return nil
}

func assertScopeUnique(db *gorm.DB, s *models.ConfigScope, excludeID uint) error {
	if s.Kind == models.ConfigScopeKindDevice && s.DeviceID != nil {
		q := db.Model(&models.ConfigScope{}).
			Where("kind = ? AND device_id = ?", models.ConfigScopeKindDevice, *s.DeviceID)
		if excludeID != 0 {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return statusErr(409, "a device scope already exists for this device")
		}
	}
	if s.Kind == models.ConfigScopeKindInterface && s.InterfaceID != nil {
		q := db.Model(&models.ConfigScope{}).
			Where("kind = ? AND interface_id = ?", models.ConfigScopeKindInterface, *s.InterfaceID)
		if excludeID != 0 {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return statusErr(409, "an interface scope already exists for this interface")
		}
	}
	return nil
}

func UpdateScope(db *gorm.DB, id uint, patch *models.ConfigScope) (*models.ConfigScope, error) {
	existing, err := GetScope(db, id)
	if err != nil {
		return nil, err
	}
	root, err := RootScope(db)
	if err != nil {
		return nil, err
	}
	if existing.ID == root.ID {
		if patch.ParentID != nil {
			return nil, statusErr(400, "cannot reparent the global root")
		}
		if patch.Name != "" && patch.Name != existing.Name {
			return nil, statusErr(400, "cannot rename the global root")
		}
		if patch.Kind != "" && patch.Kind != existing.Kind {
			return nil, statusErr(400, "cannot change the kind of the global root")
		}
	}
	if patch.Kind != "" && !ValidScopeKind(patch.Kind) {
		return nil, statusErr(400, "invalid kind")
	}
	if patch.ParentID != nil {
		if *patch.ParentID == existing.ID {
			return nil, statusErr(400, "scope cannot be its own parent")
		}
		cycle, err := WouldCycle(db, existing.ID, *patch.ParentID)
		if err != nil {
			return nil, err
		}
		if cycle {
			return nil, statusErr(400, "scope parent cycle")
		}
		existing.ParentID = patch.ParentID
	}
	if patch.Name != "" {
		existing.Name = patch.Name
	}
	if patch.Kind != "" {
		existing.Kind = patch.Kind
	}
	if patch.SiteID != nil {
		existing.SiteID = patch.SiteID
	}
	if patch.DeviceID != nil {
		existing.DeviceID = patch.DeviceID
	}
	if patch.InterfaceID != nil {
		existing.InterfaceID = patch.InterfaceID
	}
	existing.SortOrder = patch.SortOrder
	if err := assertScopeKindIDs(existing); err != nil {
		return nil, err
	}
	if err := assertScopeUnique(db, existing, existing.ID); err != nil {
		return nil, err
	}
	if err := db.Save(existing).Error; err != nil {
		return nil, err
	}
	return existing, nil
}

func DeleteScope(db *gorm.DB, id uint) error {
	existing, err := GetScope(db, id)
	if err != nil {
		return err
	}
	root, err := RootScope(db)
	if err != nil {
		return err
	}
	if existing.ID == root.ID {
		return statusErr(400, "cannot delete the global root")
	}
	var n int64
	if err := db.Model(&models.ConfigScope{}).Where("parent_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return statusErr(409, "scope has children")
	}
	return db.Delete(existing).Error
}

// AttachDevice creates or updates a device-kind node under parentID and
// ensures a child interface node exists for each inventory interface.
func AttachDevice(db *gorm.DB, parentID, deviceID uint) (*models.ConfigScope, error) {
	if _, err := GetScope(db, parentID); err != nil {
		return nil, err
	}
	var device models.Device
	if err := db.First(&device, deviceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "device not found")
		}
		return nil, err
	}
	existing, err := scopeByDeviceID(db, deviceID)
	if err != nil {
		return nil, err
	}
	var node *models.ConfigScope
	if existing != nil {
		if parentID == existing.ID {
			return nil, statusErr(400, "scope cannot be its own parent")
		}
		cycle, err := WouldCycle(db, existing.ID, parentID)
		if err != nil {
			return nil, err
		}
		if cycle {
			return nil, statusErr(400, "scope parent cycle")
		}
		existing.ParentID = &parentID
		existing.Name = device.Name
		existing.Kind = models.ConfigScopeKindDevice
		if err := db.Save(existing).Error; err != nil {
			return nil, err
		}
		node = existing
	} else {
		created := models.ConfigScope{
			ParentID: &parentID,
			Name:     device.Name,
			Kind:     models.ConfigScopeKindDevice,
			DeviceID: &deviceID,
			SiteID:   nil,
		}
		if device.SiteID != 0 {
			sid := device.SiteID
			created.SiteID = &sid
		}
		if err := db.Create(&created).Error; err != nil {
			return nil, err
		}
		node = &created
	}
	if err := ensureInterfaceChildren(db, node, deviceID); err != nil {
		return nil, err
	}
	return node, nil
}

func ensureInterfaceChildren(db *gorm.DB, deviceNode *models.ConfigScope, deviceID uint) error {
	var ifaces []models.Interface
	if err := db.Where("device_id = ?", deviceID).Order("name").Find(&ifaces).Error; err != nil {
		return err
	}
	for i := range ifaces {
		iface := ifaces[i]
		found, err := scopeByInterfaceID(db, iface.ID)
		if err != nil {
			return err
		}
		if found != nil {
			continue
		}
		id := iface.ID
		child := models.ConfigScope{
			ParentID:    &deviceNode.ID,
			Name:        iface.Name,
			Kind:        models.ConfigScopeKindInterface,
			DeviceID:    &deviceID,
			InterfaceID: &id,
			SortOrder:   i,
		}
		if err := db.Create(&child).Error; err != nil {
			return err
		}
	}
	return nil
}

func ScopeTree(db *gorm.DB) ([]ScopeTreeNode, error) {
	rows, err := ListScopes(db)
	if err != nil {
		return nil, err
	}
	byParent := map[uint][]models.ConfigScope{}
	var roots []models.ConfigScope
	for _, s := range rows {
		if s.ParentID == nil {
			roots = append(roots, s)
			continue
		}
		byParent[*s.ParentID] = append(byParent[*s.ParentID], s)
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].SortOrder != roots[j].SortOrder {
			return roots[i].SortOrder < roots[j].SortOrder
		}
		return roots[i].Name < roots[j].Name
	})
	out := make([]ScopeTreeNode, 0, len(roots))
	path := map[uint]bool{}
	for i := range roots {
		out = append(out, buildTreeNode(&roots[i], byParent, path))
	}
	return out, nil
}

func buildTreeNode(s *models.ConfigScope, byParent map[uint][]models.ConfigScope, path map[uint]bool) ScopeTreeNode {
	kids := byParent[s.ID]
	sort.Slice(kids, func(i, j int) bool {
		if kids[i].SortOrder != kids[j].SortOrder {
			return kids[i].SortOrder < kids[j].SortOrder
		}
		return kids[i].Name < kids[j].Name
	})
	n := ScopeTreeNode{
		Key:   strconv.FormatUint(uint64(s.ID), 10),
		Title: s.Name,
		Type:  s.Kind,
		Data: ScopeTreeData{
			ID:          s.ID,
			Kind:        s.Kind,
			ParentID:    s.ParentID,
			SiteID:      s.SiteID,
			DeviceID:    s.DeviceID,
			InterfaceID: s.InterfaceID,
			SortOrder:   s.SortOrder,
		},
	}
	if path[s.ID] {
		return n
	}
	path[s.ID] = true
	for i := range kids {
		n.Children = append(n.Children, buildTreeNode(&kids[i], byParent, path))
	}
	delete(path, s.ID)
	return n
}

func DescendantScopes(db *gorm.DB, rootID uint) ([]models.ConfigScope, error) {
	rows, err := ListScopes(db)
	if err != nil {
		return nil, err
	}
	byParent := map[uint][]models.ConfigScope{}
	var start *models.ConfigScope
	for i := range rows {
		s := rows[i]
		if s.ID == rootID {
			cp := s
			start = &cp
		}
		if s.ParentID != nil {
			byParent[*s.ParentID] = append(byParent[*s.ParentID], s)
		}
	}
	if start == nil {
		return nil, statusErr(404, "scope not found")
	}
	var out []models.ConfigScope
	seen := map[uint]bool{}
	var walk func(models.ConfigScope)
	walk = func(s models.ConfigScope) {
		if seen[s.ID] {
			return
		}
		seen[s.ID] = true
		out = append(out, s)
		for _, c := range byParent[s.ID] {
			walk(c)
		}
	}
	walk(*start)
	return out, nil
}

func UpsertAssignment(db *gorm.DB, defID, scopeID uint, value []byte) (*models.ConfigAssignment, error) {
	def := models.ConfigVariableDef{}
	if err := db.First(&def, defID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "variable not found")
		}
		return nil, err
	}
	if _, err := GetScope(db, scopeID); err != nil {
		return nil, err
	}
	v, err := TypeCheckRaw(&def, value)
	if err != nil {
		return nil, statusErr(400, err.Error())
	}
	if def.Required && v == nil {
		return nil, statusErr(400, "required variable cannot be null")
	}
	var existing models.ConfigAssignment
	err = db.Where("variable_def_id = ? AND scope_id = ?", defID, scopeID).First(&existing).Error
	if err == nil {
		existing.Value = value
		if err := db.Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row := models.ConfigAssignment{VariableDefID: defID, ScopeID: scopeID, Value: value}
	if err := db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func ListAssignments(db *gorm.DB, scopeID uint) ([]models.ConfigAssignment, error) {
	q := db.Model(&models.ConfigAssignment{})
	if scopeID != 0 {
		q = q.Where("scope_id = ?", scopeID)
	}
	var rows []models.ConfigAssignment
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func DeleteAssignment(db *gorm.DB, id uint) error {
	var row models.ConfigAssignment
	if err := db.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return statusErr(404, "assignment not found")
		}
		return err
	}
	return db.Delete(&row).Error
}

// MatrixRows are interfaces under a scope subtree, with one variable resolved.
type MatrixRow struct {
	InterfaceID   uint   `json:"interface_id"`
	InterfaceName string `json:"interface_name"`
	DeviceID      uint   `json:"device_id"`
	DeviceName    string `json:"device_name"`
	ScopeID       *uint  `json:"scope_id,omitempty"`
	Value         any    `json:"value"`
	SourceID      *uint  `json:"source_id,omitempty"`
	SourceName    string `json:"source_name,omitempty"`
	FromDefault   bool   `json:"from_default"`
	Error         string `json:"error,omitempty"`
}

func Matrix(db *gorm.DB, scopeID uint, varName string) ([]MatrixRow, error) {
	if _, err := loadVarDef(db, varName); err != nil {
		return nil, err
	}
	scopes, err := DescendantScopes(db, scopeID)
	if err != nil {
		return nil, err
	}
	type ifaceRef struct {
		id, deviceID, scopeID uint
		name, deviceName      string
		hasScope              bool
	}
	seen := map[uint]bool{}
	var refs []ifaceRef
	add := func(ifaceID uint, scopeID uint, hasScope bool) error {
		if ifaceID == 0 || seen[ifaceID] {
			return nil
		}
		var iface models.Interface
		if err := db.First(&iface, ifaceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var dev models.Device
		_ = db.First(&dev, iface.DeviceID).Error
		seen[ifaceID] = true
		refs = append(refs, ifaceRef{
			id: iface.ID, deviceID: iface.DeviceID, scopeID: scopeID,
			name: iface.Name, deviceName: dev.Name, hasScope: hasScope,
		})
		return nil
	}
	for i := range scopes {
		s := scopes[i]
		switch s.Kind {
		case models.ConfigScopeKindInterface:
			if s.InterfaceID != nil {
				if err := add(*s.InterfaceID, s.ID, true); err != nil {
					return nil, err
				}
			}
		case models.ConfigScopeKindDevice:
			if s.DeviceID == nil {
				continue
			}
			var ifaces []models.Interface
			if err := db.Where("device_id = ?", *s.DeviceID).Find(&ifaces).Error; err != nil {
				return nil, err
			}
			for _, iface := range ifaces {
				child, err := scopeByInterfaceID(db, iface.ID)
				if err != nil {
					return nil, err
				}
				sid := s.ID
				has := false
				if child != nil {
					sid = child.ID
					has = true
				}
				if err := add(iface.ID, sid, has); err != nil {
					return nil, err
				}
			}
		}
	}
	rows := make([]MatrixRow, 0, len(refs))
	for _, r := range refs {
		row := MatrixRow{
			InterfaceID: r.id, InterfaceName: r.name,
			DeviceID: r.deviceID, DeviceName: r.deviceName,
		}
		if r.hasScope {
			sid := r.scopeID
			row.ScopeID = &sid
		}
		v, src, err := Resolve(db, r.id, varName)
		if err != nil {
			row.Error = err.Error()
			rows = append(rows, row)
			continue
		}
		row.Value = v
		if src != nil {
			id := src.ID
			row.SourceID = &id
			row.SourceName = src.Name
		} else if v != nil {
			row.FromDefault = true
			row.SourceName = "default"
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DeviceName != rows[j].DeviceName {
			return rows[i].DeviceName < rows[j].DeviceName
		}
		return rows[i].InterfaceName < rows[j].InterfaceName
	})
	return rows, nil
}

func ancestorIDs(db *gorm.DB, s *models.ConfigScope) (map[uint]bool, error) {
	chain, err := WalkParents(db, s)
	if err != nil {
		return nil, err
	}
	ids := make(map[uint]bool, len(chain))
	for _, n := range chain {
		ids[n.ID] = true
	}
	return ids, nil
}
