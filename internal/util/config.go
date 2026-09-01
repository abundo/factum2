package util

type ConfigDB struct {
	Host     string `boa:"configonly" yaml:"host"`
	Port     string `boa:"configonly" yaml:"port"`
	User     string `boa:"configonly" yaml:"user"`
	Pass     string `boa:"configonly" yaml:"pass"`
	Database string `boa:"configonly" yaml:"database"`
}
type ConfigFactum struct {
	// URL/Token are optional at the boa level - like ConfigWorker.Roles,
	// not every subcommand of every binary that embeds ConfigFactum
	// actually calls out to the primary (e.g. "factum2-worker start" never
	// does, only "run" does, via internal/worker.RunRemote), so requiring
	// them unconditionally would force values into a config file that
	// aren't needed for that particular subcommand. Call sites that do
	// need them (RunRemote, internal/factum.FactumClient) check for an
	// empty URL/Token themselves and fail with a clear error instead.
	URL string `boa:"configonly" yaml:"url" optional:"true"`
	// Token authenticates server-to-server callers (internal/factum's HTTP
	// client, used by e.g. factum2-dns and factum2-librenms-cli, the latter
	// typically running on a different host than the primary) against the
	// primary's Settings.FactumApiToken, sent as "Authorization: Bearer
	// <token>". See web.Controller.RequireAPIAuth.
	Token string `boa:"configonly" yaml:"token" optional:"true"`
	// Socket is the local unix API path when this CLI is co-located with
	// factum2-worker. Empty uses DefaultHubSocket unless FACTUM_WORKER_API_SOCKET
	// overrides it; "none"/"0" disables the socket (CLI-only escape hatch).
	Socket string `boa:"configonly" yaml:"socket" optional:"true"`
}

// ConfigIcinga is a runtime-only DTO, not part of ConfigRoot - unlike
// ConfigLibrenms's Sync maps, every field here already has a DB-backed
// equivalent (Settings.IcingaApiURL/User/Pass/HostsFile/UsersFile/
// IgnoreDevices/DefaultNotification/HostTemplate/DependencyTemplate/
// UserTemplate), so factum2-icinga - which typically runs on a different
// host than the primary - fetches this entirely over REST
// (internal/icinga.FetchRemoteConfig/RemoteClient, GET /api/icinga-config,
// served by web.ApiIcingaConfig from the Settings row) rather than reading
// any of it from local YAML.
type ConfigIcinga struct {
	CommonConfig
	URL      string
	Username string
	Password string

	HostsFile string
	UsersFile string

	// IgnoreDevices is a newline-separated list of device names to skip
	// entirely (Settings.IcingaIgnoreDevices).
	IgnoreDevices string
	// DefaultNotification is applied to a device with no
	// CfAlarmDestination set (Settings.IcingaDefaultNotification).
	DefaultNotification string

	// HostTemplate/DependencyTemplate/UserTemplate are Go text/template
	// source, executed by internal/icinga.FactumIcingaClient - see that
	// package for the data each is executed with.
	HostTemplate       string
	DependencyTemplate string
	UserTemplate       string
}

// ConfigOxidized is a runtime-only DTO, not part of ConfigRoot - same
// pattern as ConfigIcinga: every field has a DB-backed equivalent
// (Settings.OxidizedApiURL/User/Pass/DestFile/IgnoreDevices/
// IgnoreManufacturers/IgnoreModels/IgnorePlatforms), fetched over REST by
// internal/oxidized.FetchRemoteConfig/RemoteClient from web.ApiOxidizedConfig.
type ConfigOxidized struct {
	CommonConfig
	URL  string
	User string
	Pass string

	// DestFile is oxidized's own router.db - the file
	// internal/oxidized.FactumOxidizedClient.Sync writes the filtered
	// device list to (name:ip:model per line).
	DestFile string

	// IgnoreDevices/IgnoreManufacturers/IgnoreModels/IgnorePlatforms are
	// newline-separated lists (one value per line) - a device matching any
	// one of them is skipped during sync.
	IgnoreDevices       string
	IgnoreManufacturers string
	IgnoreModels        string
	IgnorePlatforms     string
}

// ConfigPrometheus is a runtime-only DTO, not part of ConfigRoot - same
// pattern as ConfigOxidized: every field has a DB-backed equivalent
// (Settings.PrometheusDestFile/ReloadURL/Module/Auth/Ignore*), fetched over
// REST by internal/prometheus.FetchRemoteConfig from web.ApiPrometheusConfig.
// factum2-prometheus typically runs on the Prometheus/snmp_exporter host,
// not the primary, so it has no local prometheus YAML of its own.
type ConfigPrometheus struct {
	CommonConfig
	DestFile  string
	ReloadURL string
	Module    string
	Auth      string

	IgnoreDevices       string
	IgnoreManufacturers string
	IgnoreModels        string
	IgnorePlatforms     string
}

// ConfigDNS is a runtime-only DTO, not part of ConfigRoot - same pattern as
// ConfigIcinga: every field has a DB-backed equivalent
// (Settings.DnsDestFile/DnsIgnoreModels/DnsIgnorePlatforms, plus
// Settings.DefaultDomain via the embedded CommonConfig), fetched over REST
// by internal/dns.FetchRemoteConfig/RemoteClient from web.ApiDNSConfig.
type ConfigDNS struct {
	CommonConfig
	DestFile        string
	IgnoreModels    string
	IgnorePlatforms string
}

// ConfigNetbox is a runtime-only DTO, not part of ConfigRoot - same pattern
// as ConfigIcinga: every field has a DB-backed equivalent
// (Settings.NetboxApiURL/NetboxApiToken), fetched over REST by
// internal/netbox.FetchRemoteConfig/RemoteClient from web.ApiNetboxConfig -
// so factum2-librenms-cli (which typically runs on a different host than the
// primary, without direct Postgres access) doesn't need its own copy of
// these credentials, or a direct DB connection, to build a Netbox client.
type ConfigNetbox struct {
	CommonConfig
	URL   string
	Token string
}

// ConfigDeviceSyncAuth is the username/password internal/device-sync uses to
// log into a device - keyed by device name in ConfigDeviceSync.Auth, with a
// "default" entry as the fallback for devices without their own entry.
type ConfigDeviceSyncAuth struct {
	Username string
	Password string
}

// ConfigDeviceSync is a runtime-only DTO, not part of ConfigRoot - same
// pattern as ConfigNetbox/ConfigOxidized: every scalar-ish field has a
// DB-backed equivalent (Settings.DeviceSyncVRFInGlobal/DeviceStates/
// DeviceIgnore, all newline-separated text; Auth comes from the
// models.DeviceSyncAuth table instead of a Settings column, since it's a
// list of credentials, not a single value), fetched over REST by
// internal/device-sync.FetchRemoteConfig/RemoteClient from
// web.ApiDeviceSyncConfig - factum2-device-sync-cli typically runs on a host
// with network access to the devices, not the primary, so it has no direct
// DB connection to read either from. The Netbox client itself isn't fetched
// here - internal/netbox.RemoteClient/FetchRemoteConfig already does that
// (GET /api/netbox-config), and device-sync reuses it rather than
// duplicating Netbox credentials in a second config type.
type ConfigDeviceSync struct {
	CommonConfig
	VRFInGlobal  []string
	DeviceStates []string
	DeviceIgnore []string
	// VlanGroupName is the Netbox VLAN Group every synced VLAN is created
	// in (Settings.DeviceSyncVlanGroupName) - "" disables VLAN sync.
	VlanGroupName string
	Auth          map[string]ConfigDeviceSyncAuth
}

// ConfigLibrenms is a runtime-only DTO, not part of ConfigRoot - same
// pattern as ConfigIcinga/ConfigDNS/ConfigOxidized: every field has a
// DB-backed equivalent (Settings.LibrenmsApiURL/LibrenmsApiToken/
// LibrenmsPersistentDevices/LibrenmsDelayedDeleteEnabled/
// LibrenmsDelayedDeleteDays/LibrenmsRolesEnabled/LibrenmsInterfacesDisabled),
// fetched over REST by internal/librenms.FetchRemoteConfig/RemoteClient from
// web.ApiLibrenmsConfig - so factum2-librenms-cli, which typically runs on a
// different host than the primary, doesn't need any local librenms config of
// its own. LibreNMS's own MySQL credentials aren't part of this struct at
// all - NewFactumLibrenmsClient reads those directly from LibreNMS's .env
// file on disk instead, since factum2-librenms-cli assumes co-location with
// the LibreNMS server.
type ConfigLibrenms struct {
	CommonConfig
	URL string
	Key string

	// PersistentDevices is a newline-separated list of LibreNMS hostnames
	// or display names sync never quarantines or deletes - see
	// Settings.LibrenmsPersistentDevices's doc comment.
	PersistentDevices string
	// DelayedDeleteEnabled/DelayedDeleteDays control the LibreNMS delete
	// path - see Settings.LibrenmsDelayedDeleteEnabled's doc comment.
	DelayedDeleteEnabled bool
	DelayedDeleteDays    int
	// RolesEnabled/InterfacesDisabled are newline-separated lists of
	// regexes - see Settings.LibrenmsRolesEnabled's doc comment.
	RolesEnabled       string
	InterfacesDisabled string

	// SNMPVersion/SNMPCommunities are used when creating devices in
	// LibreNMS - see Settings.LibrenmsSNMPVersion/LibrenmsSNMPCommunities's
	// doc comments. SNMPCommunities is newline-separated, same convention
	// as RolesEnabled/InterfacesDisabled.
	SNMPVersion     string
	SNMPCommunities string
}

// ConfigWorkerCommand is one predefined command a worker agent is allowed
// to run. Agents only ever execute commands looked up by name from this
// map - the command arriving over the hub connection is never used to
// build a shell command directly, so a compromised/forged message can at
// most pick one of these predefined commands.
type ConfigWorkerCommand struct {
	Cmd  string   `boa:"configonly" yaml:"cmd"`
	Args []string `boa:"configonly" yaml:"args"`
}

type ConfigWorker struct {
	// Roles lists what this worker instance does. "primary" runs the
	// primary loop (dispatches ad hoc commands via "factum2-worker run" and
	// logs everything agents report); any other entry is the name of a
	// Commands entry that this instance additionally runs as an agent for
	// - so one worker process can be primary AND handle one or more
	// specific agent roles (e.g. ["primary", "librenms"]) at once, instead
	// of being purely one or the other. Every non-"primary" role must have
	// a matching Commands entry.
	//
	// optional:"true" because boa's required-field check would otherwise
	// treat this flat []string as required (unlike map fields, which
	// default to optional) for every cmd/worker subcommand, including ones
	// like "run" and "show-config" that never look at it - Worker.Start
	// does its own "at least one role" check instead, only when it
	// actually matters.
	Roles []string `boa:"configonly" yaml:"roles" optional:"true"`
	// Commands lists the predefined commands this worker is able to run as
	// an agent, keyed by name - Roles selects which of these (if any) this
	// particular instance actually activates. An agent only ever receives
	// commands it has both defined here and activated via Roles - there is
	// no separate addressing by node name.
	Commands map[string]ConfigWorkerCommand `boa:"configonly" yaml:"commands"`

	// Listen is the bind address (e.g. ":8443") for this agent's hub
	// listener (internal/worker.runHubListener), which the primary's
	// RemoteManager dials into - the reverse of this agent dialing out.
	// Empty (the default) disables the listener entirely.
	Listen string `boa:"configonly" yaml:"listen" optional:"true"`
	// Token is the shared secret this agent expects from the primary as
	// "Authorization: Bearer <Token>" on the hub connection - the primary
	// side of the same value lives on the matching models.WorkerNode row,
	// not in any config file.
	Token string `boa:"configonly" yaml:"token" optional:"true"`
	// APISocket is the unix HTTP listener for hub RPC. Empty uses
	// DefaultHubSocket / FACTUM_WORKER_API_SOCKET; "none"/"0" is invalid
	// for factum2-worker start (fail closed). Not a route on worker.listen.
	APISocket string `boa:"configonly" yaml:"api_socket" optional:"true"`
}

type ConfigWeb struct {
	Bind      string
	JWTSecret string
}

// ConfigLdapWriteback is the elevated LDAP/AD identity permitted to change
// another user's password (web.ApiForgotPassword/ApiResetPassword,
// web.ApiMeUpdate) - deliberately separate from Settings.LdapBindDN/
// LdapBindPassword (the read-only search/bind service account, DB-stored
// and returned in full by GET /api/admin/settings). Password write-back
// needs much stronger directory permissions than a search bind, so unlike
// every other LDAP setting it stays config-file-only and is never exposed
// through the Settings API - same reasoning as ConfigWeb.JWTSecret. Both
// fields are optional since most installs never enable LDAP password
// write-back at all (Settings.LdapAllowPasswordChange defaults to off).
type ConfigLdapWriteback struct {
	BindDN   string `boa:"configonly" yaml:"bind_dn" optional:"true"`
	Password string `boa:"configonly" yaml:"bind_password" optional:"true"`
}

// Primary configuation, enough to get it up and running. most coinfig is in database
type ConfigRoot struct {
	DB            ConfigDB            `yaml:"db"`
	Factum        ConfigFactum        `yaml:"factum"`
	Web           ConfigWeb           `yaml:"web"`
	Worker        ConfigWorker        `yaml:"worker"`
	LdapWriteback ConfigLdapWriteback `yaml:"ldap_writeback"`
}

// Agents get most of their configuration from the Factum API - factum2-worker
// is the one exception, needing its own local Worker section (worker.listen/
// .token/.roles/.commands - see the comment on ConfigWorker for why these
// must stay local rather than fetched remotely).
type ConfigAgentRoot struct {
	Factum ConfigFactum `yaml:"factum"`
	Worker ConfigWorker `yaml:"worker"`
}

var Config *ConfigRoot
