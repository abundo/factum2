package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// IcingaConfigResponse is what factum2-icinga (internal/icinga's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type IcingaConfigResponse struct {
	util.CommonConfig
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`

	HostsFile           string                  `json:"hosts_file"`
	UsersFile           string                  `json:"users_file"`
	CertsFile           string                  `json:"certs_file"`
	IgnoreDevices       string                  `json:"ignore_devices"`
	DefaultNotification string                  `json:"default_notification"`
	HostTemplate        string                  `json:"host_template"`
	DependencyTemplate  string                  `json:"dependency_template"`
	UserTemplate        string                  `json:"user_template"`
	CertTemplate        string                  `json:"cert_template"`
	Certificates        []util.ConfigIcingaCert `json:"certificates"`
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
	var list []models.Certificate
	if err := ctrl.DB.Preload("Domains", func(db *gorm.DB) *gorm.DB { return db.Order("rank") }).
		Order("name").Find(&list).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	certs := make([]util.ConfigIcingaCert, 0, len(list))
	for _, cert := range list {
		domains := make([]string, 0, len(cert.Domains))
		for _, d := range cert.Domains {
			domains = append(domains, d.Name)
		}
		certs = append(certs, util.ConfigIcingaCert{
			Name: cert.Name, Host: cert.Host, Domains: domains,
		})
	}
	return c.JSON(http.StatusOK, IcingaConfigResponse{
		CommonConfig: util.NewCommonConfig(settings),
		URL:          settings.IcingaApiURL,
		Username:     settings.IcingaApiUser,
		Password:     settings.IcingaApiPass,

		HostsFile:           settings.IcingaHostsFile,
		UsersFile:           settings.IcingaUsersFile,
		CertsFile:           settings.IcingaCertsFile,
		IgnoreDevices:       settings.IcingaIgnoreDevices,
		DefaultNotification: settings.IcingaDefaultNotification,
		HostTemplate:        settings.IcingaHostTemplate,
		DependencyTemplate:  settings.IcingaDependencyTemplate,
		UserTemplate:        settings.IcingaUserTemplate,
		CertTemplate:        settings.IcingaCertTemplate,
		Certificates:        certs,
	})
}
