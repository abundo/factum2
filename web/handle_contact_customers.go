package web

import (
	"net/http"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func (ctrl *Controller) ApiContactCustomers(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var links []models.CustomerContact
	if err := ctrl.DB.Where("contact_id = ?", id).Find(&links).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	ids := make([]uint, len(links))
	for i, l := range links {
		ids[i] = l.CustomerID
	}
	var customers []models.Customer
	if len(ids) > 0 {
		ctrl.DB.Where("id IN ?", ids).Find(&customers)
	}
	return c.JSON(http.StatusOK, customers)
}

func (ctrl *Controller) ApiContactCustomersPut(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var existing models.Contact
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if existing.Source == "lime" {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "contacts synced from Lime cannot be edited"})
	}
	var body struct {
		CustomerIDs []uint `json:"customer_ids"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Where("contact_id = ?", id).Delete(&models.CustomerContact{}).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	for _, cid := range body.CustomerIDs {
		if err := ctrl.DB.Create(&models.CustomerContact{ContactID: id, CustomerID: cid}).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}
	return ctrl.ApiContactCustomers(c)
}

func (ctrl *Controller) ApiCustomerContacts(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var links []models.CustomerContact
	if err := ctrl.DB.Where("customer_id = ?", id).Find(&links).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	ids := make([]uint, len(links))
	for i, l := range links {
		ids[i] = l.ContactID
	}
	var contacts []models.Contact
	if len(ids) > 0 {
		ctrl.DB.Where("id IN ?", ids).Find(&contacts)
	}
	return c.JSON(http.StatusOK, contacts)
}
