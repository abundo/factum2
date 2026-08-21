package web

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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
		id := c.Param("id")
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
	ServiceType     string `json:"service_type"`
	BandwidthMbps   int    `json:"bandwidth_mbps"`
	MaxMacAddresses int    `json:"max_mac_addresses"`
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
	if dto.ServiceType != "" && !validServiceTypes[dto.ServiceType] {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid service_type"})
	}

	updates := map[string]any{
		"service_type":      dto.ServiceType,
		"bandwidth_mbps":    dto.BandwidthMbps,
		"max_mac_addresses": dto.MaxMacAddresses,
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
// (models.Service's AppliedEndpointX* fields, which record that, are
// json:"-" since they're internal bookkeeping, not something a client
// should ever set directly). Used by the delete dialog
// (ServiceList.vue) to decide whether to offer a "remove from device"
// cleanup option.
type ServiceDetailResponse struct {
	models.Service
	AppliedToDevice bool `json:"applied_to_device"`
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
	return c.JSON(http.StatusOK, ServiceDetailResponse{
		Service:         item,
		AppliedToDevice: item.AppliedEndpointADeviceID != 0 || item.AppliedEndpointBDeviceID != 0,
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

var validServiceTypes = map[string]bool{"ELINE": true, "ELAN": true, "L3VPN": true, "POLARIX": true}

// serviceIDRe matches the <category><5 digits> shape ApiServiceCreate
// auto-assigns - used to pick out which existing ServiceID values count
// towards the next number for a category, since older/Lime-sourced rows
// carry arbitrary free-text service IDs that must not be mistaken for one
// of ours just because they happen to start with the same prefix.
var serviceIDRe = regexp.MustCompile(`^([A-Z]{2})(\d{5})$`)

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
	if capacityCategories[dto.Category] && !validServiceTypes[dto.ServiceType] {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid service_type"})
	}

	var created models.Service
	err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		serviceID := strings.TrimSpace(dto.ServiceID)
		if serviceID == "" {
			// Locking existing rows for this category serializes concurrent
			// auto-assignments against each other; the only unguarded edge
			// is two concurrent first-ever creations for a brand-new
			// category (nothing to lock yet) both computing "1" - accepted
			// as practically irrelevant for a low-traffic internal admin
			// tool.
			var existing []models.Service
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("service_id LIKE ?", dto.Category+"%").
				Find(&existing).Error; err != nil {
				return err
			}

			next := 1
			for _, svc := range existing {
				m := serviceIDRe.FindStringSubmatch(svc.ServiceID)
				if m == nil || m[1] != dto.Category {
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
			serviceID = fmt.Sprintf("%s%05d", dto.Category, next)
		} else {
			var count int64
			if err := tx.Model(&models.Service{}).
				Where("service_id = ?", serviceID).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("service ID %q is already in use", serviceID)
			}
		}

		created = models.Service{
			CustomerID:      dto.CustomerID,
			Comment:         dto.Comment,
			ServiceID:       serviceID,
			ServiceType:     dto.ServiceType,
			BandwidthMbps:   dto.BandwidthMbps,
			MaxMacAddresses: dto.MaxMacAddresses,
			DeliveryPoint1:  dto.DeliveryPoint1,
			DeliveryPoint2:  dto.DeliveryPoint2,
			Product:         dto.Product,
			Service:         dto.Service,
		}
		return tx.Create(&created).Error
	})
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, created)
}

// ServiceDeleteRequest is ApiServiceDelete's optional request body - for an
// ELINE service, lets the caller ask for the NetBox objects and/or device
// config a prior eline/update and eline/push left behind to be torn down
// as part of the delete, rather than orphaned. Ignored entirely for
// non-ELINE services. Username/Password are only needed when
// RemoveFromDevice is set (same per-request, never-persisted credentials
// convention as deviceCredentialsRequest).
type ServiceDeleteRequest struct {
	RemoveFromNetbox bool   `json:"remove_from_netbox"`
	RemoveFromDevice bool   `json:"remove_from_device"`
	Username         string `json:"username"`
	Password         string `json:"password"`
}

// ApiServiceDelete mirrors ApiServiceUpdate's Lime-source guard, then -
// unlike a plain generic-CRUD delete - optionally tears down an ELINE
// service's NetBox/device state first (removeELINEServiceFromDevices/
// removeELINEServiceFromNetbox, web/handler_service_eline.go) before
// hard-deleting the row. Any requested cleanup step failing aborts the
// whole delete (row is left in place) rather than leaving a deleted local
// record with orphaned NetBox objects or live device config and no way to
// find it again - the operator can retry, or uncheck that option and
// clean up by hand. Used both for an explicit admin delete and for
// cleaning up an aborted create-wizard draft (which never sets the
// cleanup flags, so behaves exactly as before).
func (ctrl *Controller) ApiServiceDelete(services *SecureCRUDHandler[models.Service, models.ServiceDTO]) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := c.Param("id")
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

		response := map[string]any{}
		cleanupRequested := existing.ServiceType == "ELINE" && (req.RemoveFromNetbox || req.RemoveFromDevice)

		if req.RemoveFromDevice && existing.ServiceType == "ELINE" {
			results, err := ctrl.removeELINEServiceFromDevices(c, &existing, req.Username, req.Password)
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			}
			for _, r := range results {
				if r.Error != "" {
					return c.JSON(http.StatusBadGateway, map[string]any{
						"error": fmt.Sprintf("failed to remove ELINE config from device %s: %s", r.Device, r.Error),
					})
				}
			}
			response["device_results"] = results
		}

		if req.RemoveFromNetbox && existing.ServiceType == "ELINE" && existing.L2VPNNetboxID != 0 {
			if err := ctrl.removeELINEServiceFromNetbox(&existing); err != nil {
				return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to remove ELINE from netbox: " + err.Error()})
			}
			response["netbox_removed"] = true
		}

		if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
			if err := optical.DeletePathForService(tx, existing.ID); err != nil {
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
