package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

func (ctrl *Controller) opticalOn() bool {
	return optical.OpticalEnabled(ctrl.DB)
}

// RequireOpticalEnabled 404s optical routes when the Factum setting is off.
func (ctrl *Controller) RequireOpticalEnabled(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !ctrl.opticalOn() {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "optical modeling is disabled"})
		}
		return next(c)
	}
}

func normalizeKindMapName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (ctrl *Controller) ApiOpticalKindMapList(c *echo.Context) error {
	var rows []models.OpticalKindMap
	if err := ctrl.DB.Order("netbox_role_name").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiOpticalKindMapCreate(c *echo.Context) error {
	var dto models.OpticalKindMapDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	kind := optical.NormalizeOpticalKindCF(dto.OpticalKind)
	if kind == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid optical_kind"})
	}
	row := models.OpticalKindMap{
		NetboxRoleName: normalizeKindMapName(dto.NetboxRoleName),
		OpticalKind:    kind,
	}
	if row.NetboxRoleName == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "netbox_role_name required"})
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	_ = optical.ReresolveAllKinds(ctrl.DB)
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiOpticalKindMapUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var row models.OpticalKindMap
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.OpticalKindMapDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.NetboxRoleName != "" {
		row.NetboxRoleName = normalizeKindMapName(dto.NetboxRoleName)
	}
	if dto.OpticalKind != "" {
		kind := optical.NormalizeOpticalKindCF(dto.OpticalKind)
		if kind == "" {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid optical_kind"})
		}
		row.OpticalKind = kind
	}
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	_ = optical.ReresolveAllKinds(ctrl.DB)
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiOpticalKindMapDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Delete(&models.OpticalKindMap{}, id).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	_ = optical.ReresolveAllKinds(ctrl.DB)
	return c.NoContent(http.StatusNoContent)
}

type opticalPortDTO struct {
	Role       string  `json:"role"`
	FreqHz     uint64  `json:"freq_hz"`
	FreqTHz    float64 `json:"freq_thz"`
	FreqNm     float64 `json:"freq_nm"`
	ITUChannel *int    `json:"itu_channel"`
	ITUGridGHz float64 `json:"itu_grid_ghz"`
	Notes      string  `json:"notes"`
}

func (ctrl *Controller) ApiOpticalPortGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var port models.OpticalPort
	if err := ctrl.DB.Where("interface_id = ?", id).First(&port).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusOK, nil)
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, port)
}

func (ctrl *Controller) ApiOpticalPortPut(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var iface models.Interface
	if err := ctrl.DB.First(&iface, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "interface not found"})
	}
	var dto opticalPortDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	freq := dto.FreqHz
	if freq == 0 && dto.FreqTHz > 0 {
		freq = optical.HzFromTHz(dto.FreqTHz)
	}
	if freq == 0 && dto.FreqNm > 0 {
		freq = optical.HzFromNm(dto.FreqNm)
	}
	if freq == 0 && dto.ITUChannel != nil {
		freq = optical.FreqFromITU(*dto.ITUChannel, dto.ITUGridGHz)
	}
	if dto.Role == models.PortROADMAddDrop && freq == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "add/drop port requires a frequency"})
	}

	var port models.OpticalPort
	err = ctrl.DB.Where("interface_id = ?", id).First(&port).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		port = models.OpticalPort{InterfaceID: id}
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	port.Role = dto.Role
	port.FreqHz = freq
	port.ITUChannel = dto.ITUChannel
	port.Notes = dto.Notes
	if port.ID == 0 {
		if err := ctrl.DB.Create(&port).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	} else {
		if err := ctrl.DB.Save(&port).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}
	_ = optical.MarkStaleByInterface(ctrl.DB, id)
	return c.JSON(http.StatusOK, port)
}

func (ctrl *Controller) ApiOpticalPortDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Where("interface_id = ?", id).Delete(&models.OpticalPort{}).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	_ = optical.MarkStaleByInterface(ctrl.DB, id)
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiOpticalXConnectList(c *echo.Context) error {
	var deviceID uint
	_ = echo.QueryParamsBinder(c).Uint("device_id", &deviceID).BindError()
	if deviceID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "device_id required"})
	}
	var rows []models.OpticalXConnect
	if err := ctrl.DB.Where("device_id = ?", deviceID).Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiOpticalXConnectCreate(c *echo.Context) error {
	var xc models.OpticalXConnect
	if err := c.Bind(&xc); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	xc.ID = 0
	if err := optical.ValidateXConnect(ctrl.DB, &xc); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Create(&xc).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, xc)
}

func (ctrl *Controller) ApiOpticalXConnectDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Delete(&models.OpticalXConnect{}, id).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

type traceRequest struct {
	InterfaceID  uint   `json:"interface_id"`
	InterfaceAID uint   `json:"interface_a_id"`
	InterfaceZID uint   `json:"interface_z_id"`
	Mode         string `json:"mode"`
}

func (ctrl *Controller) ApiOpticalTrace(c *echo.Context) error {
	var req traceRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	start := req.InterfaceID
	if start == 0 {
		start = req.InterfaceAID
	}
	if start == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "interface_id required"})
	}
	g, err := optical.LoadGraph(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	res := optical.Walk(g, start, req.InterfaceZID, req.Mode)
	return c.JSON(http.StatusOK, res)
}

type pathAttachRequest struct {
	InterfaceAID uint   `json:"interface_a_id"`
	InterfaceZID uint   `json:"interface_z_id"`
	Mode         string `json:"mode"`
}

func (ctrl *Controller) ApiServicePathGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var path models.ServicePath
	if err := ctrl.DB.Where("service_id = ?", id).First(&path).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusOK, nil)
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var hops []models.ServiceHop
	_ = ctrl.DB.Where("service_id = ?", id).Order("seq").Find(&hops).Error
	path.Hops = hops
	return c.JSON(http.StatusOK, path)
}

func (ctrl *Controller) ApiServicePathPut(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var svc models.Service
	if err := ctrl.DB.First(&svc, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "service not found"})
	}
	cat := models.CategoryFromServiceID(svc.ServiceID)
	if cat == "CN" || cat == "CI" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "capacity services cannot attach an optical path"})
	}
	var req pathAttachRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if req.InterfaceAID == 0 || req.InterfaceZID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "both endpoints required"})
	}
	mode := req.Mode
	if mode == "" {
		if cat == "LF" || cat == "LI" {
			mode = models.TraceModeFiber
		} else {
			mode = models.TraceModeWDM
		}
	}
	// 409 if another VL/VI path already uses this client
	if mode == models.TraceModeWDM {
		var n int64
		ctrl.DB.Model(&models.ServicePath{}).
			Where("service_id <> ? AND mode = ? AND (endpoint_a_interface_id IN ? OR endpoint_z_interface_id IN ?)",
				id, models.TraceModeWDM,
				[]uint{req.InterfaceAID, req.InterfaceZID},
				[]uint{req.InterfaceAID, req.InterfaceZID}).
			Count(&n)
		if n > 0 {
			return c.JSON(http.StatusConflict, map[string]any{"error": "another wavelength service already uses this endpoint"})
		}
	}

	var path models.ServicePath
	err = ctrl.DB.Where("service_id = ?", id).First(&path).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		path = models.ServicePath{ServiceID: id, Status: models.PathNone}
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	path.Mode = mode
	path.EndpointAInterfaceID = req.InterfaceAID
	path.EndpointZInterfaceID = req.InterfaceZID
	if path.ID == 0 {
		if err := ctrl.DB.Create(&path).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	} else if err := ctrl.DB.Save(&path).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	g, err := optical.LoadGraph(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := optical.RebuildPath(ctrl.DB, g, id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return ctrl.ApiServicePathGet(c)
}

func (ctrl *Controller) ApiOpticalRetraceStale(c *echo.Context) error {
	if err := optical.RebuildStale(ctrl.DB); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

func (ctrl *Controller) ApiOpticalDeviceInventoryPut(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var inv optical.Inventory
	if err := c.Bind(&inv); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	res, err := optical.ApplyInventory(ctrl.DB, id, inv)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
		}
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, res)
}

func (ctrl *Controller) ApiDeviceOpticalPorts(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var ifaces []models.Interface
	if err := ctrl.DB.Where("device_id = ?", id).Find(&ifaces).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	ids := make([]uint, len(ifaces))
	for i, iface := range ifaces {
		ids[i] = iface.ID
	}
	var ports []models.OpticalPort
	if len(ids) > 0 {
		_ = ctrl.DB.Where("interface_id IN ?", ids).Find(&ports).Error
	}
	return c.JSON(http.StatusOK, ports)
}
