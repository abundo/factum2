package cfgmgmt

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func roleByName(st *models.ServiceType, name string) *models.EndpointRole {
	for i := range st.EndpointRoles {
		if st.EndpointRoles[i].Name == name {
			return &st.EndpointRoles[i]
		}
	}
	return nil
}

// ValidateEndpoints checks endpoints against the service type's EndpointRoles
// and that each device/interface pair exists in inventory.
func ValidateEndpoints(db *gorm.DB, st *models.ServiceType, eps []models.ServiceEndpoint) error {
	if st == nil {
		return statusErr(400, "service type required")
	}
	counts := map[string]int{}
	for i := range eps {
		ep := &eps[i]
		if ep.Role == "" {
			return statusErr(400, "endpoint role is required")
		}
		if ep.DeviceID == 0 || ep.InterfaceID == 0 {
			return statusErr(400, "endpoint device_id and interface_id are required")
		}
		var device models.Device
		if err := db.First(&device, ep.DeviceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return statusErr(400, "endpoint device not found")
			}
			return err
		}
		var iface models.Interface
		if err := db.First(&iface, ep.InterfaceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return statusErr(400, "endpoint interface not found")
			}
			return err
		}
		if iface.DeviceID != ep.DeviceID {
			return statusErr(400, "endpoint interface does not belong to the given device")
		}
		role := roleByName(st, ep.Role)
		if role == nil {
			return statusErrf(400, "unknown endpoint role %q", ep.Role)
		}
		counts[ep.Role]++
		fields := fieldsMap(ep.Fields)
		for _, f := range role.Fields {
			v, ok := fields[f.Name]
			if !f.Required {
				if !ok || v == nil {
					continue
				}
			} else if !ok || v == nil {
				return statusErrf(400, "endpoint role %q missing required field %q", ep.Role, f.Name)
			}
			def := &models.ConfigVariableDef{Name: f.Name, Type: f.Type}
			checked, err := TypeCheck(def, v)
			if err != nil {
				return statusErr(400, err.Error())
			}
			if f.Required && checked == nil {
				return statusErrf(400, "endpoint role %q missing required field %q", ep.Role, f.Name)
			}
		}
	}
	for _, role := range st.EndpointRoles {
		n := counts[role.Name]
		if role.Min > 0 && n < role.Min {
			return statusErrf(400, "role %q requires at least %d endpoint(s)", role.Name, role.Min)
		}
		if role.Max > 0 && n > role.Max {
			return statusErrf(400, "role %q allows at most %d endpoint(s)", role.Name, role.Max)
		}
	}
	return nil
}

func ReplaceEndpoints(db *gorm.DB, serviceID uint, eps []models.ServiceEndpoint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("service_id = ?", serviceID).Delete(&models.ServiceEndpoint{}).Error; err != nil {
			return err
		}
		for i := range eps {
			eps[i].ID = 0
			eps[i].ServiceID = serviceID
			if err := tx.Create(&eps[i]).Error; err != nil {
				return fmt.Errorf("create endpoint: %w", err)
			}
		}
		return nil
	})
}

func ListEndpoints(db *gorm.DB, serviceID uint) ([]models.ServiceEndpoint, error) {
	var rows []models.ServiceEndpoint
	if err := db.Where("service_id = ?", serviceID).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// EncodeEndpointFields stores vlan plus optional NetBox ids on an endpoint.
func EncodeEndpointFields(vlan int, subNetboxID, termNetboxID uint) json.RawMessage {
	m := map[string]any{FieldVLAN: vlan}
	if subNetboxID != 0 {
		m[FieldSubinterfaceNetboxID] = subNetboxID
	}
	if termNetboxID != 0 {
		m[FieldTerminationNetboxID] = termNetboxID
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func FieldUint(m map[string]any, key string) uint {
	n, ok := asInt(m[key])
	if !ok || n < 0 {
		return 0
	}
	return uint(n)
}

func NetboxIDsFromFields(raw json.RawMessage) (sub, term uint) {
	m := fieldsMap(raw)
	return FieldUint(m, FieldSubinterfaceNetboxID), FieldUint(m, FieldTerminationNetboxID)
}

func VLANFromFields(raw json.RawMessage) int {
	return vlanFromFields(fieldsMap(raw))
}

func IsPhysicalInterfaceType(t string) bool {
	return t != "" && t != "virtual" && t != "lag"
}

// ValidateELINEShape enforces physical ports and that A/B are not the same
// device+interface. Call after ValidateEndpoints.
func ValidateELINEShape(db *gorm.DB, eps []models.ServiceEndpoint) error {
	type key struct {
		device, iface uint
	}
	seen := map[key]bool{}
	for i := range eps {
		ep := &eps[i]
		var iface models.Interface
		if err := db.First(&iface, ep.InterfaceID).Error; err != nil {
			return err
		}
		if !IsPhysicalInterfaceType(iface.Type) {
			return statusErrf(400, "endpoint role %q: interface %q is not a physical interface", ep.Role, iface.Name)
		}
		k := key{ep.DeviceID, ep.InterfaceID}
		if seen[k] {
			return statusErr(400, "endpoint A and endpoint B must not be the same device/interface")
		}
		seen[k] = true
	}
	return nil
}
