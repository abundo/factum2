package drivers

// Hierarchical (indentation-based) config parsing, à la
// https://github.com/aerogo/codetree/blob/master/CodeTree.go

// https://github.com/nleiva/xrgrpc?tab=readme-ov-file

import (
	"errors"
	"strings"

	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/netboxtool"
)

// validateDriverParam checks the credentials every driver constructor
// requires, so each NewXxxDriver doesn't repeat the same two checks.
func validateDriverParam(p DriverParam) error {
	if p.Username == "" {
		return errors.New("missing username")
	}
	if p.Password == "" {
		return errors.New("missing password")
	}
	return nil
}

// setInterfaceDescription is every driver's SetInterfaceDescription: wrap
// the single interface into a one-element SetInterfaceDescriptions call.
func setInterfaceDescription(setMany func([]string, []*netboxtool.NBInterface) error, intf *netboxtool.NBInterface) error {
	return setMany([]string{intf.Name}, []*netboxtool.NBInterface{intf})
}

// checkNamesMatchInterfaces validates that a SetInterfaceDescriptions call's
// two parallel slices have the same length, which every driver requires
// before turning them into per-interface edits.
func checkNamesMatchInterfaces(name []string, intf []*netboxtool.NBInterface) error {
	if len(name) != len(intf) {
		return errors.New("number of interface names and descriptions must be same")
	}
	return nil
}

// checkNamesMatchVLANConfigs is checkNamesMatchInterfaces' counterpart for
// SetInterfaceVLANs.
func checkNamesMatchVLANConfigs(name []string, params []*VLANConfig) error {
	if len(name) != len(params) {
		return errors.New("number of interface names and vlan configs must be same")
	}
	return nil
}

type DriverClient interface {
	Exec(cmd string) (*ExecModel, error)
	RunningConfigGet(json bool) (*RunningConfigModel, error)
	RunningConfigSave() error
	GetInterfacesStatus() ([]*netboxtool.NBInterface, error)
	SetInterfaceDescription(intf *netboxtool.NBInterface) error
	SetInterfaceDescriptions(name []string, intf []*netboxtool.NBInterface) error
	// SetInterfaceVLANs pushes switchport/VLAN config to a set of interfaces
	// - only implemented for global-VLAN platforms (EOS, VRP, Cisco SMB);
	// every other platform returns an error, since they have no per-interface
	// global VLAN concept (see Interface.SwitchportMode's doc comment).
	SetInterfaceVLANs(name []string, params []*VLANConfig) error
	Version() (*VersionModel, error)

	// GetDeviceConfig fetches and parses the device's running config into
	// interfaces/VRFs/pseudowires/ELINE/ELAN/L3VPN, for internal/device-sync
	// to compare against Netbox.
	GetDeviceConfig() (*DeviceConfig, error)
	// GetNeighbors returns the device's LLDP-discovered neighbors.
	GetNeighbors() ([]*Neighbor, error)
}

// DriverFactory builds a DriverClient for one platform.
type DriverFactory func(DriverParam) (DriverClient, error)

// SupportedPlatforms are the Netbox platform names (lower-cased) NewDriver
// can build a driver for, exported so callers like internal/device-sync can
// filter devices by platform up front rather than failing per-device. Each
// driver registers itself here via registerDriver in its own file's init(),
// so adding a platform never requires touching this file.
var SupportedPlatforms = map[string]DriverFactory{}

// registerDriver adds a platform's factory to SupportedPlatforms. Called
// from each driver's init(); panics on a duplicate platform since that can
// only mean a copy-paste mistake at build time, never a runtime condition.
func registerDriver(platform string, factory DriverFactory) {
	if _, exists := SupportedPlatforms[platform]; exists {
		panic("drivers: duplicate registration for platform " + platform)
	}
	SupportedPlatforms[platform] = factory
}

type MyDevice struct {
	Id           int
	Name         string
	Manufacturer string
	DeviceType   string
	Platform     string
}

// Parameters needed to create a device instance
type DriverParam struct {
	Name     string // name on device
	Port     string
	Username string
	Password string
	Platform string
}

// DeviceFQDN returns name as-is if it already looks like an FQDN (contains a
// dot); otherwise it appends defaultDomain, since drivers dial the device
// over the network and need a resolvable name while factum/Netbox device
// names are often stored as short hostnames.
func DeviceFQDN(name string, defaultDomain string) string {
	if strings.Contains(name, ".") || defaultDomain == "" {
		return name
	}
	return name + "." + defaultDomain
}

// commitCommentMax is a conservative length for `commit comment ...` text.
// Nokia MD-CLI comments are historically 80 characters; EOS and XR allow
// more, so 80 is the common floor.
const commitCommentMax = 80

// sanitizeCommitComment collapses whitespace and strips quotes/control
// characters that would break `commit comment "..."` (EOS/SR OS) or
// unquoted `commit comment ...` (IOS-XR). Empty after sanitizing means
// the caller should emit a bare "commit".
func sanitizeCommitComment(comment string) string {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(comment))
	for _, r := range comment {
		switch {
		case r == '"' || r == '\'' || r == '\\' || r == ';' || r == '|':
			b.WriteByte(' ')
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > commitCommentMax {
		out = strings.TrimSpace(out[:commitCommentMax])
	}
	return out
}

// CommitCLI is the last command of a candidate-session apply. Platforms
// that support a commit comment (EOS, SR OS, IOS-XR) get
// `commit comment "..."` (quoted=true) or `commit comment ...`
// (quoted=false, XR) when comment is non-empty; otherwise a bare "commit".
func CommitCLI(comment string, quoted bool) string {
	comment = sanitizeCommitComment(comment)
	if comment == "" {
		return "commit"
	}
	if quoted {
		return `commit comment "` + comment + `"`
	}
	return "commit comment " + comment
}

// NewDriverName builds a driver for the device called name, looking up
// everything it needs over the primary's REST API rather than from Postgres:
// the device itself (GET /api/device/name/:name, for its platform) and
// util.CommonConfig (GET /api/common-config, for the default domain used to
// turn a short device name into something resolvable). That's what lets
// factum2-driver-cli run on a host that can reach the devices but has no
// database access - the same remote-config pattern the DNS/Icinga/LibreNMS/
// Oxidized tools use, see internal/util.FetchRemoteConfig.
func NewDriverName(factumConfig *util.ConfigFactum, name string, username string, password string) (DriverClient, error) {
	device, err := factum.NewFactumClient(factumConfig).GetDeviceByName(name)
	if err != nil {
		return nil, err
	}
	common, err := util.FetchRemoteConfig[util.CommonConfig](factumConfig, "/api/common-config")
	if err != nil {
		return nil, err
	}
	p := DriverParam{
		Name:     DeviceFQDN(device.Name, common.DefaultDomain),
		Platform: strings.ToLower(device.Platform),
		Username: username,
		Password: password,
	}
	return NewDriver(p)
}

func NewDriver(deviceParam DriverParam) (DriverClient, error) {
	factory, ok := SupportedPlatforms[deviceParam.Platform]
	if !ok {
		return nil, errors.New("Unknown platform " + deviceParam.Platform)
	}
	return factory(deviceParam)
}
