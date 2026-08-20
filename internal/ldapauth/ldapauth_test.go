package ldapauth

import (
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
