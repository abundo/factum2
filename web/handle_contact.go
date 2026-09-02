package web

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"

	"github.com/labstack/echo/v5"
)

// contactListItem is GET /contact's row shape: the contact plus the
// comma-separated names of customers (companies) it is linked to via
// CustomerContact. The list UI shows that as a Company column; the join
// is many-to-many so a contact can name more than one.
type contactListItem struct {
	models.Contact
	Company string `json:"company"`
}

// Fetch all contacts, with related company names attached for the list view.
func (ctrl *Controller) ApiContact(c *echo.Context) error {
	items, err := gorm.G[models.Contact](ctrl.DB).Order("name").Find(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"message": err.Error()})
	}
	names, err := ctrl.contactCompanyNames()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := make([]contactListItem, len(items))
	for i, item := range items {
		out[i] = contactListItem{Contact: item, Company: names[item.ID]}
	}
	return c.JSON(http.StatusOK, out)
}

// contactCompanyNames maps contact ID -> sorted, comma-separated customer
// names. Missing/deleted customers are skipped so a stale join row does
// not produce a blank name in the list.
func (ctrl *Controller) contactCompanyNames() (map[uint]string, error) {
	var links []models.CustomerContact
	if err := ctrl.DB.Find(&links).Error; err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return map[uint]string{}, nil
	}

	seen := make(map[uint]struct{}, len(links))
	ids := make([]uint, 0, len(links))
	for _, l := range links {
		if _, ok := seen[l.CustomerID]; ok {
			continue
		}
		seen[l.CustomerID] = struct{}{}
		ids = append(ids, l.CustomerID)
	}

	var customers []models.Customer
	if err := ctrl.DB.Where("id IN ?", ids).Find(&customers).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]string, len(customers))
	for _, cust := range customers {
		byID[cust.ID] = cust.Name
	}

	perContact := make(map[uint][]string, len(links))
	for _, l := range links {
		name := byID[l.CustomerID]
		if name == "" {
			continue
		}
		perContact[l.ContactID] = append(perContact[l.ContactID], name)
	}

	out := make(map[uint]string, len(perContact))
	for id, names := range perContact {
		sort.Strings(names)
		out[id] = strings.Join(names, ", ")
	}
	return out, nil
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
		id, err := echo.PathParam[uint](c, "id")
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
		}
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
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
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
