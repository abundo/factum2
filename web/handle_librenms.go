package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// LibrenmsConfigResponse is what factum-librenms-cli (internal/librenms's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type LibrenmsConfigResponse struct {
	util.CommonConfig
	URL                string `json:"url"`
	Key                string `json:"key"`
	PersistentDevices  string `json:"persistent_devices"`
	RolesEnabled       string `json:"roles_enabled"`
	InterfacesDisabled string `json:"interfaces_disabled"`
	SNMPVersion        string `json:"snmp_version"`
	SNMPCommunities    string `json:"snmp_communities"`
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
	return c.JSON(http.StatusOK, LibrenmsConfigResponse{
		CommonConfig:       util.NewCommonConfig(settings),
		URL:                settings.LibrenmsApiURL,
		Key:                settings.LibrenmsApiToken,
		PersistentDevices:  settings.LibrenmsPersistentDevices,
		RolesEnabled:       settings.LibrenmsRolesEnabled,
		InterfacesDisabled: settings.LibrenmsInterfacesDisabled,
		SNMPVersion:        settings.LibrenmsSNMPVersion,
		SNMPCommunities:    settings.LibrenmsSNMPCommunities,
	})
}
