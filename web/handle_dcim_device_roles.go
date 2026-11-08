package web

import (
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

func (ctrl *Controller) ApiGetDeviceRoles(c *echo.Context) error {
	var items []models.DeviceRole
	if err := ctrl.DB.Order("name, id").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if items == nil {
		items = []models.DeviceRole{}
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiGetDeviceRole(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var item models.DeviceRole
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiCreateDeviceRole(c *echo.Context) error {
	var dto models.DeviceRoleDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	source := "factum"
	if dto.Netbox {
		source = "netbox"
	}
	row, err := deviceRoleFromDTO(dto, source)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := deviceRoleSlugFree(ctrl.DB, row.Slug, row.Source, 0); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.Netbox {
		nb, err := ctrl.requireDeviceRoleNetbox()
		if err != nil {
			return httpErrorJSON(c, err)
		}
		id, err := nb.CreateDeviceRole(row.Name, row.Slug, row.Color, row.Description, row.VMRole)
		if err != nil {
			return netboxUpstream(c, err)
		}
		row.Source = "netbox"
		row.NetboxID = id
		if row.Color == "" {
			row.Color = "9e9e9e"
		}
		if err := ctrl.DB.Create(&row).Error; err != nil {
			if delErr := nb.DeleteDeviceRole(id); delErr != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
			}
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		_ = linkLocalDevicesToRole(ctrl.DB, row.Name, row.NetboxID)
		return c.JSON(http.StatusCreated, row)
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiUpdateDeviceRole(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var row models.DeviceRole
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.DeviceRoleDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	next, err := deviceRoleFromDTO(dto, row.Source)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	writeNetbox := row.Source == "netbox" || row.NetboxID != 0 || dto.Netbox
	if writeNetbox {
		next.Source = "netbox"
	} else {
		next.Source = "factum"
	}
	if err := deviceRoleSlugFree(ctrl.DB, next.Slug, next.Source, row.ID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	oldName := row.Name
	if writeNetbox {
		nb, err := ctrl.requireDeviceRoleNetbox()
		if err != nil {
			return httpErrorJSON(c, err)
		}
		color := next.Color
		if color == "" {
			color = "9e9e9e"
		}
		changes := map[string]any{
			"name":        next.Name,
			"slug":        next.Slug,
			"color":       color,
			"description": next.Description,
			"vm_role":     next.VMRole,
		}
		if row.NetboxID == 0 {
			nid, err := nb.CreateDeviceRole(next.Name, next.Slug, color, next.Description, next.VMRole)
			if err != nil {
				return netboxUpstream(c, err)
			}
			row.NetboxID = nid
			next.Color = color
		} else if err := nb.UpdateDeviceRole(row.NetboxID, changes); err != nil {
			return netboxUpstream(c, err)
		}
		next.NetboxID = row.NetboxID
		next.Source = "netbox"
		if next.Color == "" {
			next.Color = color
		}
	}
	row.Name = next.Name
	row.Slug = next.Slug
	row.Color = next.Color
	row.Description = next.Description
	row.VMRole = next.VMRole
	row.Source = next.Source
	row.NetboxID = next.NetboxID
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if row.NetboxID != 0 {
		if err := ctrl.DB.Model(&models.Device{}).
			Where("role_id = ? AND role <> ?", row.NetboxID, row.Name).
			Update("role", row.Name).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if err := linkLocalDevicesToRole(ctrl.DB, row.Name, row.NetboxID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	} else if oldName != row.Name {
		if err := ctrl.DB.Model(&models.Device{}).
			Where("role_id = 0 AND role = ?", oldName).
			Update("role", row.Name).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDeleteDeviceRole(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var row models.DeviceRole
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	inUse, err := deviceRoleInUse(ctrl.DB, row)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if inUse {
		return c.JSON(http.StatusConflict, map[string]any{"error": "device role is assigned to a device"})
	}
	if row.NetboxID != 0 {
		nb, err := ctrl.requireDeviceRoleNetbox()
		if err != nil {
			return httpErrorJSON(c, err)
		}
		if err := nb.DeleteDeviceRole(row.NetboxID); err != nil {
			return netboxUpstream(c, err)
		}
	}
	if err := ctrl.DB.Delete(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) requireDeviceRoleNetbox() (*netboxtool.NetboxClient, error) {
	nb, err := ctrl.netboxClientIfConfigured()
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if nb == nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "NetBox is not configured; NetBox device roles cannot be changed here")
	}
	return nb, nil
}

func deviceRoleFromDTO(dto models.DeviceRoleDTO, source string) (models.DeviceRole, error) {
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return models.DeviceRole{}, errors.New("name is required")
	}
	slug := strings.TrimSpace(dto.Slug)
	if slug == "" {
		slug = models.Slugify(name)
	}
	if slug == "" {
		return models.DeviceRole{}, errors.New("slug is required")
	}
	color, err := normalizeDeviceRoleColor(dto.Color)
	if err != nil {
		return models.DeviceRole{}, err
	}
	if source == "" {
		source = "factum"
	}
	return models.DeviceRole{
		Name:        name,
		Slug:        slug,
		Color:       color,
		Description: strings.TrimSpace(dto.Description),
		VMRole:      dto.VMRole,
		Source:      source,
	}, nil
}

func normalizeDeviceRoleColor(s string) (string, error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if s == "" {
		return "", nil
	}
	if len(s) != 6 {
		return "", errors.New("color must be 6 hex digits")
	}
	for _, r := range s {
		if !unicode.Is(unicode.ASCII_Hex_Digit, r) {
			return "", errors.New("color must be 6 hex digits")
		}
	}
	return strings.ToLower(s), nil
}

func deviceRoleSlugFree(db *gorm.DB, slug, source string, exceptID uint) error {
	if source == "" {
		source = "factum"
	}
	q := db.Model(&models.DeviceRole{}).Where("slug = ? AND source = ?", slug, source)
	if exceptID != 0 {
		q = q.Where("id <> ?", exceptID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errors.New("slug is already used")
	}
	return nil
}

func deviceRoleInUse(db *gorm.DB, row models.DeviceRole) (bool, error) {
	var n int64
	q := db.Model(&models.Device{})
	if row.NetboxID != 0 {
		q = q.Where("role_id = ?", row.NetboxID)
	} else {
		q = q.Where("role_id = 0 AND role = ?", row.Name)
	}
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func linkLocalDevicesToRole(db *gorm.DB, name string, netboxID uint) error {
	if netboxID == 0 || name == "" {
		return nil
	}
	return db.Model(&models.Device{}).
		Where("role_id = 0 AND role = ?", name).
		Update("role_id", netboxID).Error
}
