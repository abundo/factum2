package cfgmgmt

import (
	"errors"
	"fmt"
	"log/slog"
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
	ID            uint                      `json:"id"`
	Kind          string                    `json:"kind"`
	ParentID      *uint                     `json:"parent_id,omitempty"`
	SiteID        *uint                     `json:"site_id,omitempty"`
	DeviceID      *uint                     `json:"device_id,omitempty"`
	InterfaceID   *uint                     `json:"interface_id,omitempty"`
	ServiceID     *uint                     `json:"service_id,omitempty"`
	ServiceTypeID *uint                     `json:"service_type_id,omitempty"`
	Platform      string                    `json:"platform,omitempty"`
	PayloadKind   string                    `json:"payload_kind,omitempty"`
	Enabled       bool                      `json:"enabled"`
	SortOrder     int                       `json:"sort_order"`
	Payload       models.ConfigScopePayload `json:"payload"`
	CanonicalID   uint                      `json:"canonical_id,omitempty"`
	ServiceRowID  uint                      `json:"service_row_id,omitempty"`
	ServiceLabel  string                    `json:"service_label,omitempty"`
	Role          string                    `json:"role,omitempty"`
	Disc          string                    `json:"disc,omitempty"`
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
	var parent *models.ConfigScope
	if s.ParentID != nil {
		p, err := GetScope(db, *s.ParentID)
		if err != nil {
			return nil, err
		}
		parent = p
	} else {
		root, err := RootScope(db)
		if err != nil {
			return nil, err
		}
		s.ParentID = &root.ID
		parent = root
	}
	if err := assertParentKind(s, parent); err != nil {
		return nil, err
	}
	if err := assertScopeKindIDs(s); err != nil {
		return nil, err
	}
	if err := assertScopeUnique(db, s, 0); err != nil {
		return nil, err
	}
	if err := normalizeCLIScope(s); err != nil {
		return nil, err
	}
	if err := assertCLIUnique(db, s, 0); err != nil {
		return nil, err
	}
	s.Enabled = true
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
	case models.ConfigScopeKindService:
		if s.ServiceID == nil || *s.ServiceID == 0 {
			return statusErr(400, "service scope requires service_id")
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
	if s.Kind == models.ConfigScopeKindService && s.ServiceID != nil {
		q := db.Model(&models.ConfigScope{}).
			Where("kind = ? AND service_id = ?", models.ConfigScopeKindService, *s.ServiceID)
		if excludeID != 0 {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return statusErr(409, "a service scope already exists for this service")
		}
	}
	return nil
}

func UpdateScope(db *gorm.DB, id uint, patch *models.ConfigScopeDTO) (*models.ConfigScope, error) {
	if patch == nil {
		patch = &models.ConfigScopeDTO{}
	}
	var out *models.ConfigScope
	err := db.Transaction(func(tx *gorm.DB) error {
		existing, err := GetScope(tx, id)
		if err != nil {
			return err
		}
		root, err := RootScope(tx)
		if err != nil {
			return err
		}
		if existing.ID == root.ID {
			if parentChanged(existing, patch.ParentID) {
				return statusErr(400, "cannot reparent the global root")
			}
			if patch.Name != nil && *patch.Name != "" && *patch.Name != existing.Name {
				return statusErr(400, "cannot rename the global root")
			}
			if patch.Kind != nil && *patch.Kind != "" && *patch.Kind != existing.Kind {
				return statusErr(400, "cannot change the kind of the global root")
			}
		}
		if patch.Kind != nil && *patch.Kind != "" && !ValidScopeKind(*patch.Kind) {
			return statusErr(400, "invalid kind")
		}
		kindChanged := patch.Kind != nil && *patch.Kind != "" && *patch.Kind != existing.Kind
		if kindChanged {
			existing.Kind = *patch.Kind
		}
		if parentChanged(existing, patch.ParentID) {
			moved, err := moveScopeTx(tx, existing, *patch.ParentID, patch.SortOrder)
			if err != nil {
				return err
			}
			existing = moved
			if kindChanged {
				existing.Kind = *patch.Kind
			}
		} else {
			if kindChanged {
				if err := assertCurrentParentKind(tx, existing); err != nil {
					return err
				}
			}
			if patch.SortOrder != nil && existing.ParentID != nil {
				if err := placeAmongSiblings(tx, existing.ID, *existing.ParentID, patch.SortOrder); err != nil {
					return err
				}
				reloaded, err := GetScope(tx, id)
				if err != nil {
					return err
				}
				if kindChanged {
					reloaded.Kind = *patch.Kind
				}
				existing = reloaded
			}
		}
		if patch.Name != nil && *patch.Name != "" {
			existing.Name = *patch.Name
		}
		if patch.Kind != nil && *patch.Kind != "" {
			existing.Kind = *patch.Kind
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
		if patch.ServiceID != nil {
			existing.ServiceID = patch.ServiceID
		}
		if patch.ServiceTypeID != nil {
			if *patch.ServiceTypeID == 0 {
				existing.ServiceTypeID = nil
			} else {
				existing.ServiceTypeID = patch.ServiceTypeID
			}
		}
		if patch.Platform != nil {
			existing.Platform = *patch.Platform
		}
		if patch.PayloadKind != nil {
			existing.PayloadKind = *patch.PayloadKind
		}
		if patch.Payload != nil {
			existing.Payload = *patch.Payload
		}
		if patch.Enabled != nil {
			if existing.Kind == models.ConfigScopeKindParameter || existing.Kind == models.ConfigScopeKindCLI {
				existing.Enabled = *patch.Enabled
			} else if !*patch.Enabled {
				return statusErr(400, "enabled is only valid for parameter and cli")
			}
		}
		if err := assertScopeKindIDs(existing); err != nil {
			return err
		}
		if err := assertScopeUnique(tx, existing, existing.ID); err != nil {
			return err
		}
		if err := normalizeCLIScope(existing); err != nil {
			return err
		}
		if err := assertCLIUnique(tx, existing, existing.ID); err != nil {
			return err
		}
		if err := tx.Save(existing).Error; err != nil {
			return err
		}
		out = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
	if existing.Kind == models.ConfigScopeKindInterface {
		return statusErr(409, "managed by device")
	}
	if existing.Kind == models.ConfigScopeKindService {
		return detachServiceNode(db, existing)
	}
	if existing.Kind == models.ConfigScopeKindServiceEndpoint {
		return deleteServiceEndpointScope(db, existing)
	}
	var n int64
	if err := db.Model(&models.ConfigScope{}).Where("parent_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return statusErr(409, "scope has children")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return deleteScopeSubtree(tx, []models.ConfigScope{*existing})
	})
}

func deleteServiceEndpointScope(db *gorm.DB, child *models.ConfigScope) error {
	var serviceRowID uint
	if child.ServiceID != nil {
		serviceRowID = *child.ServiceID
	}
	if serviceRowID == 0 && child.ParentID != nil {
		parent, err := GetScope(db, *child.ParentID)
		if err != nil {
			return err
		}
		if parent.ServiceID != nil {
			serviceRowID = *parent.ServiceID
		}
	}
	if serviceRowID == 0 {
		return db.Transaction(func(tx *gorm.DB) error {
			return deleteScopeSubtree(tx, []models.ConfigScope{*child})
		})
	}
	eps, err := ListEndpoints(db, serviceRowID)
	if err != nil {
		return err
	}
	drop := identityFromEndpointScope(child)
	remaining := make([]models.ServiceEndpoint, 0, len(eps))
	matched := false
	for _, ep := range eps {
		ep.ServiceID = serviceRowID
		if EndpointIdentity(ep) == drop {
			matched = true
			continue
		}
		remaining = append(remaining, ep)
	}
	if !matched {
		return db.Transaction(func(tx *gorm.DB) error {
			desc, err := DescendantScopes(tx, child.ID)
			if err != nil {
				return err
			}
			return deleteScopeSubtree(tx, desc)
		})
	}
	var svc models.Service
	if err := db.First(&svc, serviceRowID).Error; err != nil {
		return err
	}
	st, err := LookupServiceType(db, svc.ServiceType)
	if err != nil {
		return err
	}
	if err := ValidateEndpoints(db, st, remaining); err != nil {
		return err
	}
	if svc.ServiceType == "ELINE" {
		if err := ValidateELINEShape(db, remaining); err != nil {
			return err
		}
	}
	return ReplaceEndpoints(db, serviceRowID, remaining)
}

func parentChanged(existing *models.ConfigScope, newParent *uint) bool {
	if newParent == nil {
		return false
	}
	if existing.ParentID == nil {
		return true
	}
	return *existing.ParentID != *newParent
}

func isReservedFolder(s *models.ConfigScope, rootID uint) bool {
	if s == nil || s.Kind != models.ConfigScopeKindFolder {
		return false
	}
	if s.ParentID == nil {
		return s.Name == models.ConfigRootName
	}
	if *s.ParentID != rootID {
		return false
	}
	return s.Name == models.ConfigCatalogName || s.Name == models.ConfigServicesFolderName
}

func assertCurrentParentKind(db *gorm.DB, child *models.ConfigScope) error {
	if child == nil || child.ParentID == nil {
		return nil
	}
	parent, err := GetScope(db, *child.ParentID)
	if err != nil {
		return err
	}
	return assertParentKind(child, parent)
}

func organizationalParentKind(k string) bool {
	switch k {
	case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation:
		return true
	}
	return false
}

func assertParentKind(child, parent *models.ConfigScope) error {
	if child == nil || parent == nil {
		return statusErr(400, "parent scope not found")
	}
	switch child.Kind {
	case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation:
		if organizationalParentKind(parent.Kind) {
			return nil
		}
		return statusErr(400, child.Kind+" cannot be under "+parent.Kind)
	case models.ConfigScopeKindDevice:
		if organizationalParentKind(parent.Kind) {
			return nil
		}
		return statusErr(400, "device cannot be under "+parent.Kind)
	case models.ConfigScopeKindInterface:
		if parent.Kind != models.ConfigScopeKindDevice {
			return statusErr(400, "interface must be under its device")
		}
		if child.DeviceID == nil || parent.DeviceID == nil || *child.DeviceID != *parent.DeviceID {
			return statusErr(400, "interface must be under its device")
		}
		return nil
	case models.ConfigScopeKindParameter:
		switch parent.Kind {
		case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation,
			models.ConfigScopeKindDevice, models.ConfigScopeKindInterface, models.ConfigScopeKindService:
			return nil
		}
		return statusErr(400, "parameter cannot be under "+parent.Kind)
	case models.ConfigScopeKindCLI:
		switch parent.Kind {
		case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation,
			models.ConfigScopeKindDevice, models.ConfigScopeKindInterface:
			return nil
		}
		return statusErr(400, "cli cannot be under "+parent.Kind)
	case models.ConfigScopeKindService:
		switch parent.Kind {
		case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation,
			models.ConfigScopeKindDevice:
			return nil
		}
		return statusErr(400, "service cannot be under "+parent.Kind)
	case models.ConfigScopeKindServiceEndpoint:
		if parent.Kind != models.ConfigScopeKindService {
			return statusErr(400, "service_endpoint must be under its service")
		}
		if child.ServiceID != nil && parent.ServiceID != nil && *child.ServiceID != *parent.ServiceID {
			return statusErr(400, "service_endpoint must be under its service")
		}
		return nil
	default:
		return statusErr(400, "invalid kind")
	}
}

func placeAmongSiblings(db *gorm.DB, nodeID, parentID uint, want *int) error {
	var siblings []models.ConfigScope
	if err := db.Where("parent_id = ? AND id <> ?", parentID, nodeID).
		Order("sort_order, name").Find(&siblings).Error; err != nil {
		return err
	}
	pos := len(siblings)
	if want != nil {
		pos = *want
		if pos < 0 {
			pos = 0
		}
		if pos > len(siblings) {
			pos = len(siblings)
		}
	}
	write := func(id uint, order int) error {
		return db.Model(&models.ConfigScope{}).Where("id = ?", id).Update("sort_order", order).Error
	}
	i := 0
	for _, s := range siblings {
		if i == pos {
			if err := write(nodeID, i); err != nil {
				return err
			}
			i++
		}
		if err := write(s.ID, i); err != nil {
			return err
		}
		i++
	}
	if pos >= i {
		if err := write(nodeID, i); err != nil {
			return err
		}
	}
	return nil
}

func compactSiblings(db *gorm.DB, parentID uint) error {
	var siblings []models.ConfigScope
	if err := db.Where("parent_id = ?", parentID).Order("sort_order, name").Find(&siblings).Error; err != nil {
		return err
	}
	for i := range siblings {
		if siblings[i].SortOrder == i {
			continue
		}
		if err := db.Model(&models.ConfigScope{}).Where("id = ?", siblings[i].ID).
			Update("sort_order", i).Error; err != nil {
			return err
		}
	}
	return nil
}

// MoveScope reparents id under parentID. sortOrder nil means last sibling.
func MoveScope(db *gorm.DB, id, parentID uint, sortOrder *int) (*models.ConfigScope, error) {
	var out *models.ConfigScope
	err := db.Transaction(func(tx *gorm.DB) error {
		existing, err := GetScope(tx, id)
		if err != nil {
			return err
		}
		node, err := moveScopeTx(tx, existing, parentID, sortOrder)
		if err != nil {
			return err
		}
		out = node
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func moveScopeTx(tx *gorm.DB, existing *models.ConfigScope, parentID uint, sortOrder *int) (*models.ConfigScope, error) {
	root, err := RootScope(tx)
	if err != nil {
		return nil, err
	}
	if existing.ID == root.ID {
		return nil, statusErr(400, "cannot reparent the global root")
	}
	if existing.Kind == models.ConfigScopeKindInterface {
		return nil, statusErr(409, "managed by device")
	}
	parent, err := GetScope(tx, parentID)
	if err != nil {
		return nil, err
	}
	changed := parentChanged(existing, &parentID)
	if changed && isReservedFolder(existing, root.ID) {
		return nil, statusErr(400, "cannot reparent reserved folder")
	}
	if existing.Kind == models.ConfigScopeKindServiceEndpoint && changed {
		return nil, statusErr(409, "service_endpoint parent cannot change")
	}
	if changed {
		if parentID == existing.ID {
			return nil, statusErr(400, "scope cannot be its own parent")
		}
		cycle, err := WouldCycle(tx, existing.ID, parentID)
		if err != nil {
			return nil, err
		}
		if cycle {
			return nil, statusErr(400, "scope parent cycle")
		}
		if err := assertParentKind(existing, parent); err != nil {
			return nil, err
		}
	}
	oldParent := existing.ParentID
	if changed && existing.Kind == models.ConfigScopeKindDevice {
		if existing.DeviceID == nil || *existing.DeviceID == 0 {
			return nil, statusErr(400, "device scope requires device_id")
		}
		node, err := AttachDevice(tx, parentID, *existing.DeviceID)
		if err != nil {
			return nil, err
		}
		existing = node
	} else if changed {
		existing.ParentID = &parentID
		if err := tx.Save(existing).Error; err != nil {
			return nil, err
		}
	}
	if err := placeAmongSiblings(tx, existing.ID, parentID, sortOrder); err != nil {
		return nil, err
	}
	if changed && oldParent != nil && *oldParent != parentID {
		if err := compactSiblings(tx, *oldParent); err != nil {
			return nil, err
		}
	}
	existing, err = GetScope(tx, existing.ID)
	if err != nil {
		return nil, err
	}
	slog.Info("cfgmgmt move", "scope", existing.ID, "kind", existing.Kind, "parent", parentID)
	return existing, nil
}

func deleteScopeSubtree(db *gorm.DB, scopes []models.ConfigScope) error {
	for i := len(scopes) - 1; i >= 0; i-- {
		s := &scopes[i]
		if s.Kind == models.ConfigScopeKindParameter {
			if err := deleteParameterAssignments(db, s); err != nil {
				return err
			}
		}
		if err := db.Where("scope_id = ?", s.ID).Delete(&models.ConfigAssignment{}).Error; err != nil {
			return err
		}
		if err := db.Where("scope_id = ?", s.ID).Delete(&models.ConfigCLIFeature{}).Error; err != nil {
			return err
		}
		if err := db.Delete(s).Error; err != nil {
			return err
		}
	}
	return nil
}

func descendantIDs(db *gorm.DB, rootID uint) (map[uint]bool, error) {
	rows, err := DescendantScopes(db, rootID)
	if err != nil {
		return nil, err
	}
	ids := make(map[uint]bool, len(rows))
	for _, s := range rows {
		ids[s.ID] = true
	}
	return ids, nil
}

func servicesFolder(db *gorm.DB) (*models.ConfigScope, error) {
	root, err := RootScope(db)
	if err != nil {
		return nil, err
	}
	return ensureChildFolder(db, root.ID, models.ConfigServicesFolderName)
}

func detachDestParent(db *gorm.DB, device *models.ConfigScope) (*uint, error) {
	if device.ParentID != nil {
		if _, err := GetScope(db, *device.ParentID); err == nil {
			return device.ParentID, nil
		} else if se := AsStatusError(err); se == nil || se.Status != 404 {
			return nil, err
		}
	}
	sf, err := servicesFolder(db)
	if err != nil {
		return nil, err
	}
	return &sf.ID, nil
}

// DetachDevice removes a device scope and its config descendants. kind=service
// children are reparented. The DCIM device row is never deleted.
func DetachDevice(db *gorm.DB, id uint) error {
	existing, err := GetScope(db, id)
	if err != nil {
		return err
	}
	if existing.Kind != models.ConfigScopeKindDevice {
		return statusErr(400, "detach is only for device scopes")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		dest, err := detachDestParent(tx, existing)
		if err != nil {
			return err
		}
		var serviceKids []models.ConfigScope
		if err := tx.Where("parent_id = ? AND kind = ?", existing.ID, models.ConfigScopeKindService).
			Find(&serviceKids).Error; err != nil {
			return err
		}
		keep := map[uint]bool{}
		for i := range serviceKids {
			ids, err := descendantIDs(tx, serviceKids[i].ID)
			if err != nil {
				return err
			}
			for id := range ids {
				keep[id] = true
			}
			serviceKids[i].ParentID = dest
			if err := tx.Save(&serviceKids[i]).Error; err != nil {
				return err
			}
		}
		desc, err := DescendantScopes(tx, existing.ID)
		if err != nil {
			return err
		}
		var toDelete []models.ConfigScope
		for _, s := range desc {
			if keep[s.ID] {
				continue
			}
			toDelete = append(toDelete, s)
		}
		if err := deleteScopeSubtree(tx, toDelete); err != nil {
			return err
		}
		parentID := uint(0)
		if dest != nil {
			parentID = *dest
		}
		slog.Info("cfgmgmt detach", "scope", existing.ID, "kind", existing.Kind, "parent", parentID)
		return nil
	})
}

func isOrganizationalScope(s *models.ConfigScope) bool {
	if s == nil {
		return false
	}
	switch s.Kind {
	case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation,
		models.ConfigScopeKindDevice, models.ConfigScopeKindInterface:
		return true
	}
	return false
}

func findParametersChild(db *gorm.DB, parentID uint) (*models.ConfigScope, error) {
	var s models.ConfigScope
	err := db.Where("parent_id = ? AND kind = ? AND name = ?", parentID, models.ConfigScopeKindParameter, models.ConfigParametersChildName).
		First(&s).Error
	if err == nil {
		return &s, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, err
}

func ensureParametersChild(db *gorm.DB, parentID uint) (*models.ConfigScope, error) {
	existing, err := findParametersChild(db, parentID)
	if err != nil || existing != nil {
		return existing, err
	}
	s := models.ConfigScope{
		ParentID:  &parentID,
		Name:      models.ConfigParametersChildName,
		Kind:      models.ConfigScopeKindParameter,
		SortOrder: 0,
		Enabled:   true,
	}
	if err := db.Create(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// reservedParametersParent returns the organizational parent when s is the
// reserved migrated "parameters" child; otherwise nil.
func reservedParametersParent(db *gorm.DB, s *models.ConfigScope) (*models.ConfigScope, error) {
	if s == nil || s.Kind != models.ConfigScopeKindParameter || s.Name != models.ConfigParametersChildName || s.ParentID == nil {
		return nil, nil
	}
	parent, err := GetScope(db, *s.ParentID)
	if err != nil {
		if se := AsStatusError(err); se != nil && se.Status == 404 {
			return nil, nil
		}
		return nil, err
	}
	if !isOrganizationalScope(parent) {
		return nil, nil
	}
	return parent, nil
}

func deleteParameterAssignments(db *gorm.DB, scope *models.ConfigScope) error {
	parent, err := reservedParametersParent(db, scope)
	if err != nil {
		return err
	}
	var childRows []models.ConfigAssignment
	if err := db.Where("scope_id = ?", scope.ID).Find(&childRows).Error; err != nil {
		return err
	}
	if err := db.Where("scope_id = ?", scope.ID).Delete(&models.ConfigAssignment{}).Error; err != nil {
		return err
	}
	if parent == nil {
		return nil
	}
	for _, a := range childRows {
		if err := db.Where("variable_def_id = ? AND scope_id = ?", a.VariableDefID, parent.ID).
			Delete(&models.ConfigAssignment{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// AttachDevice creates or updates a device-kind node under parentID and
// ensures a child interface node exists for each inventory interface.
func AttachDevice(db *gorm.DB, parentID, deviceID uint) (*models.ConfigScope, error) {
	parent, err := GetScope(db, parentID)
	if err != nil {
		return nil, err
	}
	probe := &models.ConfigScope{Kind: models.ConfigScopeKindDevice, DeviceID: &deviceID}
	if err := assertParentKind(probe, parent); err != nil {
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
	refs, err := virtualServiceRefs(db, rows)
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
		out = append(out, buildTreeNode(&roots[i], byParent, refs, path))
	}
	return out, nil
}

func virtualServiceRefs(db *gorm.DB, scopes []models.ConfigScope) (map[uint][]ScopeTreeNode, error) {
	var eps []models.ServiceEndpoint
	if err := db.Find(&eps).Error; err != nil {
		return nil, err
	}
	if len(eps) == 0 {
		return nil, nil
	}
	svcIDs := make([]uint, 0, len(eps))
	seenSvc := map[uint]bool{}
	for _, ep := range eps {
		if seenSvc[ep.ServiceID] {
			continue
		}
		seenSvc[ep.ServiceID] = true
		svcIDs = append(svcIDs, ep.ServiceID)
	}
	var svcs []models.Service
	if err := db.Where("id IN ?", svcIDs).Find(&svcs).Error; err != nil {
		return nil, err
	}
	labelByID := make(map[uint]string, len(svcs))
	for _, s := range svcs {
		label := s.ServiceID
		if label == "" {
			label = fmt.Sprintf("service-%d", s.ID)
		}
		labelByID[s.ID] = label
	}
	ifaceByID := map[uint]*models.ConfigScope{}
	deviceByID := map[uint]*models.ConfigScope{}
	canonBySvc := map[uint]*models.ConfigScope{}
	for i := range scopes {
		s := &scopes[i]
		switch s.Kind {
		case models.ConfigScopeKindInterface:
			if s.InterfaceID != nil {
				ifaceByID[*s.InterfaceID] = s
			}
		case models.ConfigScopeKindDevice:
			if s.DeviceID != nil {
				deviceByID[*s.DeviceID] = s
			}
		case models.ConfigScopeKindService:
			if s.ServiceID != nil {
				canonBySvc[*s.ServiceID] = s
			}
		}
	}
	out := map[uint][]ScopeTreeNode{}
	for i := range eps {
		ep := eps[i]
		parent := ifaceByID[ep.InterfaceID]
		if parent == nil {
			parent = deviceByID[ep.DeviceID]
		}
		if parent == nil {
			continue
		}
		label := labelByID[ep.ServiceID]
		disc := endpointDisc(ep.Fields)
		ident := EndpointIdentity(ep)
		var canonicalID uint
		if c := canonBySvc[ep.ServiceID]; c != nil {
			canonicalID = c.ID
		}
		did, iid, sid := ep.DeviceID, ep.InterfaceID, ep.ServiceID
		title := label + " (" + ep.Role + ")"
		if vlan := VLANFromFields(ep.Fields); vlan != 0 {
			title = fmt.Sprintf("%s (%s · %d)", label, ep.Role, vlan)
		}
		node := ScopeTreeNode{
			Key:   "ref:" + ident,
			Title: title,
			Type:  models.ConfigScopeKindServiceRef,
			Data: ScopeTreeData{
				Kind:         models.ConfigScopeKindServiceRef,
				ParentID:     &parent.ID,
				DeviceID:     &did,
				InterfaceID:  &iid,
				ServiceID:    &sid,
				CanonicalID:  canonicalID,
				ServiceRowID: ep.ServiceID,
				ServiceLabel: label,
				Role:         ep.Role,
				Disc:         disc,
			},
		}
		out[parent.ID] = append(out[parent.ID], node)
	}
	for pid := range out {
		sort.Slice(out[pid], func(i, j int) bool {
			return out[pid][i].Title < out[pid][j].Title
		})
	}
	return out, nil
}

func buildTreeNode(s *models.ConfigScope, byParent map[uint][]models.ConfigScope, refs map[uint][]ScopeTreeNode, path map[uint]bool) ScopeTreeNode {
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
			ID:            s.ID,
			Kind:          s.Kind,
			ParentID:      s.ParentID,
			SiteID:        s.SiteID,
			DeviceID:      s.DeviceID,
			InterfaceID:   s.InterfaceID,
			ServiceID:     s.ServiceID,
			ServiceTypeID: s.ServiceTypeID,
			Platform:      s.Platform,
			PayloadKind:   s.PayloadKind,
			Enabled:       s.Enabled,
			SortOrder:     s.SortOrder,
			Payload:       s.Payload,
		},
	}
	if path[s.ID] {
		return n
	}
	path[s.ID] = true
	for i := range kids {
		n.Children = append(n.Children, buildTreeNode(&kids[i], byParent, refs, path))
	}
	n.Children = append(n.Children, refs[s.ID]...)
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

func upsertAssignmentAt(db *gorm.DB, defID, scopeID uint, value []byte) (*models.ConfigAssignment, error) {
	var existing models.ConfigAssignment
	err := db.Where("variable_def_id = ? AND scope_id = ?", defID, scopeID).First(&existing).Error
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

func existingSecretAssignment(db *gorm.DB, def *models.ConfigVariableDef, scope *models.ConfigScope) (*models.ConfigAssignment, error) {
	ids := []uint{scope.ID}
	if isOrganizationalScope(scope) {
		child, err := findParametersChild(db, scope.ID)
		if err != nil {
			return nil, err
		}
		if child != nil {
			ids = []uint{child.ID, scope.ID}
		}
	} else if parent, err := reservedParametersParent(db, scope); err != nil {
		return nil, err
	} else if parent != nil {
		ids = []uint{scope.ID, parent.ID}
	}
	for _, id := range ids {
		existing, err := assignmentAt(db, def.ID, id)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	return nil, nil
}

// dualWriteScopeIDs remaps a folder/device/interface (or the reserved
// parameters child) onto that child as the primary write target. also is
// the organizational original, updated only when that row already exists.
func dualWriteScopeIDs(db *gorm.DB, scope *models.ConfigScope) (primary uint, also *uint, err error) {
	if isOrganizationalScope(scope) {
		child, err := ensureParametersChild(db, scope.ID)
		if err != nil {
			return 0, nil, err
		}
		orig := scope.ID
		return child.ID, &orig, nil
	}
	parent, err := reservedParametersParent(db, scope)
	if err != nil {
		return 0, nil, err
	}
	if parent != nil {
		pid := parent.ID
		return scope.ID, &pid, nil
	}
	return scope.ID, nil, nil
}

func UpsertAssignment(db *gorm.DB, defID, scopeID uint, value []byte) (*models.ConfigAssignment, error) {
	def := models.ConfigVariableDef{}
	if err := db.First(&def, defID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, statusErr(404, "variable not found")
		}
		return nil, err
	}
	scope, err := GetScope(db, scopeID)
	if err != nil {
		return nil, err
	}
	if isSecretDef(&def) && SecretDefaultUnchanged(value) {
		existing, err := existingSecretAssignment(db, &def, scope)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, statusErr(400, "secret value is required")
		}
		return existing, nil
	}
	v, err := TypeCheckRaw(&def, value)
	if err != nil {
		return nil, statusErr(400, err.Error())
	}
	if def.Required && v == nil {
		return nil, statusErr(400, "required variable cannot be null")
	}
	var out *models.ConfigAssignment
	err = db.Transaction(func(tx *gorm.DB) error {
		primaryID, alsoID, err := dualWriteScopeIDs(tx, scope)
		if err != nil {
			return err
		}
		row, err := upsertAssignmentAt(tx, defID, primaryID, value)
		if err != nil {
			return err
		}
		out = row
		if alsoID != nil && *alsoID != primaryID {
			existing, err := assignmentAt(tx, defID, *alsoID)
			if err != nil {
				return err
			}
			if existing != nil {
				if _, err := upsertAssignmentAt(tx, defID, *alsoID, value); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func ListAssignments(db *gorm.DB, scopeID uint) ([]models.ConfigAssignment, error) {
	if scopeID == 0 {
		// Every assignment row. Callers that want one winner per
		// variable pass a non-zero scope_id.
		var rows []models.ConfigAssignment
		if err := db.Find(&rows).Error; err != nil {
			return nil, err
		}
		return rows, nil
	}
	scope, err := GetScope(db, scopeID)
	if err != nil {
		if se := AsStatusError(err); se != nil && se.Status == 404 {
			return []models.ConfigAssignment{}, nil
		}
		return nil, err
	}
	if isOrganizationalScope(scope) {
		return winningAssignments(db, scope)
	}
	var rows []models.ConfigAssignment
	if err := db.Where("scope_id = ?", scopeID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func winningAssignments(db *gorm.DB, scope *models.ConfigScope) ([]models.ConfigAssignment, error) {
	var originals []models.ConfigAssignment
	if err := db.Where("scope_id = ?", scope.ID).Find(&originals).Error; err != nil {
		return nil, err
	}
	child, err := findParametersChild(db, scope.ID)
	if err != nil {
		return nil, err
	}
	byDef := map[uint]models.ConfigAssignment{}
	order := make([]uint, 0, len(originals))
	for _, a := range originals {
		if _, ok := byDef[a.VariableDefID]; ok {
			continue
		}
		byDef[a.VariableDefID] = a
		order = append(order, a.VariableDefID)
	}
	if child != nil {
		var copies []models.ConfigAssignment
		if err := db.Where("scope_id = ?", child.ID).Find(&copies).Error; err != nil {
			return nil, err
		}
		for _, a := range copies {
			if _, ok := byDef[a.VariableDefID]; !ok {
				order = append(order, a.VariableDefID)
			}
			byDef[a.VariableDefID] = a
		}
	}
	out := make([]models.ConfigAssignment, 0, len(order))
	for _, id := range order {
		out = append(out, byDef[id])
	}
	return out, nil
}

func DeleteAssignment(db *gorm.DB, id uint) error {
	var row models.ConfigAssignment
	if err := db.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return statusErr(404, "assignment not found")
		}
		return err
	}
	scope, err := GetScope(db, row.ScopeID)
	if err != nil {
		if se := AsStatusError(err); se != nil && se.Status == 404 {
			return db.Delete(&row).Error
		}
		return err
	}
	parent, err := reservedParametersParent(db, scope)
	if err != nil {
		return err
	}
	var childID uint
	if isOrganizationalScope(scope) {
		child, err := findParametersChild(db, scope.ID)
		if err != nil {
			return err
		}
		if child != nil {
			childID = child.ID
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if parent != nil {
			if err := tx.Where("variable_def_id = ? AND scope_id = ?", row.VariableDefID, parent.ID).
				Delete(&models.ConfigAssignment{}).Error; err != nil {
				return err
			}
		}
		if childID != 0 {
			if err := tx.Where("variable_def_id = ? AND scope_id = ?", row.VariableDefID, childID).
				Delete(&models.ConfigAssignment{}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&row).Error
	})
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
	start, err := GetScope(db, scopeID)
	if err != nil {
		return nil, err
	}
	start, err = nearestMatrixScope(db, start)
	if err != nil {
		return nil, err
	}
	scopes, err := DescendantScopes(db, start.ID)
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
