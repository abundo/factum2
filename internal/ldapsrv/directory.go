package ldapsrv

import (
	"strings"
	"unicode/utf8"
)

const (
	// DefaultBaseDN is used when the internal directory is enabled and no
	// base DN has been set.
	DefaultBaseDN = "dc=factum,dc=local"
	// DefaultListen is the plain LDAP listen address.
	DefaultListen = ":389"
	// ServiceCN is the RDN of the search bind account. It is not a person.
	ServiceCN = "svc-factum"
	// MatchingRuleInChain is Active Directory's LDAP_MATCHING_RULE_IN_CHAIN.
	// A filter (member:1.2.840.113556.1.4.1941:=dn) matches a group when dn
	// is a direct or nested member. memberOf walks the other way.
	MatchingRuleInChain = "1.2.840.113556.1.4.1941"

	codeSuccess                = 0
	codeProtocolError          = 2
	codeSizeLimit              = 4
	codeAuthMethodNotSupported = 7
	codeUnavailableCritical    = 12
	codeNoSuchObject           = 32
	codeInvalidCredentials     = 49
	codeInsufficientAccess     = 50
	codeUnwillingToPerform     = 53
)

// OU is one folder. ID 0 is not used. ParentID nil is the base DN.
type OU struct {
	ID       uint
	ParentID *uint
	Name     string
}

// User is a person published in the directory. Password is a bcrypt hash.
// Accounts without a bcrypt hash are left out. ID is the factum user id
// and is not part of the DN.
type User struct {
	ID       uint
	Username string
	Name     string
	Email    string
	Mobile   string
	Password string
	OUID     *uint
	RoleIDs  []uint
}

// Role is a group. MemberIDs are roles that are direct members of this role.
type Role struct {
	ID          uint
	Name        string
	Description string
	OUID        *uint
	MemberIDs   []uint
}

// Input is the directory the hub snapshot is built from.
type Input struct {
	BaseDN string
	OUs    []OU
	Users  []User
	Roles  []Role
}

// Entry is one object served over LDAP. Password is checked on bind and
// is never returned as an attribute. DirectMembers and DirectMemberOf are
// the unexpanded edges used by the Active Directory in-chain rule.
// memberOf in Attrs is already expanded, so a plain equality filter sees
// nested groups.
type Entry struct {
	DN             string
	Attrs          map[string][]string
	Password       string
	DirectMembers  []string
	DirectMemberOf []string
	operational    map[string]bool
}

// Directory is an immutable snapshot.
type Directory struct {
	BaseDN       string
	BindDN       string
	BindPassword string
	// RoleDN maps a role id to the DN it was published at. OUDN does the
	// same for folders. Both are empty for a snapshot loaded from disk.
	RoleDN  map[uint]string
	OUDN    map[uint]string
	entries []*Entry
	byDN    map[string]*Entry
}

// ServiceBindDN is the search account for BaseDN.
func ServiceBindDN(base string) string {
	return "cn=" + escapeDN(ServiceCN) + "," + strings.TrimSpace(base)
}

// Build turns people, roles, and folders into LDAP entries.
func Build(in Input) *Directory {
	base := strings.TrimSpace(in.BaseDN)
	if base == "" {
		base = DefaultBaseDN
	}
	d := &Directory{
		BaseDN: base,
		BindDN: ServiceBindDN(base),
		RoleDN: map[uint]string{},
		OUDN:   map[uint]string{},
		byDN:   map[string]*Entry{},
	}
	d.add(baseEntry(base))

	byID := map[uint]OU{}
	for _, ou := range in.OUs {
		if ou.ID == 0 || strings.TrimSpace(ou.Name) == "" {
			continue
		}
		byID[ou.ID] = ou
	}
	ouDN := map[uint]string{}
	var walk func(id uint, seen map[uint]bool) string
	walk = func(id uint, seen map[uint]bool) string {
		if id == 0 || seen[id] {
			return ""
		}
		if dn, ok := ouDN[id]; ok {
			return dn
		}
		ou, ok := byID[id]
		if !ok {
			return ""
		}
		seen[id] = true
		parent := base
		if ou.ParentID != nil {
			if p := walk(*ou.ParentID, seen); p != "" {
				parent = p
			}
		}
		dn := "ou=" + escapeDN(strings.TrimSpace(ou.Name)) + "," + parent
		ouDN[id] = dn
		d.OUDN[id] = dn
		return dn
	}
	for id := range byID {
		walk(id, map[uint]bool{})
	}
	// Parents first, so a tree walk of the snapshot is stable.
	ids := make([]uint, 0, len(ouDN))
	for id := range ouDN {
		ids = append(ids, id)
	}
	sortUints(ids)
	for _, id := range ids {
		ou := byID[id]
		d.add(ouEntry(ouDN[id], strings.TrimSpace(ou.Name)))
	}

	people := "ou=People," + base
	rolesOU := "ou=Roles," + base
	if d.byDN[normalizeDN(people)] == nil {
		d.add(ouEntry(people, "People"))
	}
	if d.byDN[normalizeDN(rolesOU)] == nil {
		d.add(ouEntry(rolesOU, "Roles"))
	}

	roleDN := map[uint]string{}
	var roles []Role
	for _, role := range in.Roles {
		name := strings.TrimSpace(role.Name)
		if role.ID == 0 || name == "" {
			continue
		}
		parent := rolesOU
		if role.OUID != nil {
			if dn := ouDN[*role.OUID]; dn != "" {
				parent = dn
			}
		}
		roleDN[role.ID] = "cn=" + escapeDN(name) + "," + parent
		d.RoleDN[role.ID] = roleDN[role.ID]
		role.Name = name
		roles = append(roles, role)
	}

	type pubUser struct {
		User
		dn string
	}
	var users []pubUser
	for _, u := range in.Users {
		if !strings.HasPrefix(u.Password, "$2") {
			continue
		}
		name := strings.TrimSpace(u.Username)
		if name == "" {
			continue
		}
		parent := people
		if u.OUID != nil {
			if dn := ouDN[*u.OUID]; dn != "" {
				parent = dn
			}
		}
		u.Username = name
		users = append(users, pubUser{User: u, dn: "uid=" + escapeDN(name) + "," + parent})
	}

	parents := map[uint][]uint{}
	children := map[uint][]uint{}
	for _, role := range roles {
		for _, member := range role.MemberIDs {
			if member == role.ID || roleDN[member] == "" {
				continue
			}
			children[role.ID] = append(children[role.ID], member)
			parents[member] = append(parents[member], role.ID)
		}
	}
	var ancestors func(id uint, seen map[uint]bool) []uint
	ancestors = func(id uint, seen map[uint]bool) []uint {
		var out []uint
		for _, p := range parents[id] {
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
			out = append(out, ancestors(p, seen)...)
		}
		return out
	}

	userByRole := map[uint][]string{}
	for _, u := range users {
		for _, id := range u.RoleIDs {
			if roleDN[id] == "" {
				continue
			}
			userByRole[id] = append(userByRole[id], u.dn)
		}
	}

	for _, u := range users {
		cn, sn := splitName(u.Name, u.Username)
		attrs := map[string][]string{
			"objectclass": {"top", "person", "organizationalPerson", "inetOrgPerson"},
			"uid":         {u.Username},
			"cn":          {cn},
			"sn":          {sn},
			"displayname": {cn},
		}
		if u.Email != "" {
			attrs["mail"] = []string{u.Email}
		}
		if u.Mobile != "" {
			attrs["mobile"] = []string{u.Mobile}
		}
		seen := map[uint]bool{}
		var memberOf []string
		var direct []string
		for _, id := range u.RoleIDs {
			if roleDN[id] == "" || seen[id] {
				continue
			}
			seen[id] = true
			direct = append(direct, roleDN[id])
			memberOf = append(memberOf, roleDN[id])
			for _, p := range ancestors(id, seen) {
				memberOf = append(memberOf, roleDN[p])
			}
		}
		if len(memberOf) > 0 {
			attrs["memberof"] = memberOf
		}
		d.add(&Entry{
			DN:             u.dn,
			Attrs:          attrs,
			Password:       u.Password,
			DirectMemberOf: direct,
			operational:    map[string]bool{"memberof": true},
		})
	}

	for _, role := range roles {
		attrs := map[string][]string{
			"objectclass": {"top", "groupOfNames", "group"},
			"cn":          {role.Name},
		}
		if role.Description != "" {
			attrs["description"] = []string{role.Description}
		}
		var members []string
		seenM := map[string]bool{}
		addMember := func(dn string) {
			key := normalizeDN(dn)
			if dn == "" || seenM[key] {
				return
			}
			seenM[key] = true
			members = append(members, dn)
		}
		for _, dn := range userByRole[role.ID] {
			addMember(dn)
		}
		for _, id := range children[role.ID] {
			addMember(roleDN[id])
		}
		if len(members) > 0 {
			attrs["member"] = members
		}
		seen := map[uint]bool{role.ID: true}
		var memberOf []string
		var direct []string
		for _, p := range parents[role.ID] {
			if seen[p] {
				continue
			}
			seen[p] = true
			direct = append(direct, roleDN[p])
			memberOf = append(memberOf, roleDN[p])
			for _, a := range ancestors(p, seen) {
				memberOf = append(memberOf, roleDN[a])
			}
		}
		if len(memberOf) > 0 {
			attrs["memberof"] = memberOf
		}
		d.add(&Entry{
			DN:             roleDN[role.ID],
			Attrs:          attrs,
			DirectMembers:  members,
			DirectMemberOf: direct,
			operational:    map[string]bool{"memberof": true},
		})
	}
	return d
}

func baseEntry(base string) *Entry {
	rdnAttr, rdnVal := firstRDN(base)
	attrs := map[string][]string{"objectclass": {"top"}}
	switch strings.ToLower(rdnAttr) {
	case "dc":
		attrs["objectclass"] = []string{"top", "domain", "dcObject"}
		attrs["dc"] = []string{rdnVal}
	case "o":
		attrs["objectclass"] = []string{"top", "organization"}
		attrs["o"] = []string{rdnVal}
	default:
		if rdnAttr != "" {
			attrs[strings.ToLower(rdnAttr)] = []string{rdnVal}
		}
	}
	return &Entry{DN: base, Attrs: attrs}
}

func ouEntry(dn, name string) *Entry {
	return &Entry{
		DN: dn,
		Attrs: map[string][]string{
			"objectclass": {"top", "organizationalUnit"},
			"ou":          {name},
		},
	}
}

func (d *Directory) add(e *Entry) {
	if e == nil || e.DN == "" {
		return
	}
	key := normalizeDN(e.DN)
	if _, ok := d.byDN[key]; ok {
		return
	}
	d.byDN[key] = e
	d.entries = append(d.entries, e)
}

func splitName(name, username string) (cn, sn string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return username, username
	}
	parts := strings.Fields(name)
	return name, parts[len(parts)-1]
}

func firstRDN(dn string) (attr, value string) {
	parts := splitDN(dn)
	if len(parts) == 0 {
		return "", ""
	}
	rdn := strings.TrimSpace(parts[0])
	i := strings.IndexByte(rdn, '=')
	if i <= 0 {
		return "", rdn
	}
	return rdn[:i], unescapeDN(rdn[i+1:])
}

func escapeDN(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch r {
		case ',', '+', '"', '\\', '<', '>', ';', '=':
			b.WriteByte('\\')
			b.WriteRune(r)
		case ' ':
			if i == 0 || i == len(s)-1 {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		case '#':
			if i == 0 {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		case 0:
			b.WriteString(`\00`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func unescapeDN(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func splitDN(dn string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(dn); i++ {
		if dn[i] == '\\' {
			i++
			continue
		}
		if dn[i] == ',' {
			parts = append(parts, dn[start:i])
			start = i + 1
		}
	}
	if start <= len(dn) {
		parts = append(parts, dn[start:])
	}
	return parts
}

func normalizeDN(dn string) string {
	parts := splitDN(strings.TrimSpace(dn))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, strings.ToLower(p))
	}
	return strings.Join(out, ",")
}

func dnParent(dn string) string {
	parts := splitDN(dn)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Join(parts[1:], ","))
}

func dnUnder(dn, base string) bool {
	n := normalizeDN(dn)
	b := normalizeDN(base)
	if b == "" {
		return false
	}
	return n == b || strings.HasSuffix(n, ","+b)
}

func sortUints(s []uint) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && s[j] < s[j-1] {
			s[j], s[j-1] = s[j-1], s[j]
			j--
		}
	}
}

// ValidBaseDN reports whether dn is a usable base DN.
func ValidBaseDN(dn string) bool {
	dn = strings.TrimSpace(dn)
	if dn == "" || len(dn) > 512 || !utf8.ValidString(dn) {
		return false
	}
	parts := splitDN(dn)
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		i := strings.IndexByte(p, '=')
		if i <= 0 || i == len(p)-1 {
			return false
		}
		attr := p[:i]
		if attr == "" || strings.ContainsAny(attr, " +\"\\<>,;") {
			return false
		}
	}
	return true
}

// ValidOUName is a folder name that can be an RDN.
func ValidOUName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ' || r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
