package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// DNSConfigResponse is what factum2-dns (internal/dns's FetchRemoteConfig)
// parses - keep the JSON tags in sync with that type.
type DNSConfigResponse struct {
	util.CommonConfig
	DestFile        string `json:"dest_file"`
	IgnoreModels    string `json:"ignore_models"`
	IgnorePlatforms string `json:"ignore_platforms"`
}

// ApiDNSConfig returns the DNS sync settings from the database-backed
// Settings row, so factum2-dns - which typically runs on a different host
// than the primary - doesn't need a direct Postgres connection just to read
// these.
func (ctrl *Controller) ApiDNSConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, DNSConfigResponse{
		CommonConfig:    util.NewCommonConfig(settings),
		DestFile:        settings.DnsDestFile,
		IgnoreModels:    settings.DnsIgnoreModels,
		IgnorePlatforms: settings.DnsIgnorePlatforms,
	})
}
