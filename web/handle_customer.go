package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"

	"github.com/labstack/echo/v5"
)

// --------------------------------------------------------------------------
//
//	API
//
// --------------------------------------------------------------------------

// customerListItem is GET /customer's row shape: the customer plus how many
// services point at it. The list UI uses this to hide the Services button
// for customers that have none.
type customerListItem struct {
	models.Customer
	ServiceCount int64 `json:"service_count"`
}

// Fetch all companies, with a per-customer service count attached for the
// list view.
func (ctrl *Controller) ApiCustomer(c *echo.Context) error {
	items, err := gorm.G[models.Customer](ctrl.DB).Order("name").Find(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"message": err.Error()})
	}
	counts, err := ctrl.customerServiceCounts()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := make([]customerListItem, len(items))
	for i, item := range items {
		out[i] = customerListItem{Customer: item, ServiceCount: counts[item.ID]}
	}
	return c.JSON(http.StatusOK, out)
}

// customerServiceCounts maps customer ID -> number of services that
// reference it. Customers with no services are omitted (callers treat a
// missing key as 0).
func (ctrl *Controller) customerServiceCounts() (map[uint]int64, error) {
	var rows []struct {
		CustomerID uint  `gorm:"column:customer_id"`
		Count      int64 `gorm:"column:count"`
	}
	if err := ctrl.DB.Model(&models.Service{}).
		Select("customer_id, count(*) as count").
		Group("customer_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]int64, len(rows))
	for _, row := range rows {
		out[row.CustomerID] = row.Count
	}
	return out, nil
}

func (ctrl *Controller) ApiCustomerByID(c *echo.Context) error {
	// data := ctrl.GetUser(c)
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	item, err := gorm.G[models.Customer](ctrl.DB).Where("id = ?", id).First(c.Request().Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"message": "item not found"})
		}
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

// ApiCustomerUpdate rejects edits to customers synced from Lime (the existing
// row's Source == "lime") before delegating to the generic CRUD handler for
// everything else. Lime-sourced fields get overwritten wholesale on the
// next sync run (SaveCustomer in internal/lime/lime.go), so letting an edit
// through the API would just have it silently discarded on the next sync -
// better to reject it upfront than have an edit mysteriously disappear.
func (ctrl *Controller) ApiCustomerUpdate(customers *SecureCRUDHandler[models.Customer, models.CustomerDTO]) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := c.Param("id")
		var existing models.Customer
		if err := ctrl.DB.First(&existing, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		if existing.Source == "lime" {
			return c.JSON(http.StatusForbidden, map[string]any{"error": "customers synced from Lime cannot be edited"})
		}
		return customers.Update(c)
	}
}

// ApiCustomerDelete removes a locally-created customer. Lime-synced rows are
// rejected the same way as updates (they'd just reappear on the next Lime
// sync). A customer that still has services is also rejected: those rows
// would be left pointing at a missing company. CustomerContact join rows
// are not independent records, so they're removed in the same transaction
// rather than treated as a blocker.
func (ctrl *Controller) ApiCustomerDelete(c *echo.Context) error {
	id := c.Param("id")
	var existing models.Customer
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if existing.Source == "lime" {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "customers synced from Lime cannot be deleted"})
	}

	var serviceCount int64
	if err := ctrl.DB.Model(&models.Service{}).Where("customer_id = ?", existing.ID).Count(&serviceCount).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if serviceCount > 0 {
		return c.JSON(http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("customer has %d service(s) and cannot be deleted", serviceCount),
		})
	}

	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("customer_id = ?", existing.ID).Delete(&models.CustomerContact{}).Error; err != nil {
			return err
		}
		return tx.Delete(&existing).Error
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
