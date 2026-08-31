package prometheus

//
// factum2-prometheus typically runs on a different host than the primary
// factum server (the Prometheus / snmp_exporter host), so its
// util.ConfigPrometheus is pulled from the primary over REST instead of
// living in this host's own YAML config - see web.ApiPrometheusConfig.
//

import (
	"github.com/abundo/factum2/internal/util"
)

// remoteConfigResponse mirrors web.PrometheusConfigResponse.
type remoteConfigResponse struct {
	util.CommonConfig
	DestFile            string `json:"dest_file"`
	ReloadURL           string `json:"reload_url"`
	Module              string `json:"module"`
	Auth                string `json:"auth"`
	IgnoreDevices       string `json:"ignore_devices"`
	IgnoreManufacturers string `json:"ignore_manufacturers"`
	IgnoreModels        string `json:"ignore_models"`
	IgnorePlatforms     string `json:"ignore_platforms"`
}

// FetchRemoteConfig pulls the Prometheus/snmp_exporter sync settings from
// the primary, authenticated with factumConfig.Token (checked against the
// primary's Settings.FactumApiToken - see web.Controller.checkServiceToken).
func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*util.ConfigPrometheus, error) {
	remote, err := util.FetchRemoteConfig[remoteConfigResponse](factumConfig, "/api/prometheus-config")
	if err != nil {
		return nil, err
	}
	return &util.ConfigPrometheus{
		CommonConfig:        remote.CommonConfig,
		DestFile:            remote.DestFile,
		ReloadURL:           remote.ReloadURL,
		Module:              remote.Module,
		Auth:                remote.Auth,
		IgnoreDevices:       remote.IgnoreDevices,
		IgnoreManufacturers: remote.IgnoreManufacturers,
		IgnoreModels:        remote.IgnoreModels,
		IgnorePlatforms:     remote.IgnorePlatforms,
	}, nil
}
