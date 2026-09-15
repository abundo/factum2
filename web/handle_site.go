package web

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

type siteTreeNode struct {
	Key      string         `json:"key"`
	Title    string         `json:"title"`
	Type     string         `json:"type"`
	Data     siteTreeData   `json:"data"`
	Children []siteTreeNode `json:"children,omitempty"`
}

type siteTreeData struct {
	ID         uint    `json:"id"`
	ParentID   *uint   `json:"parent_id,omitempty"`
	Kind       string  `json:"kind"`
	Source     string  `json:"source"`
	NetboxKind string  `json:"netbox_kind,omitempty"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
}

func (ctrl *Controller) ApiSiteList(c *echo.Context) error {
	var items []models.Site
	if err := ctrl.DB.Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiSiteTree(c *echo.Context) error {
	var items []models.Site
	if err := ctrl.DB.Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, buildSiteTree(items))
}

func (ctrl *Controller) ApiSiteGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var item models.Site
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiSiteCreate(c *echo.Context) error {
	var dto models.SiteDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	dto.ParentID = normalizeSiteParent(dto.ParentID)
	if err := ctrl.validateSiteParent(0, dto.ParentID); err != nil {
		return httpErrorJSON(c, err)
	}
	row := models.Site{
		ParentID:  dto.ParentID,
		Name:      name,
		Latitude:  dto.Latitude,
		Longitude: dto.Longitude,
		Source:    models.SiteSourceFactum,
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiSiteUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var existing models.Site
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if !existing.IsLocal() {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "sites synced from NetBox cannot be edited here"})
	}
	var dto models.SiteDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	dto.ParentID = normalizeSiteParent(dto.ParentID)
	if err := ctrl.validateSiteParent(existing.ID, dto.ParentID); err != nil {
		return httpErrorJSON(c, err)
	}
	existing.Name = name
	existing.Slug = models.Slugify(name)
	existing.ParentID = dto.ParentID
	existing.Latitude = dto.Latitude
	existing.Longitude = dto.Longitude
	if err := ctrl.DB.Save(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, existing)
}

func (ctrl *Controller) ApiSiteDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var existing models.Site
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if !existing.IsLocal() {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "sites synced from NetBox cannot be deleted here"})
	}
	var childCount int64
	if err := ctrl.DB.Model(&models.Site{}).Where("parent_id = ?", existing.ID).Count(&childCount).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if childCount > 0 {
		return c.JSON(http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("site has %d child site(s) and cannot be deleted", childCount),
		})
	}
	if err := ctrl.DB.Delete(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func normalizeSiteParent(parentID *uint) *uint {
	if parentID == nil || *parentID == 0 {
		return nil
	}
	return parentID
}

func (ctrl *Controller) validateSiteParent(nodeID uint, parentID *uint) error {
	if parentID == nil || *parentID == 0 {
		return nil
	}
	if nodeID != 0 && *parentID == nodeID {
		return echo.NewHTTPError(http.StatusBadRequest, "site cannot be its own parent")
	}
	var parent models.Site
	if err := ctrl.DB.First(&parent, *parentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusBadRequest, "parent site not found")
		}
		return err
	}
	if nodeID == 0 {
		return nil
	}
	cycle, err := siteWouldCycle(ctrl.DB, nodeID, *parentID)
	if err != nil {
		return err
	}
	if cycle {
		return echo.NewHTTPError(http.StatusBadRequest, "parent would create a cycle")
	}
	return nil
}

func siteWouldCycle(db *gorm.DB, nodeID, newParentID uint) (bool, error) {
	seen := map[uint]bool{nodeID: true}
	cur := newParentID
	for cur != 0 {
		if seen[cur] {
			return true, nil
		}
		seen[cur] = true
		var row models.Site
		if err := db.Select("id", "parent_id").First(&row, cur).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		if row.ParentID == nil {
			return false, nil
		}
		cur = *row.ParentID
	}
	return false, nil
}

func buildSiteTree(rows []models.Site) []siteTreeNode {
	byParent := map[uint][]models.Site{}
	var roots []models.Site
	ids := map[uint]bool{}
	for _, row := range rows {
		ids[row.ID] = true
	}
	for _, row := range rows {
		if row.ParentID != nil && *row.ParentID != 0 && ids[*row.ParentID] {
			byParent[*row.ParentID] = append(byParent[*row.ParentID], row)
			continue
		}
		roots = append(roots, row)
	}
	sortSitesByName(roots)
	out := make([]siteTreeNode, 0, len(roots))
	for _, root := range roots {
		out = append(out, siteTreeNodeFrom(root, byParent))
	}
	return out
}

func siteTreeNodeFrom(row models.Site, byParent map[uint][]models.Site) siteTreeNode {
	children := byParent[row.ID]
	sortSitesByName(children)
	node := siteTreeNode{
		Key:   fmt.Sprintf("%d", row.ID),
		Title: row.Name,
		Type:  "site",
		Data: siteTreeData{
			ID:         row.ID,
			ParentID:   row.ParentID,
			Kind:       "site",
			Source:     row.Source,
			NetboxKind: row.NetboxKind,
			Latitude:   row.Latitude,
			Longitude:  row.Longitude,
		},
	}
	if len(children) > 0 {
		node.Children = make([]siteTreeNode, 0, len(children))
		for _, child := range children {
			node.Children = append(node.Children, siteTreeNodeFrom(child, byParent))
		}
	}
	return node
}

func sortSitesByName(rows []models.Site) {
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
}
