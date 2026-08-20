package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// ApiCommonConfig returns util.CommonConfig - settings shared by every
// remote-config-fetching CLI tool, not tied to any one service. Tools that
// don't need any service-specific config (currently just factum-worker
// agents, via internal/worker.FetchRemoteConfig) fetch this directly instead
// of one of the per-service endpoints (ApiDNSConfig, ApiIcingaConfig, etc.),
// which already embed the same util.CommonConfig for tools that do need
// service-specific config too.
func (ctrl *Controller) ApiCommonConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, util.NewCommonConfig(settings))
}
