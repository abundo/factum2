package storage

import "github.com/abundo/factum2/internal/util"

func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*Config, error) {
	cfg, err := util.FetchRemoteConfig[Config](factumConfig, "/api/storage-config")
	if err != nil {
		return nil, err
	}
	ApplyConfigDefaults(cfg)
	return cfg, nil
}
