package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/dcim"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

func dcimError(c *echo.Context, err error) error {
	var e *dcim.Error
	if errors.As(err, &e) {
		body := map[string]any{"error": e.Message}
		if e.Reason != "" {
			body["reason"] = e.Reason
		}
		return c.JSON(e.Status, body)
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func parseUintQuery(c *echo.Context, name string) uint {
	v := strings.TrimSpace(c.QueryParam(name))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}

func (ctrl *Controller) ApiDCIMRacksList(c *echo.Context) error {
	rows, err := dcim.ListRacks(ctrl.DB, parseUintQuery(c, "site_id"))
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiDCIMRackCreate(c *echo.Context) error {
	var w dcim.RackWrite
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := dcim.CreateRack(ctrl.DB, w)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiDCIMRackUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var w dcim.RackWrite
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := dcim.UpdateRack(ctrl.DB, id, w)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMRackDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if err := dcim.DeleteRack(ctrl.DB, id); err != nil {
		return dcimError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiDCIMRackElevation(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	row, err := dcim.Elevation(ctrl.DB, id)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

type placementWrite struct {
	RackID      uint   `json:"rack_id"`
	OffsetTicks int    `json:"offset_ticks"`
	Face        string `json:"face"`
	Version     int    `json:"version"`
}

func (ctrl *Controller) ApiDCIMDevicePlace(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var w placementWrite
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := dcim.Place(ctrl.DB, dcim.PlaceRequest{
		DeviceID: id, RackID: w.RackID, OffsetTicks: w.OffsetTicks, Face: w.Face, Version: w.Version,
	})
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMDeviceUnmount(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	version := 0
	if v := strings.TrimSpace(c.QueryParam("version")); v != "" {
		n, _ := strconv.Atoi(v)
		version = n
	}
	if err := dcim.Unmount(ctrl.DB, id, version); err != nil {
		return dcimError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiDCIMFloorPlanList(c *echo.Context) error {
	rows, err := dcim.ListFloorPlans(ctrl.DB, parseUintQuery(c, "site_id"))
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

type floorPlanCreate struct {
	SiteID   uint   `json:"site_id"`
	Name     string `json:"name"`
	WidthMM  int    `json:"width_mm"`
	HeightMM int    `json:"height_mm"`
	GridMM   int    `json:"grid_mm"`
}

func (ctrl *Controller) ApiDCIMFloorPlanCreate(c *echo.Context) error {
	var w floorPlanCreate
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := dcim.CreateFloorPlan(ctrl.DB, w.SiteID, w.Name, w.WidthMM, w.HeightMM, w.GridMM)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiDCIMFloorPlanGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	row, err := dcim.GetFloorPlan(ctrl.DB, id)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMFloorPlanLayout(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var w dcim.FloorLayoutWrite
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := dcim.SaveFloorLayout(ctrl.DB, id, w)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMConnectionGraph(c *echo.Context) error {
	q := dcim.GraphQuery{
		SiteID:   parseUintQuery(c, "site_id"),
		RackID:   parseUintQuery(c, "rack_id"),
		DeviceID: parseUintQuery(c, "device_id"),
		Depth:    1,
	}
	if v := strings.TrimSpace(c.QueryParam("depth")); v != "" {
		n, _ := strconv.Atoi(v)
		q.Depth = n
	}
	row, err := dcim.ConnectionGraph(ctrl.DB, q)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMConnectionLayoutGet(c *echo.Context) error {
	user, ok := c.Get("user").(models.User)
	if !ok || user.ID == 0 {
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
	}
	scope := strings.TrimSpace(c.Param("scope"))
	row, err := dcim.GetLayout(ctrl.DB, user.ID, scope)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMConnectionLayoutPut(c *echo.Context) error {
	user, ok := c.Get("user").(models.User)
	if !ok || user.ID == 0 {
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
	}
	scope := strings.TrimSpace(c.Param("scope"))
	var w dcim.LayoutDTO
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := dcim.SaveLayout(ctrl.DB, user.ID, scope, w.Revision, w.Nodes)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDeviceTypeUpdateChecked(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.DeviceTypeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := dcim.ValidateDeviceTypeResize(ctrl.DB, id, dto.HeightTicks, dto.FullDepth); err != nil {
		return dcimError(c, err)
	}
	var row models.DeviceType
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if dto.ManufacturerID != 0 {
		row.ManufacturerID = dto.ManufacturerID
	}
	if strings.TrimSpace(dto.Model) != "" {
		row.Model = strings.TrimSpace(dto.Model)
	}
	row.Slug = dto.Slug
	row.PlatformID = dto.PlatformID
	row.HeightTicks = dto.HeightTicks
	row.FullDepth = dto.FullDepth
	if dto.FrontImage != "" {
		row.FrontImage = dto.FrontImage
	}
	if dto.RearImage != "" {
		row.RearImage = dto.RearImage
	}
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, row)
}
