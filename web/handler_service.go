package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// columnFromFields prefers an explicit DTO column value, then the
// same-named key in Fields (the service type schema). Zero means unset.
func columnFromFields(dtoVal int, raw json.RawMessage, key string) int {
	if dtoVal != 0 {
		return dtoVal
	}
	return intField(raw, key)
}

// intField reads a JSON number stored under key in raw Fields.
func intField(raw json.RawMessage, key string) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// --------------------------------------------------------------------------
//
//	API
//
// --------------------------------------------------------------------------

type ServiceDTO struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	Customer string `json:"company"`

	ServiceID     string `json:"service_id"`
	Category      string `json:"category"`
	ServiceType   string `json:"service_type"`
	BandwidthMbps int    `json:"bandwidth_mbps"`

	Deliverypoint1  string `json:"deliverypoint1"`
	Deliverypoint2  string `json:"deliverypoint2"`
	Product         string `json:"product"`
	Service         string `json:"service"`
	AgreementStatus string `json:"agreement_status"`
	Source          string `json:"source"`
}

func (ctrl *Controller) APIServiceList(c *echo.Context) error {
	// data := ctrl.GetUser(c)
	ctx := c.Request().Context()

	// if customer_id is set, return services for one customer
	var customerID uint
	_ = echo.QueryParamsBinder(c).Uint("customer_id", &customerID).BindError()

	q := gorm.G[models.Service](ctrl.DB).Order("service_id")
	if customerID > 0 {
		q = q.Where("customer_id = ?", customerID)
	}
	services, err := q.Find(ctx)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	customerIDs := make([]uint, len(services))
	for i, service := range services {
		customerIDs[i] = service.CustomerID
	}
	customers, err := gorm.G[models.Customer](ctrl.DB).Where("id IN ?", customerIDs).Find(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	nameByCustomerID := make(map[uint]string, len(customers))
	for _, customer := range customers {
		nameByCustomerID[customer.ID] = customer.Name
	}

	servicesDTO := make([]*ServiceDTO, 0, len(services))
	for _, service := range services {
		servicesDTO = append(servicesDTO, &ServiceDTO{
			ID:              service.ID,
			Customer:        nameByCustomerID[service.CustomerID],
			ServiceID:       service.ServiceID,
			Category:        categoryFromServiceID(service.ServiceID),
			ServiceType:     service.ServiceType,
			BandwidthMbps:   service.BandwidthMbps,
			Deliverypoint1:  service.DeliveryPoint1,
			Deliverypoint2:  service.DeliveryPoint2,
			Product:         service.Product,
			Service:         service.Service,
			AgreementStatus: service.AgreementStatus,
			Source:          service.Source,
		})
	}
	return c.JSON(http.StatusOK, servicesDTO)
}

// ApiServiceUpdate rejects edits to services synced from Lime (the existing
// row's Source == "lime") before delegating to the generic CRUD handler for
// everything else. Lime-sourced fields get overwritten wholesale on the
// next sync run (SaveDelivery in internal/lime/lime.go), so letting an edit
// through the API would just have it silently discarded on the next sync -
// better to reject it upfront than have an edit mysteriously disappear.
func (ctrl *Controller) ApiServiceUpdate(services *SecureCRUDHandler[models.Service, models.ServiceDTO]) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := echo.PathParam[uint](c, "id")
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
		}
		var existing models.Service
		if err := ctrl.DB.First(&existing, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		if existing.Source == "lime" {
			return c.JSON(http.StatusForbidden, map[string]any{"error": "services synced from Lime cannot be edited"})
		}
		return services.Update(c)
	}
}

// ServiceTypeDTO is the request shape for ApiServiceTypeUpdate - the one
// slice of a service's data that's editable regardless of Source, since
// SaveDelivery (internal/lime/lime.go) now preserves these specific fields
// across a Lime resync instead of overwriting them.
type ServiceTypeDTO struct {
	ServiceType     string          `json:"service_type"`
	BandwidthMbps   int             `json:"bandwidth_mbps"`
	MaxMacAddresses int             `json:"max_mac_addresses"`
	Fields          json.RawMessage `json:"fields"`
}

// ApiServiceTypeUpdate lets the network GUI attach a service type,
// bandwidth and max MAC address count to a service - including one synced
// from Lime, which is otherwise read-only (ApiServiceUpdate) since Lime
// doesn't supply these fields itself and SaveDelivery now preserves
// whatever's set here across future syncs. Deliberately not routed through
// the generic SecureCRUDHandler/ServiceDTO update: that DTO also carries
// Lime-owned fields (company, delivery points, product, service, comment,
// service_id, agreement_status), which must stay off-limits for a Lime-sourced row.
func (ctrl *Controller) ApiServiceTypeUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	var existing models.Service
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	var dto ServiceTypeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	fields := dto.Fields
	if dto.ServiceType != "" {
		st, err := cfgmgmt.LookupServiceType(ctrl.DB, dto.ServiceType)
		if err != nil {
			if se := cfgmgmt.AsStatusError(err); se != nil {
				return c.JSON(se.Status, map[string]any{"error": se.Message})
			}
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		canon, err := cfgmgmt.ValidateServiceFields(st, dto.Fields)
		if err != nil {
			if se := cfgmgmt.AsStatusError(err); se != nil {
				return c.JSON(se.Status, map[string]any{"error": se.Message})
			}
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
		fields = canon
	}

	updates := map[string]any{
		"service_type":      dto.ServiceType,
		"bandwidth_mbps":    columnFromFields(dto.BandwidthMbps, fields, models.SchemaFieldBandwidthMbps),
		"max_mac_addresses": columnFromFields(dto.MaxMacAddresses, fields, models.SchemaFieldMaxMacAddresses),
	}
	if len(fields) > 0 && string(fields) != "null" {
		updates["fields"] = fields
	}
	if err := ctrl.DB.Model(&models.Service{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	var updated models.Service
	if err := ctrl.DB.First(&updated, id).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, updated)
}

// ServiceDetailResponse adds AppliedToDevice to the raw Service model - a
// derived signal for whether the service has live config on a device
// (service_endpoints.applied_device_id). Used by the delete dialog
// (ServiceList.vue) to decide whether to offer a "remove from device"
// cleanup option.
type ServiceDetailResponse struct {
	models.Service
	AppliedToDevice bool                     `json:"applied_to_device"`
	Endpoints       []models.ServiceEndpoint `json:"endpoints,omitempty"`
}

func endpointsAppliedToDevice(eps []models.ServiceEndpoint) bool {
	for _, ep := range eps {
		if ep.AppliedDeviceID != 0 {
			return true
		}
	}
	return false
}

func (ctrl *Controller) ApiServiceByID(c *echo.Context) error {
	// data := ctrl.GetUser(c)
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	item, err := gorm.G[models.Service](ctrl.DB).Where("id = ?", id).First(c.Request().Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"message": "item not found"})
		}
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	eps, _ := cfgmgmt.ListEndpoints(ctrl.DB, item.ID)
	return c.JSON(http.StatusOK, ServiceDetailResponse{
		Service:         item,
		AppliedToDevice: endpointsAppliedToDevice(eps),
		Endpoints:       eps,
	})
}

// validCategories are the service-ID prefixes: CN/CI for
// ELINE/ELAN/L3VPN/POLARIX capacity services (external/internal), VL/VI for
// wavelengths, LF/LI for dark fiber.
var validCategories = map[string]bool{
	"CN": true, "CI": true,
	"VL": true, "VI": true,
	"LF": true, "LI": true,
}

// capacityCategories are the prefixes that carry a ServiceType
// (ELINE/ELAN/L3VPN/POLARIX) - wavelength (VL/VI) and fiber (LF/LI) rows
// have no service type, since the prefix alone fully describes them.
var capacityCategories = map[string]bool{"CN": true, "CI": true}

// categoryFromServiceID derives a service's category from its ServiceID's
// <category><5-digit> prefix rather than storing it as a separate column -
// older/Lime-sourced rows with free-text service IDs that don't match the
// shape simply have no derivable category ("").
func categoryFromServiceID(serviceID string) string {
	return models.CategoryFromServiceID(serviceID)
}

// ApiServiceCreate creates a service in a single step - the create wizard
// no longer reserves a service ID before the rest of the form is filled in.
// If the caller leaves ServiceID blank, the next available
// <category><5-digit> number for Category is auto-assigned (derived from
// existing rows, max + 1); otherwise the caller-supplied ID is used as-is
// after checking it isn't already taken.
func (ctrl *Controller) ApiServiceCreate(c *echo.Context) error {
	var dto models.ServiceDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if !validCategories[dto.Category] {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid category"})
	}
	if capacityCategories[dto.Category] {
		ok, err := cfgmgmt.ServiceTypeExists(ctrl.DB, dto.ServiceType)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if !ok {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid service_type"})
		}
	}

	var created *models.Service
	err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		row, err := cfgmgmt.CreateServiceRecord(tx, &dto)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, created)
}

// ServiceDeleteRequest is the optional body for ApiServiceDelete and
// ApiServiceUnrealize. Username/Password are only needed when
// RemoveFromDevice is set (same per-request, never-persisted credentials
// as deviceCredentialsRequest).
type ServiceDeleteRequest struct {
	RemoveFromNetbox bool   `json:"remove_from_netbox"`
	RemoveFromDevice bool   `json:"remove_from_device"`
	Username         string `json:"username"`
	Password         string `json:"password"`
}

func (ctrl *Controller) serviceCleanup(c *echo.Context, existing *models.Service, req ServiceDeleteRequest) (map[string]any, error) {
	response := map[string]any{}
	if req.RemoveFromDevice && existing.ServiceType != "" {
		results, err := ctrl.removeServiceFromDevices(c, existing, req.Username, req.Password)
		if err != nil {
			return nil, &elineHTTPError{http.StatusBadRequest, err.Error()}
		}
		for _, r := range results {
			if r.Error != "" {
				return nil, &elineHTTPError{http.StatusBadGateway, fmt.Sprintf("failed to remove config from device %s: %s", r.Device, r.Error)}
			}
		}
		response["device_results"] = results
	}
	if req.RemoveFromNetbox {
		st, _ := cfgmgmt.LookupServiceType(ctrl.DB, existing.ServiceType)
		hasIDs := existing.L2VPNNetboxID != 0
		if !hasIDs {
			eps, _ := cfgmgmt.ListEndpoints(ctrl.DB, existing.ID)
			for _, ep := range eps {
				sub, term := cfgmgmt.NetboxIDsFromFields(ep.Fields)
				if sub != 0 || term != 0 {
					hasIDs = true
					break
				}
			}
		}
		if hasIDs && (serviceHasNetboxMapping(st) || existing.L2VPNNetboxID != 0) {
			if err := ctrl.removeServiceFromNetbox(existing); err != nil {
				return nil, &elineHTTPError{http.StatusBadGateway, "failed to remove from netbox: " + err.Error()}
			}
			response["netbox_removed"] = true
		}
	}
	return response, nil
}

func serviceCleanupError(c *echo.Context, err error) error {
	var he *elineHTTPError
	if errors.As(err, &he) {
		return c.JSON(he.status, map[string]any{"error": he.msg})
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

// ApiServiceDelete refuses Lime-sourced rows (use unrealize to drop
// realization). Optional RemoveFromDevice / RemoveFromNetbox apply to any
// definition. Cleanup failure aborts the delete.
func (ctrl *Controller) ApiServiceDelete(services *SecureCRUDHandler[models.Service, models.ServiceDTO]) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := echo.PathParam[uint](c, "id")
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
		}
		var existing models.Service
		if err := ctrl.DB.First(&existing, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		if existing.Source == "lime" {
			return c.JSON(http.StatusForbidden, map[string]any{"error": "services synced from Lime cannot be deleted"})
		}

		var req ServiceDeleteRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}

		response, err := ctrl.serviceCleanup(c, &existing, req)
		if err != nil {
			return serviceCleanupError(c, err)
		}
		cleanupRequested := req.RemoveFromNetbox || req.RemoveFromDevice

		if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
			if err := cfgmgmt.DetachServiceByRowID(tx, existing.ID); err != nil {
				return err
			}
			if err := optical.DeletePathForService(tx, existing.ID); err != nil {
				return err
			}
			if err := tx.Where("service_id = ?", existing.ID).Delete(&models.ServiceEndpoint{}).Error; err != nil {
				return err
			}
			return tx.Delete(&existing).Error
		}); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}

		if !cleanupRequested {
			return c.NoContent(http.StatusNoContent)
		}
		return c.JSON(http.StatusOK, response)
	}
}

// ApiServiceUnrealize drops realization (type, endpoints, tree node, cfgmgmt
// fields) but keeps the commercial row. Allowed on Lime. Honors the same
// cleanup flags as delete.
func (ctrl *Controller) ApiServiceUnrealize(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var existing models.Service
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	var req ServiceDeleteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	response, err := ctrl.serviceCleanup(c, &existing, req)
	if err != nil {
		return serviceCleanupError(c, err)
	}

	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := cfgmgmt.DetachServiceByRowID(tx, existing.ID); err != nil {
			return err
		}
		if err := tx.Where("service_id = ?", existing.ID).Delete(&models.ServiceEndpoint{}).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"service_type":       "",
			"fields":             json.RawMessage(`{}`),
			"connection_type_id": nil,
			"pseudowire_id":      0,
			"l2_vpn_netbox_id":   0,
		}
		return tx.Model(&models.Service{}).Where("id = ?", existing.ID).Updates(updates).Error
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	var updated models.Service
	if err := ctrl.DB.First(&updated, existing.ID).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	response["service"] = updated
	return c.JSON(http.StatusOK, response)
}
