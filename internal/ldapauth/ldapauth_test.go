package ldapauth

import (
	"net"
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
)

// TestConfigFromSettings_AttributeDefaults guards against the LDAP-provisioned
// user ending up with an empty Name/Email/Mobile: if an admin enables LDAP
// and fills in the connection details without also typing out every
// attribute name, ConfigFromSettings must still request the standard
// AD/OpenLDAP attribute names rather than leaving them unrequested.
func TestConfigFromSettings_AttributeDefaults(t *testing.T) {
	settings := &models.Settings{}
	cfg := ConfigFromSettings(settings)

	if cfg.AttrEmail != "mail" {
		t.Errorf("AttrEmail = %q, want default %q", cfg.AttrEmail, "mail")
	}
	if cfg.AttrDisplayName != "displayName" {
		t.Errorf("AttrDisplayName = %q, want default %q", cfg.AttrDisplayName, "displayName")
	}
	if cfg.AttrMobile != "mobile" {
		t.Errorf("AttrMobile = %q, want default %q", cfg.AttrMobile, "mobile")
	}
	if cfg.AttrGroups != "memberOf" {
		t.Errorf("AttrGroups = %q, want default %q", cfg.AttrGroups, "memberOf")
	}

	settings.LdapAttrEmail = "userPrincipalName"
	cfg = ConfigFromSettings(settings)
	if cfg.AttrEmail != "userPrincipalName" {
		t.Errorf("AttrEmail = %q, want explicit override %q", cfg.AttrEmail, "userPrincipalName")
	}

	settings.LdapHost = "dc1.example.com"
	settings.LdapPort = 636
	settings.LdapHost2 = "dc2.example.com"
	settings.LdapPort2 = 389
	cfg = ConfigFromSettings(settings)
	if cfg.Host != "dc1.example.com" || cfg.Port != 636 {
		t.Errorf("primary = %s:%d, want dc1.example.com:636", cfg.Host, cfg.Port)
	}
	if cfg.Host2 != "dc2.example.com" || cfg.Port2 != 389 {
		t.Errorf("secondary = %s:%d, want dc2.example.com:389", cfg.Host2, cfg.Port2)
	}
}

func TestConfiguredServers(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want []server
	}{
		{
			name: "primary only",
			cfg:  Config{Host: "dc1.example.com", Port: 389},
			want: []server{{host: "dc1.example.com", port: 389}},
		},
		{
			name: "primary plus secondary",
			cfg:  Config{Host: "dc1.example.com", Port: 389, Host2: "dc2.example.com"},
			want: []server{{host: "dc1.example.com", port: 389}, {host: "dc2.example.com", port: 389}},
		},
		{
			name: "secondary port inherited from primary when 0",
			cfg:  Config{Host: "dc1.example.com", Port: 636, Host2: "dc2.example.com"},
			want: []server{{host: "dc1.example.com", port: 636}, {host: "dc2.example.com", port: 636}},
		},
		{
			name: "explicit secondary port",
			cfg:  Config{Host: "dc1.example.com", Port: 389, Host2: "dc2.example.com", Port2: 1389},
			want: []server{{host: "dc1.example.com", port: 389}, {host: "dc2.example.com", port: 1389}},
		},
		{
			name: "blank secondary skipped",
			cfg:  Config{Host: "dc1.example.com", Port: 389, Host2: "  "},
			want: []server{{host: "dc1.example.com", port: 389}},
		},
		{
			name: "duplicate host+port skipped",
			cfg:  Config{Host: "dc1.example.com", Port: 389, Host2: "dc1.example.com"},
			want: []server{{host: "dc1.example.com", port: 389}},
		},
		{
			name: "scheme stripped on both",
			cfg:  Config{Host: "ldaps://dc1.example.com", Port: 636, Host2: "ldap://dc2.example.com/"},
			want: []server{{host: "dc1.example.com", port: 636}, {host: "dc2.example.com", port: 636}},
		},
		{
			name: "secondary only if primary blank",
			cfg:  Config{Host2: "dc2.example.com", Port2: 389},
			want: []server{{host: "dc2.example.com", port: 389}},
		},
		{
			name: "no hosts",
			cfg:  Config{},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := servers(c.cfg)
			if len(got) != len(c.want) {
				t.Fatalf("servers() = %#v, want %#v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("servers()[%d] = %#v, want %#v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestNormalizeDN(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"CN=Admins,OU=Groups,DC=Example,DC=Com", "cn=admins,ou=groups,dc=example,dc=com"},
		{"  cn=admins,dc=example,dc=com  ", "cn=admins,dc=example,dc=com"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeDN(c.in); got != c.want {
			t.Errorf("NormalizeDN(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRdnValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"CN=Admins,OU=Groups,DC=example,DC=com", "Admins"},
		{"OU=Groups,DC=example,DC=com", "Groups"},
		{"not a dn", "not a dn"},
		{"", ""},
	}
	for _, c := range cases {
		if got := rdnValue(c.in); got != c.want {
			t.Errorf("rdnValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsGroupEntry(t *testing.T) {
	cases := []struct {
		name    string
		classes []string
		want    bool
	}{
		{"AD group", []string{"top", "group"}, true},
		{"OpenLDAP groupOfNames", []string{"top", "groupOfNames"}, true},
		{"case-insensitive", []string{"GROUP"}, true},
		{"OU, not a group", []string{"top", "organizationalUnit"}, false},
		{"user", []string{"top", "person", "user"}, false},
		{"no classes", nil, false},
	}
	for _, c := range cases {
		if got := isGroupEntry(c.classes); got != c.want {
			t.Errorf("%s: isGroupEntry(%v) = %v, want %v", c.name, c.classes, got, c.want)
		}
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"dc1.ds.itn.nu", "dc1.ds.itn.nu"},
		{"ldaps://dc1.ds.itn.nu", "dc1.ds.itn.nu"},
		{"ldap://dc1.ds.itn.nu", "dc1.ds.itn.nu"},
		{"ldaps://dc1.ds.itn.nu/", "dc1.ds.itn.nu"},
		{"  ldaps://dc1.ds.itn.nu  ", "dc1.ds.itn.nu"},
	}
	for _, c := range cases {
		if got := normalizeHost(c.in); got != c.want {
			t.Errorf("normalizeHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// startTCPListener accepts connections and holds them open until the test
// ends, so go-ldap's DialURL (ldap://, no handshake beyond TCP) succeeds.
func startTCPListener(t *testing.T) (port uint16, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port = uint16(ln.Addr().(*net.TCPAddr).Port)
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-done
			}(conn)
		}
	}()
	return port, func() {
		close(done)
		ln.Close()
	}
}

func closedLocalPort(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	return port
}

func TestConnect_FallsBackToSecondary(t *testing.T) {
	port, stop := startTCPListener(t)
	defer stop()

	cfg := Config{
		Host:    "127.0.0.1",
		Port:    closedLocalPort(t),
		Host2:   "127.0.0.1",
		Port2:   port,
		TLSMode: "none",
	}
	conn, err := connect(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	conn.Close()
}

func TestConnect_PrimarySucceedsWithoutSecondary(t *testing.T) {
	port, stop := startTCPListener(t)
	defer stop()

	cfg := Config{
		Host:    "127.0.0.1",
		Port:    port,
		Host2:   "127.0.0.1",
		Port2:   closedLocalPort(t),
		TLSMode: "none",
	}
	conn, err := connect(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	conn.Close()
}

func TestConnect_AllServersUnreachable(t *testing.T) {
	cfg := Config{
		Host:    "127.0.0.1",
		Port:    closedLocalPort(t),
		Host2:   "127.0.0.1",
		Port2:   closedLocalPort(t),
		TLSMode: "none",
	}
	_, err := connect(cfg)
	if err == nil {
		t.Fatal("connect: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "all servers unreachable") {
		t.Errorf("error = %q, want it to mention both servers", err)
	}
}

func TestConnect_NoHost(t *testing.T) {
	_, err := connect(Config{})
	if err == nil || !strings.Contains(err.Error(), "no host configured") {
		t.Errorf("connect(empty) = %v, want no host configured", err)
	}
}

func TestTestServers_Empty(t *testing.T) {
	if got := TestServers(Config{}); len(got) != 0 {
		t.Errorf("TestServers(empty) = %#v, want empty", got)
	}
}

func TestTestServers_Independent(t *testing.T) {
	cfg := Config{
		Host:    "127.0.0.1",
		Port:    closedLocalPort(t),
		Host2:   "127.0.0.1",
		Port2:   closedLocalPort(t),
		TLSMode: "none",
		BaseDN:  "dc=example,dc=com",
	}
	results := TestServers(cfg)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Error == nil {
			t.Errorf("%s:%d: expected error, got nil", r.Host, r.Port)
		}
	}
}
