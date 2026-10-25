package ldapsrv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"
	"golang.org/x/crypto/bcrypt"
)

func TestSchemaDescriptionsClosed(t *testing.T) {
	attrs := schemaNames(t, attributeTypes)
	classes := schemaNames(t, objectClasses)
	rules := schemaNames(t, matchingRules)
	syntaxes := schemaOIDs(t, ldapSyntaxes)

	for _, desc := range attributeTypes {
		for _, name := range schemaRefs(desc, "SUP") {
			if !attrs[name] {
				t.Errorf("%s SUP %s is not an attribute type", desc, name)
			}
		}
		for _, key := range []string{"EQUALITY", "SUBSTR"} {
			for _, name := range schemaRefs(desc, key) {
				if !rules[name] {
					t.Errorf("%s %s %s is not a matching rule", desc, key, name)
				}
			}
		}
		for _, oid := range syntaxOIDs(desc) {
			if !syntaxes[oid] {
				t.Errorf("%s SYNTAX %s is not a syntax", desc, oid)
			}
		}
	}
	for _, desc := range matchingRules {
		for _, oid := range syntaxOIDs(desc) {
			if !syntaxes[oid] {
				t.Errorf("%s SYNTAX %s is not a syntax", desc, oid)
			}
		}
	}
	for _, desc := range objectClasses {
		for _, name := range schemaRefs(desc, "SUP") {
			if !classes[name] {
				t.Errorf("%s SUP %s is not an object class", desc, name)
			}
		}
		for _, key := range []string{"MUST", "MAY"} {
			for _, name := range schemaRefs(desc, key) {
				if !attrs[name] {
					t.Errorf("%s %s %s is not an attribute type", desc, key, name)
				}
			}
		}
	}
	for _, want := range []string{
		"top", "person", "organizationalperson", "inetorgperson",
		"groupofnames", "group", "organizationalunit", "organization",
		"domain", "dcobject", "subschema", "openldaprootdse",
	} {
		if !classes[want] {
			t.Errorf("schema is missing object class %s", want)
		}
	}
}

func TestPublishedEntriesUseSchema(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	ou := uint(1)
	dir := Build(Input{
		BaseDN: "dc=example,dc=com",
		OUs:    []OU{{ID: ou, Name: "NOC"}},
		Roles:  []Role{{ID: 1, Name: "staff", Description: "ops"}},
		Users: []User{{
			Username: "ada",
			Name:     "Ada Lovelace",
			Email:    "ada@example.com",
			Mobile:   "+461",
			Password: string(hash),
			OUID:     &ou,
			RoleIDs:  []uint{1},
		}},
	})
	classes := schemaNames(t, objectClasses)
	attrs := schemaNames(t, attributeTypes)
	for _, e := range dir.entries {
		for _, oc := range e.Attrs["objectclass"] {
			if !classes[strings.ToLower(oc)] {
				t.Errorf("%s objectClass %s is not in the schema", e.DN, oc)
			}
		}
		for name := range e.Attrs {
			if name == "objectclass" {
				continue
			}
			if !attrs[name] {
				t.Errorf("%s attribute %s is not in the schema", e.DN, name)
			}
		}
	}
}

func TestSubschemaSearch(t *testing.T) {
	dir := Build(Input{BaseDN: "dc=example,dc=com"})
	addr := freeAddr(t)
	body, err := json.Marshal(dir.Snapshot(true, addr, "", "bind-secret"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Options{Fetch: func(context.Context) ([]byte, error) { return body, nil }})

	conn := dialLDAP(t, addr)
	defer conn.Close()
	if err := conn.UnauthenticatedBind(""); err != nil {
		t.Fatal(err)
	}
	root, err := conn.Search(ldap.NewSearchRequest(
		"", ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=*)", []string{"subschemaSubentry", "namingContexts"}, nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Entries) != 1 || root.Entries[0].GetEqualFoldAttributeValue("subschemaSubentry") != subschemaDN {
		t.Fatalf("root DSE subschemaSubentry = %q", root.Entries[0].GetEqualFoldAttributeValue("subschemaSubentry"))
	}

	schema, err := conn.Search(ldap.NewSearchRequest(
		subschemaDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=subschema)",
		[]string{"attributeTypes", "objectClasses", "ldapSyntaxes", "matchingRules"}, nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Entries) != 1 {
		t.Fatalf("schema entries = %d", len(schema.Entries))
	}
	got := schema.Entries[0]
	if n := len(got.GetEqualFoldAttributeValues("attributeTypes")); n != len(attributeTypes) {
		t.Fatalf("attributeTypes = %d, want %d", n, len(attributeTypes))
	}
	if n := len(got.GetEqualFoldAttributeValues("objectClasses")); n != len(objectClasses) {
		t.Fatalf("objectClasses = %d, want %d", n, len(objectClasses))
	}
	if !hasSchemaValue(got, "objectClasses", "inetOrgPerson") || !hasSchemaValue(got, "objectClasses", "'group'") {
		t.Fatalf("objectClasses missing inetOrgPerson or group: %v", got.GetEqualFoldAttributeValues("objectClasses"))
	}

	_, err = conn.Search(ldap.NewSearchRequest(
		"dc=example,dc=com", ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=*)", []string{"dc"}, nil,
	))
	if ldapCode(err) != ldap.LDAPResultInsufficientAccessRights {
		t.Fatalf("anonymous base search code = %v, err = %v", ldapCode(err), err)
	}

	if err := conn.Bind(dir.BindDN, "bind-secret"); err != nil {
		t.Fatal(err)
	}
	tree, err := conn.Search(ldap.NewSearchRequest(
		"dc=example,dc=com", ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=*)", []string{"dn"}, nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range tree.Entries {
		if normalizeDN(e.DN) == normalizeDN(subschemaDN) {
			t.Fatal("subschema was returned under the naming context")
		}
	}
}

func hasSchemaValue(e *ldap.Entry, attr, needle string) bool {
	for _, v := range e.GetEqualFoldAttributeValues(attr) {
		if strings.Contains(v, needle) {
			return true
		}
	}
	return false
}

func ldapCode(err error) uint16 {
	if err == nil {
		return ldap.LDAPResultSuccess
	}
	le, ok := err.(*ldap.Error)
	if !ok {
		return 0
	}
	return uint16(le.ResultCode)
}

func schemaNames(t *testing.T, descs []string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, desc := range descs {
		if !balanced(desc) {
			t.Errorf("unbalanced description: %s", desc)
		}
		names := namesIn(desc)
		if len(names) == 0 {
			t.Errorf("description has no NAME: %s", desc)
		}
		for _, n := range names {
			out[strings.ToLower(n)] = true
		}
	}
	return out
}

func schemaOIDs(t *testing.T, descs []string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, desc := range descs {
		if !balanced(desc) {
			t.Errorf("unbalanced description: %s", desc)
		}
		oid, _, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(desc, "(")), " ")
		oid = strings.TrimSpace(oid)
		if !ok || oid == "" || oid[0] < '0' || oid[0] > '9' {
			t.Errorf("description has no OID: %s", desc)
			continue
		}
		out[oid] = true
	}
	return out
}

func namesIn(desc string) []string {
	const key = "NAME "
	i := strings.Index(desc, key)
	if i < 0 {
		return nil
	}
	rest := strings.TrimSpace(desc[i+len(key):])
	if strings.HasPrefix(rest, "(") {
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return nil
		}
		var out []string
		for _, part := range strings.Fields(rest[1:end]) {
			out = append(out, strings.Trim(part, "'"))
		}
		return out
	}
	if strings.HasPrefix(rest, "'") {
		end := strings.IndexByte(rest[1:], '\'')
		if end < 0 {
			return nil
		}
		return []string{rest[1 : 1+end]}
	}
	return nil
}

func schemaRefs(desc, key string) []string {
	token := key + " "
	i := strings.Index(desc, token)
	if i < 0 {
		return nil
	}
	rest := strings.TrimSpace(desc[i+len(token):])
	if strings.HasPrefix(rest, "(") {
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return nil
		}
		var out []string
		for _, part := range strings.Split(rest[1:end], "$") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, strings.ToLower(part))
			}
		}
		return out
	}
	field := rest
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		field = rest[:sp]
	}
	field = strings.TrimSuffix(field, ")")
	if field == "" {
		return nil
	}
	return []string{strings.ToLower(field)}
}

func syntaxOIDs(desc string) []string {
	const key = "SYNTAX "
	i := strings.Index(desc, key)
	if i < 0 {
		return nil
	}
	rest := strings.TrimSpace(desc[i+len(key):])
	end := len(rest)
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		end = sp
	}
	oid := strings.TrimSuffix(rest[:end], ")")
	if oid == "" {
		return nil
	}
	return []string{oid}
}

func balanced(s string) bool {
	n := 0
	for _, r := range s {
		switch r {
		case '(':
			n++
		case ')':
			n--
			if n < 0 {
				return false
			}
		}
	}
	return n == 0
}
