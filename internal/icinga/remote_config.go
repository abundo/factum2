package icinga

//
// factum2-icinga typically runs on a different host than the primary factum
// server, so its util.ConfigIcinga is pulled from the primary over REST
// instead of living in this host's own YAML config - see
// web.ApiIcingaConfig.
//

import (
	"github.com/abundo/factum2/internal/util"
)

// remoteConfigResponse mirrors web.IcingaConfigResponse.
type remoteConfigResponse struct {
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

// FetchRemoteConfig pulls the Icinga API connection settings from the
// primary, authenticated with factumConfig.Token (checked against the
// primary's Settings.FactumApiToken - see web.Controller.checkServiceToken).
func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*util.ConfigIcinga, error) {
	remote, err := util.FetchRemoteConfig[remoteConfigResponse](factumConfig, "/api/icinga-config")
	if err != nil {
		return nil, err
	}
	return &util.ConfigIcinga{
		CommonConfig: remote.CommonConfig,
		URL:          remote.URL,
		Username:     remote.Username,
		Password:     remote.Password,

		HostsFile:           remote.HostsFile,
		UsersFile:           remote.UsersFile,
		IgnoreDevices:       remote.IgnoreDevices,
		DefaultNotification: remote.DefaultNotification,
		HostTemplate:        remote.HostTemplate,
		DependencyTemplate:  remote.DependencyTemplate,
		UserTemplate:        remote.UserTemplate,
	}, nil
}

// RemoteClient is the FetchRemoteConfig + NewIcingaClient convenience most
// callers want.
func RemoteClient(factumConfig *util.ConfigFactum) (*icingaClient, error) {
	cfg, err := FetchRemoteConfig(factumConfig)
	if err != nil {
		return nil, err
	}
	return NewIcingaClient(*cfg), nil
}
