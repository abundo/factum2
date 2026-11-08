package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// PrometheusConfigResponse is what factum2-prometheus (internal/prometheus's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type PrometheusConfigResponse struct {
	util.CommonConfig
	DestFile            string `json:"dest_file"`
	ReloadURL           string `json:"reload_url"`
	Module              string `json:"module"`
	Auth                string `json:"auth"`
	IgnoreDevices       string `json:"ignore_devices"`
	IgnoreManufacturers string `json:"ignore_manufacturers"`
	IgnoreModels        string `json:"ignore_models"`
	IgnorePlatforms     string `json:"ignore_platforms"`
	IgnoreInterfaces    string `json:"ignore_interfaces"`
	RolesEnabled        string `json:"roles_enabled"`
}

// ApiPrometheusConfig returns the Prometheus/snmp_exporter sync settings
// from the database-backed Settings row, so factum2-prometheus - which
// typically runs on a different host than the primary - doesn't need its
// own copy of these in a local YAML config file.
func (ctrl *Controller) ApiPrometheusConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	filter := settings.PrometheusDeviceFilter()
	return c.JSON(http.StatusOK, PrometheusConfigResponse{
		CommonConfig:        util.NewCommonConfig(settings),
		DestFile:            settings.PrometheusDestFile,
		ReloadURL:           settings.PrometheusReloadURL,
		Module:              settings.PrometheusModule,
		Auth:                settings.PrometheusAuth,
		IgnoreDevices:       filter.Devices,
		IgnoreManufacturers: filter.Manufacturers,
		IgnoreModels:        filter.Models,
		IgnorePlatforms:     filter.Platforms,
		IgnoreInterfaces:    settings.PrometheusIgnoreInterfacesEffective(),
		RolesEnabled:        filter.Roles,
	})
}
