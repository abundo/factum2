package web

import (
	"errors"
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

// Fetch all companies
func (ctrl *Controller) ApiCustomer(c *echo.Context) error {
	// data := ctrl.GetUser(c)
	items, err := gorm.G[models.Customer](ctrl.DB).Order("name").Find(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"message": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
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
