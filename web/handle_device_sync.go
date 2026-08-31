package web

import (
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// DeviceSyncAuthEntry is one entry of DeviceSyncConfigResponse.Auth - the
// REST-facing shape of models.DeviceSyncAuth (minus Name, which is the map
// key instead).
type DeviceSyncAuthEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// DeviceSyncConfigResponse is what internal/device-sync's FetchRemoteConfig
// parses - keep the JSON tags in sync with that type.
type DeviceSyncConfigResponse struct {
	util.CommonConfig
	VRFInGlobal   []string                       `json:"vrf_in_global"`
	DeviceStates  []string                       `json:"device_states"`
	DeviceIgnore  []string                       `json:"device_ignore"`
	VlanGroupName string                         `json:"vlan_group_name"`
	Auth          map[string]DeviceSyncAuthEntry `json:"auth"`
}

// splitLines splits a newline-separated Settings field (see
// Settings.DeviceSyncVRFInGlobal and friends) into a trimmed, non-empty
// []string - the REST-facing shape internal/device-sync's config expects.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// ApiDeviceSyncConfig returns everything factum2-device-sync-cli needs to
// run: the default domain and VRFInGlobal/DeviceStates/DeviceIgnore lists
// from the database-backed Settings row, plus per-device credentials from
// the models.DeviceSyncAuth table - so factum2-device-sync-cli, which
// typically runs on a host with network access to the devices rather than
// the primary, doesn't need any local device-sync config or DB access of
// its own. The Netbox client itself isn't served here - that's
// /api/netbox-config (web.ApiNetboxConfig), which device-sync fetches
// separately.
func (ctrl *Controller) ApiDeviceSyncConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	var authRows []models.DeviceSyncAuth
	if err := ctrl.DB.Find(&authRows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	auth := make(map[string]DeviceSyncAuthEntry, len(authRows))
	for _, row := range authRows {
		auth[row.Name] = DeviceSyncAuthEntry{Username: row.Username, Password: row.Password}
	}

	return c.JSON(http.StatusOK, DeviceSyncConfigResponse{
		CommonConfig:  util.NewCommonConfig(settings),
		VRFInGlobal:   splitLines(settings.DeviceSyncVRFInGlobal),
		DeviceStates:  splitLines(settings.DeviceSyncDeviceStates),
		DeviceIgnore:  splitLines(settings.DeviceSyncDeviceIgnore),
		VlanGroupName: settings.DeviceSyncVlanGroupName,
		Auth:          auth,
	})
}

// ApiDeviceSyncAuthCreate/ApiDeviceSyncAuthUpdate replace the generic
// SecureCRUDHandler for DeviceSyncAuth's write side: Password is json:"-"
// on the model (so GetAll/GetOne never leak it), but that same tag makes it
// invisible to SecureCRUDHandler's DTO<->model JSON round-trip in both
// directions - same reasoning as ApiWorkerNodeCreate/Update for
// WorkerNode.Token.
func (ctrl *Controller) ApiDeviceSyncAuthCreate(c *echo.Context) error {
	var dto models.DeviceSyncAuthDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	auth := models.DeviceSyncAuth{
		Name:     dto.Name,
		Username: dto.Username,
		Password: dto.Password,
	}
	if err := ctrl.DB.Create(&auth).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, auth)
}

func (ctrl *Controller) ApiDeviceSyncAuthUpdate(c *echo.Context) error {
	id := c.Param("id")
	var auth models.DeviceSyncAuth
	if err := ctrl.DB.First(&auth, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	var dto models.DeviceSyncAuthDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	auth.Name = dto.Name
	auth.Username = dto.Username
	// An empty password means "unchanged" (see DeviceSyncAuthDTO.Password's
	// doc comment) - the frontend leaves the field blank unless the operator
	// is actually changing it.
	if dto.Password != "" {
		auth.Password = dto.Password
	}

	if err := ctrl.DB.Save(&auth).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, auth)
}

// ApiDeviceSyncAuthToken returns one DeviceSyncAuth row's stored password.
// Kept separate from GetAll/GetOne (models.DeviceSyncAuth.Password is
// json:"-", so neither ever includes it) so a bulk list/detail fetch never
// carries secrets - used to prefill the admin UI's password field so its
// own eye icon can show it, same pattern as ApiWorkerNodeToken.
func (ctrl *Controller) ApiDeviceSyncAuthPassword(c *echo.Context) error {
	id := c.Param("id")
	var auth models.DeviceSyncAuth
	if err := ctrl.DB.First(&auth, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	return c.JSON(http.StatusOK, map[string]any{"password": auth.Password})
}
