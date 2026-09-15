package certs

import (
	"strings"

	"github.com/abundo/factum2/models"
)

// Lab/production layout used when Destinations → Certificates paths are blank.
// Matches the dns container bind-mount (dev/data/lego → /var/lib/lego).
const (
	DefaultLegoYAML    = "/var/lib/lego/.lego.yaml"
	DefaultEnvFile     = "/var/lib/lego/.env"
	DefaultLegoBin     = "lego"
	DefaultLegoStorage = "/var/lib/lego/storage"
	DefaultKeyType     = "EC256"
)

func ApplyConfigDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.LegoYaml) == "" {
		cfg.LegoYaml = DefaultLegoYAML
	}
	if strings.TrimSpace(cfg.EnvFile) == "" {
		cfg.EnvFile = DefaultEnvFile
	}
	if strings.TrimSpace(cfg.LegoBin) == "" {
		cfg.LegoBin = DefaultLegoBin
	}
	if strings.TrimSpace(cfg.LegoStorage) == "" {
		cfg.LegoStorage = DefaultLegoStorage
	}
	if strings.TrimSpace(cfg.DefaultKeyType) == "" {
		cfg.DefaultKeyType = DefaultKeyType
	}
}

func ApplySettingsDefaults(s *models.Settings) {
	if s == nil {
		return
	}
	if strings.TrimSpace(s.CertsLegoYaml) == "" {
		s.CertsLegoYaml = DefaultLegoYAML
	}
	if strings.TrimSpace(s.CertsEnvFile) == "" {
		s.CertsEnvFile = DefaultEnvFile
	}
	if strings.TrimSpace(s.CertsLegoBin) == "" {
		s.CertsLegoBin = DefaultLegoBin
	}
	if strings.TrimSpace(s.CertsLegoStorage) == "" {
		s.CertsLegoStorage = DefaultLegoStorage
	}
	if strings.TrimSpace(s.CertsDefaultKeyType) == "" {
		s.CertsDefaultKeyType = DefaultKeyType
	}
}
