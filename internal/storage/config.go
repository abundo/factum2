package storage

import (
	"strings"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

const (
	DefaultRoot        = "/var/lib/factum2/storage"
	DefaultHTTPListen  = ":8088"
	DefaultTFTPListen  = ":69"
	DefaultSFTPListen  = ":2222"
	DefaultSFTPUser    = "factum"
	DefaultStorageSock = "/run/factum2-storage/api.sock"
)

// Config is what factum2-storage fetches from the primary
// (GET /api/storage-config). Paths and listen addresses apply on the
// storage host, which may be the primary or a remote worker.
type Config struct {
	util.CommonConfig
	Root         string `json:"root"`
	HTTPListen   string `json:"http_listen"`
	HTTPURL      string `json:"http_url"`
	TFTPListen   string `json:"tftp_listen"`
	TFTPHost     string `json:"tftp_host"`
	SFTPListen   string `json:"sftp_listen"`
	SFTPHost     string `json:"sftp_host"`
	SFTPUser     string `json:"sftp_user"`
	SFTPPassword string `json:"sftp_password"`
}

func ApplyConfigDefaults(c *Config) {
	if c.Root == "" {
		c.Root = DefaultRoot
	}
}

// ApplySettingsDefaults fills empty storage paths when the feature is on.
func ApplySettingsDefaults(s *models.Settings) {
	if s == nil {
		return
	}
	if strings.TrimSpace(s.StorageRoot) == "" {
		s.StorageRoot = DefaultRoot
	}
}

// StorageSocketPath resolves the daemon unix API path. yamlOverride is
// ConfigStorage.Socket. "none"/"0" disables the socket. Empty uses
// DefaultStorageSock.
func StorageSocketPath(yamlOverride string) string {
	if yamlOverride == "none" || yamlOverride == "0" {
		return ""
	}
	if yamlOverride != "" {
		return yamlOverride
	}
	return DefaultStorageSock
}
