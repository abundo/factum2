package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// DCIM → Configuration loads a device's running config as a context tree
// and commits one context at a time. EOS uses a configure session.
// IOS-XR uses a candidate configure (commit, then abort). SROS-MD uses
// an MD-CLI exclusive candidate. Nokia's tree is read from configuration
// JSON; EOS and IOS-XR are read from CLI text. The device login is the
// device-sync credential, same as interface refresh.

type runningConfigCommitRequest struct {
	ContextID string `json:"context_id"`
	Body      string `json:"body"`
	BaseBody  string `json:"base_body"`
}

// ApiRunningConfigPlatforms lists platform slugs the page can edit.
func (ctrl *Controller) ApiRunningConfigPlatforms(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"platforms": drivers.RunningConfigPlatforms()})
}

// ApiRunningConfigGet fetches the running config and returns the context tree.
func (ctrl *Controller) ApiRunningConfigGet(c *echo.Context) error {
	device, drv, status, msg := ctrl.runningConfigDevice(c)
	if status != 0 {
		return c.JSON(status, map[string]any{"error": msg})
	}
	tree, err := loadRunningConfigTree(device, drv)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"device_id": device.ID,
		"name":      device.Name,
		"platform":  device.Platform,
		"contexts":  tree,
	})
}

// ApiRunningConfigCommit writes one context. The body is compared with
// the device's current text for that context. When the device rejects a
// command the session is aborted and the error is returned. On success
// the config is read again and the context before and after is diffed.
func (ctrl *Controller) ApiRunningConfigCommit(c *echo.Context) error {
	var req runningConfigCommitRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid request"})
	}
	req.ContextID = strings.TrimSpace(req.ContextID)
	if req.ContextID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "context_id is required"})
	}

	device, drv, status, msg := ctrl.runningConfigDevice(c)
	if status != 0 {
		return c.JSON(status, map[string]any{"error": msg})
	}
	committer, ok := drv.(drivers.RunningConfigCommitter)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "platform does not support committing configuration"})
	}

	tree, err := loadRunningConfigTree(device, drv)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	node := drivers.FindConfigContext(tree, req.ContextID)
	if node == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "context not found"})
	}
	if !node.Editable {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "this context is not editable"})
	}
	if drivers.NormalizeConfigText(req.BaseBody) != drivers.NormalizeConfigText(node.Body) {
		return c.JSON(http.StatusConflict, map[string]any{
			"error": "configuration changed on the device; refresh and edit again",
		})
	}
	enter, commands := drivers.ContextCommit(node, req.Body)
	if len(commands) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "no configuration changes to commit"})
	}

	comment := "factum configuration " + node.Label
	if who := sessionUserLabel(c); who != "" {
		comment += " by " + who
	}
	before := node.Body
	if err := committer.CommitRunningContext(enter, commands, comment); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
	}

	tree, err = loadRunningConfigTree(device, drv)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{
			"error": "committed, but reading the configuration back failed: " + err.Error(),
		})
	}
	after := ""
	if node = drivers.FindConfigContext(tree, req.ContextID); node != nil {
		after = node.Body
	}
	return c.JSON(http.StatusOK, map[string]any{
		"device_id":  device.ID,
		"name":       device.Name,
		"platform":   device.Platform,
		"context_id": req.ContextID,
		"diff":       drivers.UnifiedDiff(before, after),
		"contexts":   tree,
	})
}

func loadRunningConfigTree(device *models.Device, drv drivers.DriverClient) ([]drivers.ConfigContext, error) {
	cfg, err := drv.RunningConfigGet(drivers.RunningConfigAsJSON(device.Platform))
	if err != nil {
		return nil, fmt.Errorf("failed to read running configuration: %w", err)
	}
	text := ""
	if cfg != nil {
		text = cfg.ConfigStr
	}
	tree, err := drivers.BuildRunningConfigTree(device.Platform, text)
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// runningConfigDevice loads the device, checks the platform, and builds
// a driver. status is 0 on success.
func (ctrl *Controller) runningConfigDevice(c *echo.Context) (*models.Device, drivers.DriverClient, int, string) {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return nil, nil, http.StatusNotFound, "device not found"
	}
	devices, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(devices) == 0 {
		return nil, nil, http.StatusNotFound, "device not found"
	}
	device := devices[0]
	if !drivers.RunningConfigSupported(device.Platform) {
		return nil, nil, http.StatusBadRequest, "running configuration is not supported for platform " + device.Platform
	}
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err.Error()
	}
	creds, err := ctrl.deviceSyncCredentials(device.Name)
	if err != nil {
		return nil, nil, http.StatusBadGateway, err.Error()
	}
	drv, err := ctrl.newDriverForDevice(&device, creds, settings, sessionUserLabel(c))
	if err != nil {
		return nil, nil, http.StatusBadGateway, err.Error()
	}
	return &device, drv, 0, ""
}
