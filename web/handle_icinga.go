package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// IcingaConfigResponse is what factum2-icinga (internal/icinga's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type IcingaConfigResponse struct {
	util.CommonConfig
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`

	HostsFile           string `json:"hosts_file"`
	UsersFile           string `json:"users_file"`
	IgnoreDevices       string `json:"ignore_devices"`
	DefaultNotification string `json:"default_notification"`
	HostTemplate        string `json:"host_template"`
	DependencyTemplate  string `json:"dependency_template"`
	UserTemplate        string `json:"user_template"`
}

// ApiIcingaConfig returns the Icinga API connection settings from the
// database-backed Settings row, so factum2-icinga - which typically runs on
// a different host than the primary - doesn't need its own copy of these
// credentials in a local YAML config file.
func (ctrl *Controller) ApiIcingaConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, IcingaConfigResponse{
		CommonConfig: util.NewCommonConfig(settings),
		URL:          settings.IcingaApiURL,
		Username:     settings.IcingaApiUser,
		Password:     settings.IcingaApiPass,

		HostsFile:           settings.IcingaHostsFile,
		UsersFile:           settings.IcingaUsersFile,
		IgnoreDevices:       settings.IcingaIgnoreDevices,
		DefaultNotification: settings.IcingaDefaultNotification,
		HostTemplate:        settings.IcingaHostTemplate,
		DependencyTemplate:  settings.IcingaDependencyTemplate,
		UserTemplate:        settings.IcingaUserTemplate,
	})
}
