package librenms

//
// factum-librenms-cli typically runs on a different host than the primary
// factum server, so its whole util.ConfigLibrenms - REST API URL/key and the
// Sync regex-filter lists - is pulled from the primary over REST instead of
// living in this host's own YAML config - see web.ApiLibrenmsConfig.
// LibreNMS's own MySQL credentials aren't fetched this way - see
// NewFactumLibrenmsClient in factum-librenms.go, which reads them directly
// from LibreNMS's .env file on disk, since factum-librenms-cli assumes
// co-location with LibreNMS.
//

import (
	"github.com/abundo/factum2/internal/util"
)

// remoteConfigResponse mirrors web.LibrenmsConfigResponse.
type remoteConfigResponse struct {
	util.CommonConfig
	URL                  string `json:"url"`
	Key                  string `json:"key"`
	PersistentDevices    string `json:"persistent_devices"`
	DelayedDeleteEnabled bool   `json:"delayed_delete_enabled"`
	DelayedDeleteDays    int    `json:"delayed_delete_days"`
	RolesEnabled         string `json:"roles_enabled"`
	InterfacesDisabled   string `json:"interfaces_disabled"`
	SNMPVersion          string `json:"snmp_version"`
	SNMPCommunities      string `json:"snmp_communities"`
}

// FetchRemoteConfig pulls the full LibreNMS config - default domain, REST
// API connection settings, and the Sync regex-filter lists - from the
// primary, authenticated with factumConfig.Token (checked against the
// primary's Settings.FactumApiToken - see web.Controller.checkServiceToken).
func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*util.ConfigLibrenms, error) {
	remote, err := util.FetchRemoteConfig[remoteConfigResponse](factumConfig, "/api/librenms-config")
	if err != nil {
		return nil, err
	}
	return &util.ConfigLibrenms{
		CommonConfig:         remote.CommonConfig,
		URL:                  remote.URL,
		Key:                  remote.Key,
		PersistentDevices:    remote.PersistentDevices,
		DelayedDeleteEnabled: remote.DelayedDeleteEnabled,
		DelayedDeleteDays:    remote.DelayedDeleteDays,
		RolesEnabled:         remote.RolesEnabled,
		InterfacesDisabled:   remote.InterfacesDisabled,
		SNMPVersion:          remote.SNMPVersion,
		SNMPCommunities:      remote.SNMPCommunities,
	}, nil
}

// RemoteClient is the FetchRemoteConfig + NewLibrenmsClient convenience
// most callers want.
func RemoteClient(factumConfig *util.ConfigFactum) (*LibrenmsClient, error) {
	cfg, err := FetchRemoteConfig(factumConfig)
	if err != nil {
		return nil, err
	}
	return NewLibrenmsClient(cfg), nil
}
