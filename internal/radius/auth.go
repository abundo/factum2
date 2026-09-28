package radius

import (
	"net"
	"strings"
	"unicode"

	"github.com/abundo/factum2/internal/ldapauth"
)

const (
	// Role is the worker.commands name that starts the UDP listener and
	// may fetch /api/radius-config.
	Role = "radius"

	DefaultListen = ":1812"

	resultAccept = "accept"
	resultReject = "reject"
	resultDrop   = "drop"

	reasonUnknownClient  = "unknown client"
	reasonBadAuth        = "bad message-authenticator"
	reasonNoPassword     = "password authentication required"
	reasonMSCHAPDir      = "MS-CHAPv2 requires Active Directory"
	reasonMSCHAPConfig   = "MS-CHAPv2 is not configured"
	reasonNotInDirectory = "user not found in directory"
	reasonBadUser        = "bad credentials"
	reasonDirectory      = "directory unavailable"
	reasonUnknownDevice  = "unknown device"
	reasonDeviceDisabled = "device disabled"
	reasonNASMismatch    = "nas address does not match the client"
	reasonNotAllowed     = "not allowed for this device role"
	reasonOK             = "ok"
)

// Client is one NAS (router or switch) allowed to send Access-Request.
type Client struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Secret  string `json:"secret"`
}

// Policy maps one LDAP group to NetBox device roles. AllDevices permits
// every role, including a device whose role is blank.
type Policy struct {
	GroupDN    string   `json:"group_dn"`
	AllDevices bool     `json:"all_devices"`
	Roles      []string `json:"roles"`
}

// Device is one NetBox device the worker can name from an address.
type Device struct {
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Enabled   bool     `json:"enabled"`
	Addresses []string `json:"addresses"`
}

// Config is the snapshot a worker persists and serves from. LDAP is the
// directory the worker binds to; it is not read at request time from factum2.
type Config struct {
	Enabled  bool            `json:"enabled"`
	Listen   string          `json:"listen"`
	LDAP     ldapauth.Config `json:"ldap"`
	Clients  []Client        `json:"clients"`
	Policies []Policy        `json:"policies"`
	Devices  []Device        `json:"devices"`
	// Reply is the Access-Accept attribute text. Empty sends none.
	Reply string `json:"reply"`
	// Machine is the AD computer account used to check MS-CHAPv2.
	Machine Machine `json:"machine"`
}

// Event is one login attempt safe to store. Password is never included.
type Event struct {
	ReportedAt string `json:"reported_at"`
	Username   string `json:"username,omitempty"`
	NASIP      string `json:"nas_ip,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	DeviceRole string `json:"device_role,omitempty"`
	Result     string `json:"result"`
	Reason     string `json:"reason"`
	Worker     string `json:"worker,omitempty"`
}

// Index is the lookup form of Config. Ambiguous addresses (two devices, or
// two clients) are omitted so a login against them fails closed.
type Index struct {
	Enabled  bool
	Listen   string
	LDAP     ldapauth.Config
	clients  map[string]Client
	devices  map[string]Device
	policies []Policy
	reply    []byte
	machine  Machine
}

// BuildIndex indexes clients and devices by canonical IP.
func BuildIndex(cfg Config) *Index {
	idx := &Index{
		Enabled:  cfg.Enabled,
		Listen:   cfg.Listen,
		LDAP:     cfg.LDAP,
		clients:  map[string]Client{},
		devices:  map[string]Device{},
		policies: cfg.Policies,
		machine:  cfg.Machine,
	}
	if idx.Listen == "" {
		idx.Listen = DefaultListen
	}
	if raw, err := EncodeReply(cfg.Reply); err == nil {
		idx.reply = raw
	}
	clientSecrets := map[string]map[string]Client{}
	for _, c := range cfg.Clients {
		ip := canonIP(c.Address)
		if ip == "" || c.Secret == "" {
			continue
		}
		c.Address = ip
		if clientSecrets[ip] == nil {
			clientSecrets[ip] = map[string]Client{}
		}
		clientSecrets[ip][c.Secret] = c
	}
	for ip, secrets := range clientSecrets {
		if len(secrets) == 1 {
			for _, c := range secrets {
				idx.clients[ip] = c
			}
		}
	}
	owners := map[string]map[string]Device{}
	for _, d := range cfg.Devices {
		for _, addr := range d.Addresses {
			ip := canonIP(addr)
			if ip == "" {
				continue
			}
			if owners[ip] == nil {
				owners[ip] = map[string]Device{}
			}
			owners[ip][d.Name] = d
		}
	}
	for ip, devs := range owners {
		if len(devs) == 1 {
			for _, d := range devs {
				idx.devices[ip] = d
			}
		}
	}
	return idx
}

func canonIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func normRole(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// outcome is the decision for one request.
type outcome struct {
	code       byte // 0 = no reply
	reason     string
	result     string
	username   string
	nasIP      string
	deviceName string
	deviceRole string
	record     bool
	secret     string
	req        Packet
	extra      []byte
}

// login is the credential material taken from a verified Access-Request.
type login struct {
	username string
	password string
	nas      string
	chap     *mschap2
}

// gate authenticates the packet itself: known client, Message-Authenticator,
// and a PAP password. A failed gate does not trust the User-Name.
func gate(raw []byte, sourceIP string, idx *Index) (in login, out outcome, ok bool) {
	out.result = resultDrop
	out.record = true
	out.nasIP = canonIP(sourceIP)
	p, attrs, err := parse(raw)
	if err != nil || p.Code != codeAccessRequest {
		out.record = false
		return login{}, out, false
	}
	out.req = p
	client, found := idx.clients[canonIP(sourceIP)]
	if !found {
		out.reason = reasonUnknownClient
		return login{}, out, false
	}
	out.secret = client.Secret
	if err := verifyMessageAuthenticator(p.Raw, []byte(client.Secret)); err != nil {
		out.reason = reasonBadAuth
		return login{}, out, false
	}
	userRaw := attrValue(attrs, attrUserName)
	username := strings.TrimSpace(string(userRaw))
	if username != "" && len(username) <= 255 && strings.IndexFunc(username, unicode.IsControl) < 0 {
		out.username = username
	}
	in.username = out.username
	in.nas = nasIP(attrs)
	if chap, ok := parseMSCHAP2(attrs, p.Auth[:]); ok {
		chap.user = samAccount(username)
		if chap.user == "" {
			out.code = codeAccessReject
			out.result = resultReject
			out.reason = reasonBadUser
			return login{}, out, false
		}
		in.chap = &chap
		return in, outcome{}, true
	}
	passAttr := attrValue(attrs, attrUserPassword)
	if passAttr == nil {
		out.code = codeAccessReject
		out.result = resultReject
		out.reason = reasonNoPassword
		return login{}, out, false
	}
	password, err := decryptUserPassword(passAttr, []byte(client.Secret), p.Auth[:])
	if err != nil || password == "" || strings.IndexFunc(password, func(r rune) bool { return r == 0 || unicode.IsControl(r) }) >= 0 {
		out.code = codeAccessReject
		out.result = resultReject
		out.reason = reasonBadUser
		return login{}, out, false
	}
	if out.username == "" {
		out.code = codeAccessReject
		out.result = resultReject
		out.reason = reasonBadUser
		return login{}, out, false
	}
	in.password = password
	return in, outcome{}, true
}

// authorize applies the device-role policy after the directory check.
// groups are memberOf DNs. ldapErr means the directory could not be asked;
// ldapOK false with a nil error is a bad password or unknown user.
func authorize(username, nas, sourceIP string, idx *Index, groups []string, ldapOK bool, ldapErr error) outcome {
	out := outcome{
		result:   resultReject,
		code:     codeAccessReject,
		username: username,
		nasIP:    canonIP(sourceIP),
		record:   true,
	}
	dev, reason, ok := matchDevice(idx, nas, sourceIP)
	if !ok {
		out.reason = reason
		return out
	}
	out.deviceName = dev.Name
	out.deviceRole = dev.Role
	if !dev.Enabled {
		out.reason = reasonDeviceDisabled
		return out
	}
	if ldapErr != nil {
		out.reason = reasonDirectory
		return out
	}
	if !ldapOK {
		out.reason = reasonBadUser
		return out
	}
	if !groupAllows(groups, dev.Role, idx.policies) {
		out.reason = reasonNotAllowed
		return out
	}
	out.code = codeAccessAccept
	out.result = resultAccept
	out.reason = reasonOK
	return out
}

func matchDevice(idx *Index, nas, sourceIP string) (Device, string, bool) {
	source := canonIP(sourceIP)
	nasCanon := canonIP(nas)
	sourceDev, sourceOK := idx.devices[source]
	var nasDev Device
	nasOK := false
	if nasCanon != "" {
		nasDev, nasOK = idx.devices[nasCanon]
	}
	if nasCanon != "" {
		if !nasOK {
			return Device{}, reasonUnknownDevice, false
		}
		if sourceOK && nasDev.Name != sourceDev.Name {
			return Device{}, reasonNASMismatch, false
		}
		return nasDev, "", true
	}
	if sourceOK {
		return sourceDev, "", true
	}
	return Device{}, reasonUnknownDevice, false
}

func groupAllows(groups []string, role string, policies []Policy) bool {
	have := map[string]bool{}
	for _, g := range groups {
		have[ldapauth.NormalizeDN(g)] = true
	}
	want := normRole(role)
	for _, p := range policies {
		if !have[ldapauth.NormalizeDN(p.GroupDN)] {
			continue
		}
		if p.AllDevices {
			return true
		}
		for _, r := range p.Roles {
			if normRole(r) == want && want != "" {
				return true
			}
		}
	}
	return false
}

func replyMessage(reason string) string {
	switch reason {
	case reasonDirectory:
		return "Directory unavailable"
	case reasonUnknownDevice:
		return "Unknown device"
	case reasonDeviceDisabled:
		return "Device disabled"
	case reasonNoPassword:
		return "Password authentication required"
	case reasonMSCHAPDir:
		return "MS-CHAPv2 requires Active Directory"
	case reasonMSCHAPConfig:
		return "MS-CHAPv2 is not configured"
	case reasonOK:
		return ""
	default:
		return "Access denied"
	}
}
