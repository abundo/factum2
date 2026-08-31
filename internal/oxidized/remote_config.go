package oxidized

//
// factum2-oxidized typically runs on a different host than the primary
// factum server, so its util.ConfigOxidized is pulled from the primary
// over REST instead of living in this host's own YAML config - see
// web.ApiOxidizedConfig.
//

import (
	"github.com/abundo/factum2/internal/util"
)

// remoteConfigResponse mirrors web.OxidizedConfigResponse.
type remoteConfigResponse struct {
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

// FetchRemoteConfig pulls the Oxidized API connection settings from the
// primary, authenticated with factumConfig.Token (checked against the
// primary's Settings.FactumApiToken - see web.Controller.checkServiceToken).
func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*util.ConfigOxidized, error) {
	remote, err := util.FetchRemoteConfig[remoteConfigResponse](factumConfig, "/api/oxidized-config")
	if err != nil {
		return nil, err
	}
	return &util.ConfigOxidized{
		CommonConfig: remote.CommonConfig,
		URL:          remote.URL,
		User:         remote.User,
		Pass:         remote.Pass,

		DestFile:            remote.DestFile,
		IgnoreDevices:       remote.IgnoreDevices,
		IgnoreManufacturers: remote.IgnoreManufacturers,
		IgnoreModels:        remote.IgnoreModels,
		IgnorePlatforms:     remote.IgnorePlatforms,
	}, nil
}

// RemoteClient is the FetchRemoteConfig + NewOxidizedClient convenience
// most callers want.
func RemoteClient(factumConfig *util.ConfigFactum) (*oxidizedClient, error) {
	cfg, err := FetchRemoteConfig(factumConfig)
	if err != nil {
		return nil, err
	}
	return NewOxidizedClient(*cfg), nil
}
