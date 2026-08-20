package netbox

//
// factum-librenms-cli's Sync() needs a Netbox client but typically runs on
// the LibreNMS host, not the primary - so it pulls util.ConfigNetbox from
// the primary over REST instead of connecting to Postgres directly - see
// web.ApiNetboxConfig.
//

import (
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/netboxtool"
)

// remoteConfigResponse mirrors web.NetboxConfigResponse.
type remoteConfigResponse struct {
	util.CommonConfig
	URL   string `json:"url"`
	Token string `json:"token"`
}

// FetchRemoteConfig pulls the Netbox API connection settings from the
// primary, authenticated with factumConfig.Token (checked against the
// primary's Settings.FactumApiToken - see web.Controller.checkServiceToken).
func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*util.ConfigNetbox, error) {
	remote, err := util.FetchRemoteConfig[remoteConfigResponse](factumConfig, "/api/netbox-config")
	if err != nil {
		return nil, err
	}
	return &util.ConfigNetbox{
		CommonConfig: remote.CommonConfig,
		URL:          remote.URL,
		Token:        remote.Token,
	}, nil
}

// RemoteClient fetches util.ConfigNetbox from the primary and builds a
// netboxtool.NetboxClient from it.
func RemoteClient(factumConfig *util.ConfigFactum) (*netboxtool.NetboxClient, error) {
	cfg, err := FetchRemoteConfig(factumConfig)
	if err != nil {
		return nil, err
	}
	return netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   cfg.URL,
		Token: cfg.Token,
	})
}
