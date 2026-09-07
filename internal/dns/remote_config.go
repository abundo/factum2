package dns

//
// factum2-dns typically runs on a different host than the primary factum
// server, so its Config is pulled from the primary over REST instead of
// connecting directly to factum's Postgres - see web.ApiDNSConfig.
//

import (
	"github.com/abundo/factum2/internal/util"
)

// remoteConfigResponse mirrors web.DNSConfigResponse.
type remoteConfigResponse struct {
	util.CommonConfig
	DestFile        string              `json:"dest_file"`
	IgnoreModels    string              `json:"ignore_models"`
	IgnorePlatforms string              `json:"ignore_platforms"`
	ZonesEnabled    bool                `json:"zones_enabled"`
	ConfigFile      string              `json:"config_file"`
	DbFile          string              `json:"db_file"`
	Host            ConfigDNSHost       `json:"host_template"`
	SOATemplates    []ConfigDNSSOA      `json:"soa_templates"`
	Templates       []ConfigDNSTemplate `json:"zone_templates"`
	Zones           []ConfigDNSZone     `json:"zones"`
}

// FetchRemoteConfig pulls the DNS sync settings from the primary,
// authenticated with factumConfig.Token (checked against the primary's
// Settings.FactumApiToken - see web.Controller.checkServiceToken).
func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*Config, error) {
	remote, err := util.FetchRemoteConfig[remoteConfigResponse](factumConfig, "/api/dns-config")
	if err != nil {
		return nil, err
	}
	return &Config{
		ConfigDNS: util.ConfigDNS{
			CommonConfig:    remote.CommonConfig,
			DestFile:        remote.DestFile,
			IgnoreModels:    remote.IgnoreModels,
			IgnorePlatforms: remote.IgnorePlatforms,
		},
		ZonesEnabled: remote.ZonesEnabled,
		ConfigFile:   remote.ConfigFile,
		DbFile:       remote.DbFile,
		Host:         remote.Host,
		SOATemplates: remote.SOATemplates,
		Templates:    remote.Templates,
		Zones:        remote.Zones,
	}, nil
}
