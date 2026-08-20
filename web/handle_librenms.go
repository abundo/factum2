package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// LibrenmsConfigResponse is what factum-librenms-cli (internal/librenms's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type LibrenmsConfigResponse struct {
	util.CommonConfig
	URL                  string `json:"url"`
	Key                  string `json:"key"`
	PersistentDevices    string `json:"persistent_devices"`
	DelayedDeleteEnabled bool   `json:"delayed_delete_enabled"`
	DelayedDeleteDays    int    `json:"delayed_delete_days"`
	RolesEnabled         string `json:"roles_enabled"`
	InterfacesDisabled   string `json:"interfaces_disabled"`
	SNMPVersion          string `json:"snmp_version"`
	SNMPCommunities      string `json:"snmp_communities"`
}

func settingOn(v *bool) bool {
	return v != nil && *v
}

// ApiLibrenmsConfig returns everything factum-librenms-cli needs to run -
// the default domain, LibreNMS REST API settings, and the Sync regex-filter
// lists, all from the database-backed Settings row - so factum-librenms-cli,
// which typically runs on a different host than the primary, doesn't need
// any local librenms config of its own. LibreNMS's own MySQL credentials
// aren't included here - factum-librenms-cli reads those directly from
// LibreNMS's .env file, since it assumes co-location with the LibreNMS
// server.
func (ctrl *Controller) ApiLibrenmsConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	days := settings.LibrenmsDelayedDeleteDays
	if days < 1 {
		days = 30
	}
	return c.JSON(http.StatusOK, LibrenmsConfigResponse{
		CommonConfig:         util.NewCommonConfig(settings),
		URL:                  settings.LibrenmsApiURL,
		Key:                  settings.LibrenmsApiToken,
		PersistentDevices:    settings.LibrenmsPersistentDevices,
		DelayedDeleteEnabled: settingOn(settings.LibrenmsDelayedDeleteEnabled),
		DelayedDeleteDays:    days,
		RolesEnabled:         settings.LibrenmsRolesEnabled,
		InterfacesDisabled:   settings.LibrenmsInterfacesDisabled,
		SNMPVersion:          settings.LibrenmsSNMPVersion,
		SNMPCommunities:      settings.LibrenmsSNMPCommunities,
	})
}

// ApiLibrenmsPendingDeleteList is GET /api/librenms/pending-deletes - the
// UI's pending-deletion table and factum-librenms-cli's delete pass.
func (ctrl *Controller) ApiLibrenmsPendingDeleteList(c *echo.Context) error {
	var rows []models.LibrenmsPendingDelete
	if err := ctrl.DB.Order("scheduled_at asc").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if rows == nil {
		rows = []models.LibrenmsPendingDelete{}
	}
	return c.JSON(http.StatusOK, rows)
}

// ApiLibrenmsPendingDeleteUpsert is PUT /api/librenms/pending-deletes/:device_id
// - factum-librenms-cli records a newly quarantined device. An existing row
// keeps its ScheduledAt and ForceDelete so a later sync cannot reset the
// clock or clear a user's "delete next run" flag.
func (ctrl *Controller) ApiLibrenmsPendingDeleteUpsert(c *echo.Context) error {
	deviceID, err := strconv.Atoi(c.Param("device_id"))
	if err != nil || deviceID < 1 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid device_id"})
	}

	var body models.LibrenmsPendingDelete
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	var row models.LibrenmsPendingDelete
	err = ctrl.DB.Where("device_id = ?", deviceID).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.LibrenmsPendingDelete{
			DeviceID:    deviceID,
			Hostname:    body.Hostname,
			Display:     body.Display,
			Reason:      body.Reason,
			ScheduledAt: body.ScheduledAt,
		}
		if err := ctrl.DB.Create(&row).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, row)
	}

	row.Hostname = body.Hostname
	row.Display = body.Display
	row.Reason = body.Reason
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, row)
}

// ApiLibrenmsPendingDeleteRemove is DELETE /api/librenms/pending-deletes/:device_id
// - factum-librenms-cli drops the row after a real LibreNMS delete or a restore.
func (ctrl *Controller) ApiLibrenmsPendingDeleteRemove(c *echo.Context) error {
	deviceID, err := strconv.Atoi(c.Param("device_id"))
	if err != nil || deviceID < 1 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid device_id"})
	}
	if err := ctrl.DB.Where("device_id = ?", deviceID).Delete(&models.LibrenmsPendingDelete{}).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// ApiLibrenmsPendingDeleteNextSync is POST
// /api/librenms/pending-deletes/:device_id/delete-next-sync - the UI's
// "delete on next sync" action. The next factum-librenms-cli run deletes
// the LibreNMS device even if the delay has not elapsed (and even if
// delayed deletion is currently disabled).
func (ctrl *Controller) ApiLibrenmsPendingDeleteNextSync(c *echo.Context) error {
	deviceID, err := strconv.Atoi(c.Param("device_id"))
	if err != nil || deviceID < 1 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid device_id"})
	}
	var row models.LibrenmsPendingDelete
	if err := ctrl.DB.Where("device_id = ?", deviceID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "pending delete not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	row.ForceDelete = true
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, row)
}
