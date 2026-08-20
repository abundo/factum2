package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// OxidizedConfigResponse is what factum-oxidized (internal/oxidized's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type OxidizedConfigResponse struct {
	util.CommonConfig
	URL  string `json:"url"`
	User string `json:"user"`
	Pass string `json:"pass"`

	DestFile            string `json:"dest_file"`
	IgnoreDevices       string `json:"ignore_devices"`
	IgnoreManufacturers string `json:"ignore_manufacturers"`
	IgnoreModels        string `json:"ignore_models"`
	IgnorePlatforms     string `json:"ignore_platforms"`
}

// ApiOxidizedConfig returns the Oxidized API connection settings from the
// database-backed Settings row, so factum-oxidized - which typically runs
// on a different host than the primary - doesn't need its own copy of
// these credentials in a local YAML config file.
func (ctrl *Controller) ApiOxidizedConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, OxidizedConfigResponse{
		CommonConfig: util.NewCommonConfig(settings),
		URL:          settings.OxidizedApiURL,
		User:         settings.OxidizedApiUser,
		Pass:         settings.OxidizedApiPass,

		DestFile:            settings.OxidizedDestFile,
		IgnoreDevices:       settings.OxidizedIgnoreDevices,
		IgnoreManufacturers: settings.OxidizedIgnoreManufacturers,
		IgnoreModels:        settings.OxidizedIgnoreModels,
		IgnorePlatforms:     settings.OxidizedIgnorePlatforms,
	})
}
