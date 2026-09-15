package certs

import "github.com/abundo/factum2/internal/util"

func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*Config, error) {
	return util.FetchRemoteConfig[Config](factumConfig, "/api/certs-config")
}
