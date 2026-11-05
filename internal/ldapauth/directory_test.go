package ldapauth

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"
)

func TestStructuralClass(t *testing.T) {
	if got := StructuralClass([]string{"top", "organizationalUnit"}); got != "organizationalUnit" {
		t.Fatalf("ou class = %q", got)
	}
	if got := StructuralClass([]string{"top", "container"}); got != "container" {
		t.Fatalf("ad container class = %q", got)
	}
	if got := StructuralClass([]string{"top", "person", "organizationalPerson", "inetOrgPerson"}); got != "inetOrgPerson" {
		t.Fatalf("person class = %q", got)
	}
	if got := StructuralClass([]string{"domain", "dcObject"}); got != "domain" {
		t.Fatalf("domain class = %q", got)
	}
	if got := StructuralClass([]string{"top", "groupOfNames"}); got != "groupOfNames" {
		t.Fatalf("group class = %q", got)
	}
}

func TestEntryStringMail(t *testing.T) {
	e := &ldap.Entry{Attributes: []*ldap.EntryAttribute{
		{Name: "mail;lang-en", Values: []string{"ada@example.com"}},
		{Name: "cn", Values: []string{"Ada"}},
	}}
	if got := entryString(e, "mail"); got != "ada@example.com" {
		t.Fatalf("tagged mail = %q", got)
	}
	alias := &ldap.Entry{Attributes: []*ldap.EntryAttribute{
		{Name: "rfc822Mailbox", Values: []string{"ada@example.com"}},
	}}
	if got := entryString(alias, "AttrEmail", "mail"); got != "ada@example.com" {
		t.Fatalf("alias mail = %q", got)
	}
	if got := entryString(e, "userPrincipalName", "mail"); got != "ada@example.com" {
		t.Fatalf("fallback mail = %q", got)
	}
}

func TestMembershipFilter(t *testing.T) {
	got := membershipFilter("uid=ada,ou=people,dc=example,dc=com", "ada")
	if !strings.Contains(got, "(member=uid=ada,ou=people,dc=example,dc=com)") || !strings.Contains(got, "(memberUid=ada)") {
		t.Fatalf("filter = %q", got)
	}
	if membershipFilter("", "") != "" {
		t.Fatal("empty filter should be empty")
	}
}

func TestMailAttr(t *testing.T) {
	if mailAttr(Config{}) != "mail" || mailAttr(Config{AttrEmail: "rfc822Mailbox"}) != "mail" {
		t.Fatal("mail alias should write mail")
	}
	if mailAttr(Config{AttrEmail: "email"}) != "email" {
		t.Fatal("custom email attribute should be kept")
	}
}

func TestEntryKind(t *testing.T) {
	cases := []struct {
		name    string
		classes []string
		want    string
	}{
		{"inetOrgPerson", []string{"top", "person", "organizationalPerson", "inetOrgPerson"}, "person"},
		{"posixAccount", []string{"posixAccount", "top"}, "person"},
		{"AD user", []string{"top", "person", "organizationalPerson", "user"}, "person"},
		{"groupOfNames", []string{"top", "groupOfNames"}, "group"},
		{"posixGroup", []string{"top", "posixGroup"}, "group"},
		{"OU", []string{"top", "organizationalUnit"}, "container"},
		{"domain", []string{"top", "domain", "dcObject"}, "container"},
		{"device", []string{"top", "device"}, "other"},
		{"group wins over person", []string{"person", "groupOfNames"}, "group"},
	}
	for _, c := range cases {
		if got := EntryKind(c.classes); got != c.want {
			t.Errorf("%s: EntryKind = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDNScope(t *testing.T) {
	base := "dc=example,dc=com"
	if !DNInScope("DC=example,DC=com", base) {
		t.Fatal("base should be in scope")
	}
	if !DNBelowBase("uid=ada,ou=people,dc=example,dc=com", base) {
		t.Fatal("child should be below base")
	}
	if DNBelowBase(base, base) {
		t.Fatal("base is not strictly below itself")
	}
	if DNInScope("uid=ada,dc=other,dc=com", base) {
		t.Fatal("other suffix should be out of scope")
	}
	if DNInScope("", base) || DNBelowBase("uid=ada", "") {
		t.Fatal("empty DN or base is out of scope")
	}
}

func TestChildAndParentDN(t *testing.T) {
	dn, err := childDN("uid", "a,b", "ou=people,dc=example,dc=com")
	if err != nil {
		t.Fatal(err)
	}
	if dn != `uid=a\,b,ou=people,dc=example,dc=com` {
		t.Fatalf("child DN = %q", dn)
	}
	parent, err := parentDN(dn)
	if err != nil {
		t.Fatal(err)
	}
	if parent != "ou=people,dc=example,dc=com" {
		t.Fatalf("parent = %q", parent)
	}
	attr, val, err := rdnTypeValue(dn)
	if err != nil {
		t.Fatal(err)
	}
	if attr != "uid" || val != "a,b" {
		t.Fatalf("rdn = %s=%s", attr, val)
	}
}

func TestApplyAttrDiff(t *testing.T) {
	mod := ldap.NewModifyRequest("uid=ada,dc=example,dc=com", nil)
	changed := applyAttrDiff(mod, map[string]string{
		"mail": "old@example.com", "mobile": "", "cn": "Ada",
	}, map[string]string{
		"mail": "", "mobile": "+1", "cn": "Ada",
	})
	if !changed {
		t.Fatal("expected changes")
	}
	if len(mod.Changes) != 2 {
		t.Fatalf("changes = %#v", mod.Changes)
	}
}

func TestCleanMembers(t *testing.T) {
	base := "dc=example,dc=com"
	got, err := cleanMembers([]string{
		"uid=Ada,ou=people,dc=example,dc=com",
		"uid=ada,ou=people,dc=example,dc=com",
		"  ",
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("members = %#v", got)
	}
	_, err = cleanMembers([]string{"uid=ada,dc=other,dc=com"}, base)
	if err == nil {
		t.Fatal("expected out-of-scope member to fail")
	}
	_, err = cleanMembers([]string{"not a dn"}, base)
	if err == nil {
		t.Fatal("expected invalid DN to fail")
	}
}

func TestModifyDirectoryRefusesADAndMissingWriteback(t *testing.T) {
	cfg := Config{ServerType: "ad", Host: "127.0.0.1", Port: 1, BaseDN: "dc=example,dc=com"}
	err := modifyDirectory(cfg, WritebackConfig{BindDN: "cn=admin", Password: "x"}, func(*ldap.Conn) error {
		t.Fatal("write function must not run")
		return nil
	})
	if !errors.Is(err, ErrADReadOnly) {
		t.Fatalf("err = %v", err)
	}

	cfg.ServerType = "generic"
	err = modifyDirectory(cfg, WritebackConfig{}, func(*ldap.Conn) error {
		t.Fatal("write function must not run")
		return nil
	})
	// Missing write-back is reported before dial when the bind DN is empty,
	// but only after a successful dial. An empty bind is checked inside
	// bindWriteback, which runs after connectPrimary. Dial to port 1 fails
	// first, which is also a refusal to write.
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrADReadOnly) {
		t.Fatal("generic server must not be treated as AD")
	}
}
