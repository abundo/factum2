package web

import (
	"errors"
	"net/http"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"

	"github.com/labstack/echo/v5"
)

// Fetch all contacts
func (ctrl *Controller) ApiContact(c *echo.Context) error {
	items, err := gorm.G[models.Contact](ctrl.DB).Order("name").Find(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"message": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiContactByID(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	item, err := gorm.G[models.Contact](ctrl.DB).Where("id = ?", id).First(c.Request().Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"message": "item not found"})
		}
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

// ApiContactUpdate rejects Lime-owned field edits on contacts synced from
// Lime (name/email/phone get overwritten by the next person-sync in
// internal/lime/lime.go). NotifyMaintenance is factum-owned, so a Lime
// row may still toggle that flag - same idea as ApiServiceTypeUpdate on a
// Lime-sourced service. Everything else delegates to the generic CRUD
// handler.
func (ctrl *Controller) ApiContactUpdate(contacts *SecureCRUDHandler[models.Contact, models.ContactDTO]) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := c.Param("id")
		var existing models.Contact
		if err := ctrl.DB.First(&existing, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		if existing.Source == "lime" {
			var dto models.ContactDTO
			if err := c.Bind(&dto); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			}
			existing.NotifyMaintenance = dto.NotifyMaintenance
			if err := ctrl.DB.Save(&existing).Error; err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
			}
			return c.JSON(http.StatusOK, existing)
		}
		return contacts.Update(c)
	}
}

// ApiContactDelete removes a locally-created contact. Lime-synced rows are
// rejected (they'd just reappear on the next person-sync). CustomerContact
// join rows are not independent records, so they're removed in the same
// transaction rather than treated as a blocker. MaintenanceNotification
// rows keep the sent email and have ContactID cleared so the send log is
// not deleted with the person.
func (ctrl *Controller) ApiContactDelete(c *echo.Context) error {
	id := c.Param("id")
	var existing models.Contact
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if existing.Source == "lime" {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "contacts synced from Lime cannot be deleted"})
	}

	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("contact_id = ?", existing.ID).Delete(&models.CustomerContact{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.MaintenanceNotification{}).
			Where("contact_id = ?", existing.ID).
			Updates(map[string]any{"contact_id": nil}).Error; err != nil {
			return err
		}
		return tx.Delete(&existing).Error
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
