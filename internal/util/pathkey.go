package util

import (
	"fmt"
	"strings"
)

// worker.paths keys. Admin → Destinations stores one of these names.
// The filesystem path is only in the worker YAML.
const (
	KeyLegoBin              = "lego_bin"
	KeyLegoYaml             = "lego_yaml"
	KeyLegoEnv              = "lego_env"
	KeyLegoStorage          = "lego_storage"
	KeyDNSDestFile          = "dns_dest_file"
	KeyDNSZonesFile         = "dns_zones_file"
	KeyDNSPrefixesFile      = "dns_prefixes_file"
	KeyOxidizedDestFile     = "oxidized_dest_file"
	KeyPrometheusDestFile   = "prometheus_dest_file"
	KeyIcingaHostsFile      = "icinga_hosts_file"
	KeyIcingaUsersFile      = "icinga_users_file"
	KeyIcingaCertsFile      = "icinga_certs_file"
	KeyIcingaInterfacesFile = "icinga_interfaces_file"
)

// pin returns the worker-local value for a known key. known is false when
// key is not a worker.paths name.
func (p ConfigWorkerPaths) pin(key string) (string, bool) {
	switch key {
	case KeyLegoBin:
		return p.LegoBin, true
	case KeyLegoYaml:
		return p.LegoYaml, true
	case KeyLegoEnv:
		return p.LegoEnv, true
	case KeyLegoStorage:
		return p.LegoStorage, true
	case KeyDNSDestFile:
		return p.DNSDestFile, true
	case KeyDNSZonesFile:
		return p.DNSZonesFile, true
	case KeyDNSPrefixesFile:
		return p.DNSPrefixesFile, true
	case KeyOxidizedDestFile:
		return p.OxidizedDestFile, true
	case KeyPrometheusDestFile:
		return p.PrometheusDestFile, true
	case KeyIcingaHostsFile:
		return p.IcingaHostsFile, true
	case KeyIcingaUsersFile:
		return p.IcingaUsersFile, true
	case KeyIcingaCertsFile:
		return p.IcingaCertsFile, true
	case KeyIcingaInterfacesFile:
		return p.IcingaInterfacesFile, true
	default:
		return "", false
	}
}

// ValidateWorkerPathKey accepts an empty value or a worker.paths key.
// A filesystem path is rejected.
func ValidateWorkerPathKey(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if _, known := (ConfigWorkerPaths{}).pin(value); !known {
		return fmt.Errorf("%q is not a worker.paths key", value)
	}
	return nil
}

// ResolvePinnedKey maps a destination setting to a path from the worker
// config. remote is a worker.paths key from the primary, never a filesystem
// path. An empty remote selects defaultKey. The returned path always comes
// from paths.
//
// optional: an empty remote and an empty pin return "", nil. A non-empty
// remote still requires that key's pin.
// executable: the pin may be a bare command name (lego_bin).
func ResolvePinnedKey(paths ConfigWorkerPaths, remote, defaultKey string, optional, executable bool) (string, error) {
	remote = strings.TrimSpace(remote)
	key := remote
	if key == "" {
		key = defaultKey
	}
	local, known := paths.pin(key)
	if !known {
		return "", fmt.Errorf("%q is not a worker.paths key", key)
	}
	local = strings.TrimSpace(local)
	if local == "" {
		if optional && remote == "" {
			return "", nil
		}
		return "", fmt.Errorf("worker.paths.%s is not set", key)
	}
	name := "worker.paths." + key
	if executable {
		return PinnedExecutable(local, "", name)
	}
	return PinnedPath(local, "", name)
}
