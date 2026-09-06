package cfgmgmt

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// serviceIDShape matches the <category><5 digits> numbering used by
// ApiServiceCreate / CreateServiceFromTree.
var serviceIDShape = regexp.MustCompile(`^([A-Z]{2})(\d{5})$`)

func isCapacityCategory(cat string) bool {
	return cat == "CN" || cat == "CI"
}

func intFromFields(dtoVal int, raw []byte, key string) int {
	if dtoVal != 0 {
		return dtoVal
	}
	n, ok := asInt(fieldsMap(raw)[key])
	if !ok || n < 0 {
		return 0
	}
	return int(n)
}

func nextServiceID(tx *gorm.DB, category string) (string, error) {
	var existing []models.Service
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("service_id LIKE ?", category+"%").
		Find(&existing).Error; err != nil {
		return "", err
	}
	next := 1
	for _, svc := range existing {
		m := serviceIDShape.FindStringSubmatch(svc.ServiceID)
		if m == nil || m[1] != category {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if n+1 > next {
			next = n + 1
		}
	}
	return fmt.Sprintf("%s%05d", category, next), nil
}

// CreateServiceRecord inserts a services row using the same numbering and
// field-copy rules as ApiServiceCreate. The caller must validate category
// and service type first and run this inside a transaction when allocating
// the next <type><5-digit> id.
func CreateServiceRecord(tx *gorm.DB, dto *models.ServiceDTO) (*models.Service, error) {
	if dto == nil {
		return nil, statusErr(400, "service is required")
	}
	serviceID := strings.TrimSpace(dto.ServiceID)
	if serviceID == "" {
		id, err := nextServiceID(tx, dto.Category)
		if err != nil {
			return nil, err
		}
		serviceID = id
	} else {
		var count int64
		if err := tx.Model(&models.Service{}).
			Where("service_id = ?", serviceID).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, statusErrf(400, "service ID %q is already in use", serviceID)
		}
	}
	created := models.Service{
		CustomerID:      dto.CustomerID,
		Comment:         dto.Comment,
		ServiceID:       serviceID,
		ServiceType:     dto.ServiceType,
		BandwidthMbps:   intFromFields(dto.BandwidthMbps, dto.Fields, models.SchemaFieldBandwidthMbps),
		MaxMacAddresses: intFromFields(dto.MaxMacAddresses, dto.Fields, models.SchemaFieldMaxMacAddresses),
		DeliveryPoint1:  dto.DeliveryPoint1,
		DeliveryPoint2:  dto.DeliveryPoint2,
		Product:         dto.Product,
		Service:         dto.Service,
		Fields:          dto.Fields,
	}
	if err := tx.Create(&created).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func typedServiceRow(db *gorm.DB, id uint) (*models.Service, error) {
	var svc models.Service
	if err := db.First(&svc, id).Error; err != nil {
		if errorsIsNotFound(err) {
			return nil, statusErr(404, "service not found")
		}
		return nil, err
	}
	if err := assertTypedCapacityService(&svc); err != nil {
		return nil, err
	}
	if _, err := LookupServiceType(db, svc.ServiceType); err != nil {
		return nil, err
	}
	return &svc, nil
}

func assertTypedCapacityService(svc *models.Service) error {
	if svc == nil {
		return statusErr(400, "service is required")
	}
	cat := models.CategoryFromServiceID(svc.ServiceID)
	if models.OpticalServiceCategories[cat] {
		return statusErr(400, "wavelength and dark fiber services cannot be added to the config tree")
	}
	if svc.ServiceType == "" {
		return statusErr(400, "service has no cfgmgmt type")
	}
	return nil
}

func insertCanonicalService(tx *gorm.DB, parentID uint, svc *models.Service) (*models.ConfigScope, error) {
	if existing, err := scopeByServiceID(tx, svc.ID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, statusErr(409, "a service scope already exists for this service")
	}
	parent, err := GetScope(tx, parentID)
	if err != nil {
		return nil, err
	}
	sid := svc.ID
	probe := &models.ConfigScope{Kind: models.ConfigScopeKindService, ServiceID: &sid}
	if err := assertParentKind(probe, parent); err != nil {
		return nil, err
	}
	name := svc.ServiceID
	if name == "" {
		name = fmt.Sprintf("service-%d", svc.ID)
	}
	node := models.ConfigScope{
		ParentID:  &parentID,
		Name:      name,
		Kind:      models.ConfigScopeKindService,
		ServiceID: &sid,
		Enabled:   true,
	}
	if err := tx.Create(&node).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

// CreateServiceFromTree creates a CN/CI inventory row and a canonical
// kind=service scope with zero endpoints.
func CreateServiceFromTree(db *gorm.DB, parentID uint, dto *models.ServiceDTO) (*models.ConfigScope, error) {
	if dto == nil {
		return nil, statusErr(400, "attach is required")
	}
	if !isCapacityCategory(dto.Category) {
		return nil, statusErr(400, "tree create is only for CN/CI services")
	}
	if dto.ServiceType == "" {
		return nil, statusErr(400, "service_type is required")
	}
	if _, err := LookupServiceType(db, dto.ServiceType); err != nil {
		return nil, err
	}
	var node *models.ConfigScope
	err := db.Transaction(func(tx *gorm.DB) error {
		svc, err := CreateServiceRecord(tx, dto)
		if err != nil {
			return err
		}
		created, err := insertCanonicalService(tx, parentID, svc)
		if err != nil {
			return err
		}
		node = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return node, nil
}

// AttachService places an existing typed CN/CI row in the tree and projects
// endpoint children. Lime rows are allowed. VL/VI/LF/LI are rejected.
func AttachService(db *gorm.DB, parentID, serviceRowID uint) (*models.ConfigScope, error) {
	svc, err := typedServiceRow(db, serviceRowID)
	if err != nil {
		return nil, err
	}
	var node *models.ConfigScope
	err = db.Transaction(func(tx *gorm.DB) error {
		created, err := insertCanonicalService(tx, parentID, svc)
		if err != nil {
			return err
		}
		if err := projectEndpointScopes(tx, svc.ID); err != nil {
			return err
		}
		node = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return node, nil
}

// CanonicalServiceParentID is the parent for a new canonical node started
// from from. Interface/device create uses the device's folder/site/location
// parent when that parent is organizational, otherwise _services.
func CanonicalServiceParentID(db *gorm.DB, from *models.ConfigScope) (uint, error) {
	if from == nil {
		sf, err := servicesFolder(db)
		if err != nil {
			return 0, err
		}
		return sf.ID, nil
	}
	switch from.Kind {
	case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation:
		return from.ID, nil
	case models.ConfigScopeKindDevice:
		if from.ParentID != nil {
			p, err := GetScope(db, *from.ParentID)
			if err == nil && organizationalParentKind(p.Kind) {
				return p.ID, nil
			} else if err != nil {
				if se := AsStatusError(err); se == nil || se.Status != 404 {
					return 0, err
				}
			}
		}
		sf, err := servicesFolder(db)
		if err != nil {
			return 0, err
		}
		return sf.ID, nil
	case models.ConfigScopeKindInterface:
		if from.ParentID == nil {
			sf, err := servicesFolder(db)
			if err != nil {
				return 0, err
			}
			return sf.ID, nil
		}
		dev, err := GetScope(db, *from.ParentID)
		if err != nil {
			return 0, err
		}
		return CanonicalServiceParentID(db, dev)
	default:
		sf, err := servicesFolder(db)
		if err != nil {
			return 0, err
		}
		return sf.ID, nil
	}
}

func scopeByServiceID(db *gorm.DB, serviceID uint) (*models.ConfigScope, error) {
	var s models.ConfigScope
	err := db.Where("kind = ? AND service_id = ?", models.ConfigScopeKindService, serviceID).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if errorsIsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

func errorsIsNotFound(err error) bool {
	return err != nil && errors.Is(err, gorm.ErrRecordNotFound)
}

// DetachServiceByRowID drops the canonical node and its config descendants.
// Inventory rows are left in place. No-op if the service is not in the tree.
func DetachServiceByRowID(db *gorm.DB, serviceRowID uint) error {
	node, err := scopeByServiceID(db, serviceRowID)
	if err != nil || node == nil {
		return err
	}
	return detachServiceNode(db, node)
}

func detachServiceNode(db *gorm.DB, canonical *models.ConfigScope) error {
	return db.Transaction(func(tx *gorm.DB) error {
		desc, err := DescendantScopes(tx, canonical.ID)
		if err != nil {
			return err
		}
		return deleteScopeSubtree(tx, desc)
	})
}
