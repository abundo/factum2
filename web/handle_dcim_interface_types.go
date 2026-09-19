package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

func (ctrl *Controller) ApiGetInterfaceTypes(c *echo.Context) error {
	var items []models.InterfaceType
	if err := ctrl.DB.Order("sort_order, label, value").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if items == nil {
		items = []models.InterfaceType{}
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiGetInterfaceType(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var item models.InterfaceType
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiCreateInterfaceType(c *echo.Context) error {
	var dto models.InterfaceTypeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row := models.InterfaceType{
		Value:     strings.TrimSpace(dto.Value),
		Label:     strings.TrimSpace(dto.Label),
		SortOrder: dto.SortOrder,
		Source:    "factum",
	}
	if err := validateInterfaceType(ctrl.DB, &row, 0); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if row.SortOrder == 0 {
		row.SortOrder = nextInterfaceTypeSortOrder(ctrl.DB)
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiUpdateInterfaceType(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var row models.InterfaceType
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.InterfaceTypeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	oldValue := row.Value
	row.Value = strings.TrimSpace(dto.Value)
	row.Label = strings.TrimSpace(dto.Label)
	if err := validateInterfaceType(ctrl.DB, &row, row.ID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if oldValue != row.Value {
			if err := tx.Model(&models.Interface{}).Where("type = ?", oldValue).Update("type", row.Value).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.InterfaceTemplate{}).Where("type = ?", oldValue).Update("type", row.Value).Error; err != nil {
				return err
			}
		}
		return tx.Save(&row).Error
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDeleteInterfaceType(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var row models.InterfaceType
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	inUse, err := interfaceTypeInUse(ctrl.DB, row.Value)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if inUse {
		return c.JSON(http.StatusConflict, map[string]any{"error": "interface type is in use"})
	}
	if err := ctrl.DB.Delete(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func validateInterfaceType(db *gorm.DB, row *models.InterfaceType, exceptID uint) error {
	if row.Value == "" {
		return errors.New("value is required")
	}
	if row.Label == "" {
		row.Label = row.Value
	}
	q := db.Model(&models.InterfaceType{}).Where("value = ?", row.Value)
	if exceptID != 0 {
		q = q.Where("id <> ?", exceptID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errors.New("value already exists")
	}
	return nil
}

func nextInterfaceTypeSortOrder(db *gorm.DB) int {
	var max int
	_ = db.Model(&models.InterfaceType{}).Select("COALESCE(MAX(sort_order), -1)").Scan(&max)
	return max + 1
}

func interfaceTypeInUse(db *gorm.DB, value string) (bool, error) {
	var n int64
	if err := db.Model(&models.Interface{}).Where("type = ?", value).Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := db.Model(&models.InterfaceTemplate{}).Where("type = ?", value).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
