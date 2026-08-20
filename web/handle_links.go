package web

import (
	"net/http"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// ApiLinksList returns every dashboard link ordered by Position, then ID as
// a tiebreaker - shared by the admin CRUD page (GET /api/admin/links) and
// the dashboard itself (GET /api/links, any authenticated user: the list is
// meant to be seen by everyone, only edited by admins).
func (ctrl *Controller) ApiLinksList(c *echo.Context) error {
	var links []models.Link
	if err := ctrl.DB.Order("position, id").Find(&links).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, links)
}

// ApiLinkCreate replaces the generic handler's Create so a new link is
// always appended after the current highest Position instead of trusting a
// client-supplied one - drag-and-drop reordering (ApiLinksReorder) is the
// only path allowed to move an existing row's Position.
func (ctrl *Controller) ApiLinkCreate(c *echo.Context) error {
	var dto models.LinkDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	var maxPosition int
	if err := ctrl.DB.Model(&models.Link{}).Select("COALESCE(MAX(position), -1)").Scan(&maxPosition).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	link := models.Link{
		Group:        dto.Group,
		Name:         dto.Name,
		URL:          dto.URL,
		OpenInNewTab: dto.OpenInNewTab,
		Icon:         dto.Icon,
		Position:     maxPosition + 1,
	}
	if err := ctrl.DB.Create(&link).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, link)
}

// LinksReorderRequest is the drag-and-drop result from the admin page: every
// link ID in its new display order.
type LinksReorderRequest struct {
	IDs []uint `json:"ids"`
}

// ApiLinksReorder assigns each link's Position from its index in the
// dropped order.
func (ctrl *Controller) ApiLinksReorder(c *echo.Context) error {
	var req LinksReorderRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		for i, id := range req.IDs {
			if err := tx.Model(&models.Link{}).Where("id = ?", id).Update("position", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
