package devicesync

//
// factum2-device-sync-cli fetches its config (vrf_in_global/device_states/
// device_ignore/auth, all database-backed - see models.Settings and
// models.DeviceSyncAuth; inventory_maps from cfgmgmt service types) from
// factum2-web over REST. It runs on the primary and skips the hub unix
// socket (util.WithoutHubSocket). The Netbox client itself isn't fetched
// here - internal/netbox.RemoteClient already does that
// (GET /api/netbox-config), and callers use it directly.
//

import "github.com/abundo/factum2/internal/util"

// remoteConfigAuthEntry mirrors web.DeviceSyncAuthEntry.
type remoteConfigAuthEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// remoteConfigResponse mirrors web.DeviceSyncConfigResponse.
type remoteConfigResponse struct {
	util.CommonConfig
	VRFInGlobal   []string                         `json:"vrf_in_global"`
	DeviceStates  []string                         `json:"device_states"`
	DeviceIgnore  []string                         `json:"device_ignore"`
	VlanGroupName string                           `json:"vlan_group_name"`
	InventoryMaps map[string]string                `json:"inventory_maps"`
	Auth          map[string]remoteConfigAuthEntry `json:"auth"`
}

// FetchRemoteConfig pulls internal/device-sync's config from the primary,
// authenticated with factumConfig.Token.
func FetchRemoteConfig(factumConfig *util.ConfigFactum) (*util.ConfigDeviceSync, error) {
	remote, err := util.FetchRemoteConfig[remoteConfigResponse](factumConfig, "/api/device-sync-config")
	if err != nil {
		return nil, err
	}
	auth := make(map[string]util.ConfigDeviceSyncAuth, len(remote.Auth))
	for name, entry := range remote.Auth {
		auth[name] = util.ConfigDeviceSyncAuth{Username: entry.Username, Password: entry.Password}
	}
	return &util.ConfigDeviceSync{
		CommonConfig:  remote.CommonConfig,
		VRFInGlobal:   remote.VRFInGlobal,
		DeviceStates:  remote.DeviceStates,
		DeviceIgnore:  remote.DeviceIgnore,
		VlanGroupName: remote.VlanGroupName,
		InventoryMaps: remote.InventoryMaps,
		Auth:          auth,
	}, nil
}
