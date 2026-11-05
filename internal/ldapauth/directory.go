package ldapauth

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// ErrADReadOnly is returned when a directory write is attempted against an
// Active Directory server. Factum browses AD and can reset passwords through
// the existing write-back path, but account and group edits stay in AD's
// own tools.
var ErrADReadOnly = errors.New("ldap: Active Directory accounts are managed with Active Directory's own tools")

// ErrWritebackMissing is returned when a directory write is attempted
// without either the config-file write-back account or the server's own
// bind DN and password.
var ErrWritebackMissing = errors.New("ldap: directory changes need ldap_writeback in the config file, or a bind DN and password on the OpenLDAP server")

// Record is one directory entry's editable attributes. Members is set for
// groups (member, uniqueMember, or memberUid). Groups is the person's
// direct group DNs. Person fields are set for people.
type Record struct {
	DN            string
	RDN           string
	Kind          string
	ObjectClass   string
	ObjectClasses []string
	UID           string
	CN            string
	SN            string
	Mail          string
	DisplayName   string
	Mobile        string
	Description   string
	Members       []string
	Groups        []string
	// MemberAttr is "member", "uniqueMember", or "memberUid" for a group
	// Factum can edit. Empty for an Active Directory group.
	MemberAttr string
}

// PersonInput is a new or updated inetOrgPerson. ParentDN is the entry the
// person is created under. DN is the existing entry on update. Password
// is required on create and optional on update; it is applied with the
// RFC 3062 password-modify operation so the server chooses the hash.
// Groups, when SetGroups is set, replaces the person's direct group
// membership (groupOfNames member, groupOfUniqueNames uniqueMember, and
// posixGroup memberUid).
type PersonInput struct {
	DN          string
	ParentDN    string
	UID         string
	CN          string
	SN          string
	Mail        string
	DisplayName string
	Mobile      string
	Password    string
	Groups      []string
	SetGroups   bool
}

// GroupInput is a groupOfNames entry. Members are user DNs. A groupOfNames
// entry must have at least one member.
type GroupInput struct {
	DN          string
	ParentDN    string
	CN          string
	Description string
	Members     []string
}

// connectPrimary dials only the first configured server. Directory writes
// must not fail over onto a replica.
func connectPrimary(cfg Config) (*ldap.Conn, error) {
	list := servers(cfg)
	if len(list) == 0 {
		return nil, fmt.Errorf("ldap: no host configured")
	}
	return dial(cfg, list[0].host, list[0].port)
}

func bindWriteback(conn *ldap.Conn, wb WritebackConfig) error {
	if strings.TrimSpace(wb.BindDN) == "" || wb.Password == "" {
		return ErrWritebackMissing
	}
	if err := conn.Bind(wb.BindDN, wb.Password); err != nil {
		return fmt.Errorf("ldap: write-back account bind failed: %w", err)
	}
	return nil
}

func modifyDirectory(cfg Config, wb WritebackConfig, fn func(conn *ldap.Conn) error) error {
	if strings.EqualFold(cfg.ServerType, "ad") {
		return ErrADReadOnly
	}
	conn, err := connectPrimary(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := bindWriteback(conn, wb); err != nil {
		return err
	}
	return fn(conn)
}

// DNInScope reports whether dn is the base itself or an entry under it.
// Comparison follows NormalizeDN (case-insensitive, no RFC 4514 folding).
func DNInScope(dn, base string) bool {
	dn = NormalizeDN(dn)
	base = NormalizeDN(base)
	if dn == "" || base == "" {
		return false
	}
	return dn == base || strings.HasSuffix(dn, ","+base)
}

// DNBelowBase reports whether dn is strictly under base.
func DNBelowBase(dn, base string) bool {
	dn = NormalizeDN(dn)
	base = NormalizeDN(base)
	return dn != "" && base != "" && dn != base && strings.HasSuffix(dn, ","+base)
}

func parentDN(dn string) (string, error) {
	parsed, err := ldap.ParseDN(dn)
	if err != nil || len(parsed.RDNs) < 2 {
		return "", fmt.Errorf("ldap: %q has no parent", dn)
	}
	parsed.RDNs = parsed.RDNs[1:]
	return parsed.String(), nil
}

func rdnTypeValue(dn string) (string, string, error) {
	parsed, err := ldap.ParseDN(dn)
	if err != nil || len(parsed.RDNs) == 0 || len(parsed.RDNs[0].Attributes) == 0 {
		return "", "", fmt.Errorf("ldap: cannot parse %q", dn)
	}
	attr := parsed.RDNs[0].Attributes[0]
	return attr.Type, attr.Value, nil
}

func childDN(attr, value, parent string) (string, error) {
	value = strings.TrimSpace(value)
	parent = strings.TrimSpace(parent)
	if value == "" || parent == "" {
		return "", fmt.Errorf("ldap: %s and parent DN are required", attr)
	}
	if strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("ldap: %s contains a null", attr)
	}
	return attr + "=" + ldap.EscapeDN(value) + "," + parent, nil
}

func requireInScope(dn, base, what string) error {
	if strings.TrimSpace(base) == "" {
		return fmt.Errorf("ldap: no base DN configured")
	}
	if !DNInScope(dn, base) {
		return fmt.Errorf("ldap: %s %q is outside the configured base DN", what, dn)
	}
	return nil
}

func requireBelowBase(dn, base, what string) error {
	if strings.TrimSpace(base) == "" {
		return fmt.Errorf("ldap: no base DN configured")
	}
	if !DNBelowBase(dn, base) {
		return fmt.Errorf("ldap: %s %q is outside the configured base DN", what, dn)
	}
	return nil
}

// ReadEntry loads one entry's display attributes. It uses the search
// identity and may fail over to the second host.
func ReadEntry(cfg Config, dn string) (*Record, error) {
	return ReadEntryAs(cfg, WritebackConfig{}, dn)
}

// ReadEntryAs loads one entry the way the directory editor shows it.
// A write-back or server bind account is tried first so attributes the
// search account cannot read, such as mail, still populate. A failed
// privileged bind falls back to the search account.
func ReadEntryAs(cfg Config, wb WritebackConfig, dn string) (*Record, error) {
	dn = strings.TrimSpace(dn)
	if err := requireBelowBase(dn, cfg.BaseDN, "entry"); err != nil {
		return nil, err
	}
	conn, err := dialBound(cfg, wb)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	rec, err := readRecord(conn, cfg, dn)
	if err != nil {
		return nil, err
	}
	if rec.Kind == "person" {
		groups, err := directGroups(conn, cfg.BaseDN, rec.DN, rec.UID)
		if err != nil {
			return nil, err
		}
		rec.Groups = groups
	}
	if rec.MemberAttr == "memberUid" {
		rec.Members = resolveMemberUIDs(conn, cfg.BaseDN, rec.Members)
	}
	return rec, nil
}

// dialBound binds as wb when that account is set and this is not Active
// Directory. A rejected privileged bind is discarded and the search
// account is used instead, so the editor still opens.
func dialBound(cfg Config, wb WritebackConfig) (*ldap.Conn, error) {
	conn, err := connect(cfg)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(wb.BindDN) != "" && wb.Password != "" && !strings.EqualFold(cfg.ServerType, "ad") {
		if err := conn.Bind(wb.BindDN, wb.Password); err == nil {
			return conn, nil
		}
		conn.Close()
		conn, err = connect(cfg)
		if err != nil {
			return nil, err
		}
	}
	if err := bindService(conn, cfg); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func readAttrs(cfg Config) []string {
	attrs := []string{
		"*", "objectClass", "uid", "cn", "sn", "mail", "rfc822Mailbox",
		"displayName", "mobile", "description", "member", "uniqueMember", "memberUid",
	}
	for _, extra := range []string{cfg.AttrEmail, cfg.AttrDisplayName, cfg.AttrMobile, cfg.AttrUsername, cfg.AttrGroups} {
		if strings.TrimSpace(extra) != "" {
			attrs = append(attrs, extra)
		}
	}
	return attrs
}

// mailAttr is the attribute a person write updates. The rfc822Mailbox
// alias is stored as mail.
func mailAttr(cfg Config) string {
	a := strings.TrimSpace(cfg.AttrEmail)
	if a == "" || strings.EqualFold(a, "mail") || strings.EqualFold(a, "rfc822Mailbox") {
		return "mail"
	}
	return a
}

// attrBase is an attribute description without a language option, with
// the OpenLDAP aliases of mail and uid folded to the primary name.
func attrBase(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.IndexByte(name, ';'); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "rfc822mailbox":
		return "mail"
	case "userid":
		return "uid"
	default:
		return name
	}
}

// entryString returns the first non-empty value of names. A language
// option such as mail;lang-en matches mail, and rfc822Mailbox matches mail.
func entryString(e *ldap.Entry, names ...string) string {
	if e == nil {
		return ""
	}
	for _, name := range names {
		want := attrBase(name)
		if want == "" {
			continue
		}
		for _, a := range e.Attributes {
			if attrBase(a.Name) != want {
				continue
			}
			for _, v := range a.Values {
				if strings.TrimSpace(v) != "" {
					return v
				}
			}
		}
	}
	return ""
}

func readRecord(conn *ldap.Conn, cfg Config, dn string) (*Record, error) {
	req := ldap.NewSearchRequest(
		dn, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)", readAttrs(cfg), nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ldap: read %q failed: %w", dn, err)
	}
	if len(res.Entries) == 0 {
		return nil, fmt.Errorf("ldap: no entry at %q", dn)
	}
	e := res.Entries[0]
	classes := e.GetEqualFoldAttributeValues("objectClass")
	rec := &Record{
		DN:            e.DN,
		RDN:           rdnValue(e.DN),
		Kind:          EntryKind(classes),
		ObjectClass:   StructuralClass(classes),
		ObjectClasses: classes,
		UID:           entryString(e, "uid", "userid"),
		CN:            entryString(e, "cn"),
		SN:            entryString(e, "sn"),
		Mail:          entryString(e, cfg.AttrEmail, "mail", "rfc822Mailbox"),
		DisplayName:   entryString(e, cfg.AttrDisplayName, "displayName"),
		Mobile:        entryString(e, cfg.AttrMobile, "mobile"),
		Description:   entryString(e, "description"),
	}
	switch {
	case hasClass(classes, "groupofnames") || (hasClass(classes, "group") && !strings.EqualFold(cfg.ServerType, "ad")):
		rec.MemberAttr = "member"
		rec.Members = e.GetEqualFoldAttributeValues("member")
	case hasClass(classes, "groupofuniquenames"):
		rec.MemberAttr = "uniqueMember"
		rec.Members = e.GetEqualFoldAttributeValues("uniqueMember")
	case hasClass(classes, "posixgroup"):
		rec.MemberAttr = "memberUid"
		rec.Members = e.GetEqualFoldAttributeValues("memberUid")
	}
	if rec.Members == nil {
		rec.Members = []string{}
	}
	if rec.Groups == nil {
		rec.Groups = []string{}
	}
	return rec, nil
}

func hasClass(classes []string, want string) bool {
	for _, c := range classes {
		if strings.EqualFold(c, want) {
			return true
		}
	}
	return false
}

// CreatePerson adds an inetOrgPerson under parent and sets its password.
func CreatePerson(cfg Config, wb WritebackConfig, in PersonInput) (string, error) {
	uid := strings.TrimSpace(in.UID)
	cn := strings.TrimSpace(in.CN)
	sn := strings.TrimSpace(in.SN)
	parent := strings.TrimSpace(in.ParentDN)
	if uid == "" || cn == "" || sn == "" {
		return "", fmt.Errorf("ldap: uid, cn, and sn are required")
	}
	if strings.TrimSpace(in.Password) == "" {
		return "", fmt.Errorf("ldap: a password is required")
	}
	if err := requireInScope(parent, cfg.BaseDN, "parent"); err != nil {
		return "", err
	}
	dn, err := childDN("uid", uid, parent)
	if err != nil {
		return "", err
	}
	if err := requireBelowBase(dn, cfg.BaseDN, "entry"); err != nil {
		return "", err
	}
	var created string
	err = modifyDirectory(cfg, wb, func(conn *ldap.Conn) error {
		add := ldap.NewAddRequest(dn, nil)
		add.Attribute("objectClass", []string{"top", "person", "organizationalPerson", "inetOrgPerson"})
		add.Attribute("uid", []string{uid})
		add.Attribute("cn", []string{cn})
		add.Attribute("sn", []string{sn})
		if v := strings.TrimSpace(in.Mail); v != "" {
			add.Attribute(mailAttr(cfg), []string{v})
		}
		if v := strings.TrimSpace(in.DisplayName); v != "" {
			add.Attribute("displayName", []string{v})
		}
		if v := strings.TrimSpace(in.Mobile); v != "" {
			add.Attribute("mobile", []string{v})
		}
		if err := conn.Add(add); err != nil {
			return fmt.Errorf("ldap: add user %q failed: %w", dn, err)
		}
		if _, err := conn.PasswordModify(ldap.NewPasswordModifyRequest(dn, "", in.Password)); err != nil {
			_ = conn.Del(ldap.NewDelRequest(dn, nil))
			return fmt.Errorf("ldap: set password for %q failed: %w", dn, err)
		}
		if in.SetGroups {
			if err := syncPersonGroups(conn, cfg, dn, uid, in.Groups); err != nil {
				_ = conn.Del(ldap.NewDelRequest(dn, nil))
				return err
			}
		}
		created = dn
		return nil
	})
	return created, err
}

// UpdatePerson changes an inetOrgPerson's attributes. A uid change renames
// the entry when uid is the RDN, and group member attributes that pointed
// at the old DN are rewritten. Password, when set, is a separate
// password-modify after the attribute update.
func UpdatePerson(cfg Config, wb WritebackConfig, in PersonInput) (string, error) {
	dn := strings.TrimSpace(in.DN)
	if err := requireBelowBase(dn, cfg.BaseDN, "entry"); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.UID) == "" || strings.TrimSpace(in.CN) == "" || strings.TrimSpace(in.SN) == "" {
		return "", fmt.Errorf("ldap: uid, cn, and sn are required")
	}
	var newDN string
	err := modifyDirectory(cfg, wb, func(conn *ldap.Conn) error {
		rec, err := readRecord(conn, cfg, dn)
		if err != nil {
			return err
		}
		if rec.Kind != "person" {
			return fmt.Errorf("ldap: %q is not a user entry", dn)
		}
		updated, err := renamePersonIfNeeded(conn, cfg.BaseDN, rec, in)
		if err != nil {
			return err
		}
		mod := ldap.NewModifyRequest(updated, nil)
		emailAttr := mailAttr(cfg)
		current := map[string]string{
			"uid": rec.UID, "cn": rec.CN, "sn": rec.SN,
			emailAttr: rec.Mail, "displayName": rec.DisplayName, "mobile": rec.Mobile,
		}
		next := map[string]string{
			"uid": strings.TrimSpace(in.UID), "cn": strings.TrimSpace(in.CN), "sn": strings.TrimSpace(in.SN),
			emailAttr: strings.TrimSpace(in.Mail), "displayName": strings.TrimSpace(in.DisplayName), "mobile": strings.TrimSpace(in.Mobile),
		}
		if applyAttrDiff(mod, current, next) {
			if err := conn.Modify(mod); err != nil {
				return fmt.Errorf("ldap: update user %q failed: %w", updated, err)
			}
		}
		if strings.TrimSpace(in.Password) != "" {
			if _, err := conn.PasswordModify(ldap.NewPasswordModifyRequest(updated, "", in.Password)); err != nil {
				return fmt.Errorf("ldap: set password for %q failed: %w", updated, err)
			}
		}
		if in.SetGroups {
			if err := syncPersonGroups(conn, cfg, updated, strings.TrimSpace(in.UID), in.Groups); err != nil {
				return err
			}
		}
		newDN = updated
		return nil
	})
	return newDN, err
}

func renamePersonIfNeeded(conn *ldap.Conn, base string, rec *Record, in PersonInput) (string, error) {
	attr, val, err := rdnTypeValue(rec.DN)
	if err != nil {
		return "", err
	}
	uid := strings.TrimSpace(in.UID)
	if !strings.EqualFold(attr, "uid") || strings.EqualFold(val, uid) {
		return rec.DN, nil
	}
	parent, err := parentDN(rec.DN)
	if err != nil {
		return "", err
	}
	rdn := "uid=" + ldap.EscapeDN(uid)
	if err := conn.ModifyDN(ldap.NewModifyDNRequest(rec.DN, rdn, true, "")); err != nil {
		return "", fmt.Errorf("ldap: rename %q failed: %w", rec.DN, err)
	}
	updated := rdn + "," + parent
	if err := rewriteMemberDN(conn, base, rec.DN, updated); err != nil {
		return "", err
	}
	if rec.UID != "" && !strings.EqualFold(rec.UID, uid) {
		if err := rewriteMemberUID(conn, base, rec.UID, uid); err != nil {
			return "", err
		}
	}
	return updated, nil
}

// DeletePerson removes the user from group member attributes, then deletes
// the entry. The local Factum shadow row is left for the caller.
func DeletePerson(cfg Config, wb WritebackConfig, dn string) error {
	dn = strings.TrimSpace(dn)
	if err := requireBelowBase(dn, cfg.BaseDN, "entry"); err != nil {
		return err
	}
	return modifyDirectory(cfg, wb, func(conn *ldap.Conn) error {
		rec, err := readRecord(conn, cfg, dn)
		if err != nil {
			return err
		}
		if rec.Kind != "person" {
			return fmt.Errorf("ldap: %q is not a user entry", dn)
		}
		if err := rewriteMemberDN(conn, cfg.BaseDN, dn, ""); err != nil {
			return err
		}
		if rec.UID != "" {
			if err := rewriteMemberUID(conn, cfg.BaseDN, rec.UID, ""); err != nil {
				return err
			}
		}
		if err := conn.Del(ldap.NewDelRequest(dn, nil)); err != nil {
			return fmt.Errorf("ldap: delete user %q failed: %w", dn, err)
		}
		return nil
	})
}

// CreateGroup adds a groupOfNames entry. Members must be DNs under the base.
func CreateGroup(cfg Config, wb WritebackConfig, in GroupInput) (string, error) {
	cn := strings.TrimSpace(in.CN)
	parent := strings.TrimSpace(in.ParentDN)
	members, err := cleanMembers(in.Members, cfg.BaseDN)
	if err != nil {
		return "", err
	}
	if cn == "" {
		return "", fmt.Errorf("ldap: cn is required")
	}
	if len(members) == 0 {
		return "", fmt.Errorf("ldap: a group needs at least one member")
	}
	if err := requireInScope(parent, cfg.BaseDN, "parent"); err != nil {
		return "", err
	}
	dn, err := childDN("cn", cn, parent)
	if err != nil {
		return "", err
	}
	if err := requireBelowBase(dn, cfg.BaseDN, "entry"); err != nil {
		return "", err
	}
	var created string
	err = modifyDirectory(cfg, wb, func(conn *ldap.Conn) error {
		add := ldap.NewAddRequest(dn, nil)
		add.Attribute("objectClass", []string{"top", "groupOfNames"})
		add.Attribute("cn", []string{cn})
		add.Attribute("member", members)
		if v := strings.TrimSpace(in.Description); v != "" {
			add.Attribute("description", []string{v})
		}
		if err := conn.Add(add); err != nil {
			return fmt.Errorf("ldap: add group %q failed: %w", dn, err)
		}
		created = dn
		return nil
	})
	return created, err
}

// UpdateGroup changes a groupOfNames, groupOfUniqueNames, or posixGroup
// entry. A cn change renames a cn-RDN entry and returns the new DN so the
// caller can retarget role mappings. Active Directory groups are refused.
// posixGroup members are user DNs in and memberUid values on the server.
func UpdateGroup(cfg Config, wb WritebackConfig, in GroupInput) (string, error) {
	dn := strings.TrimSpace(in.DN)
	if err := requireBelowBase(dn, cfg.BaseDN, "entry"); err != nil {
		return "", err
	}
	cn := strings.TrimSpace(in.CN)
	desc := strings.TrimSpace(in.Description)
	if cn == "" {
		return "", fmt.Errorf("ldap: cn is required")
	}
	var newDN string
	err := modifyDirectory(cfg, wb, func(conn *ldap.Conn) error {
		rec, err := readRecord(conn, cfg, dn)
		if err != nil {
			return err
		}
		if rec.Kind != "group" {
			return fmt.Errorf("ldap: %q is not a group", dn)
		}
		if rec.MemberAttr == "" {
			return fmt.Errorf("ldap: %q is not a groupOfNames, groupOfUniqueNames, or posixGroup group", dn)
		}
		var members []string
		if rec.MemberAttr == "memberUid" {
			members, err = memberUIDs(conn, cfg, in.Members)
		} else {
			members, err = cleanMembers(in.Members, cfg.BaseDN)
		}
		if err != nil {
			return err
		}
		if rec.MemberAttr != "memberUid" && len(members) == 0 {
			return fmt.Errorf("ldap: a group needs at least one member")
		}
		updated := dn
		attr, val, err := rdnTypeValue(dn)
		if err != nil {
			return err
		}
		if strings.EqualFold(attr, "cn") && !strings.EqualFold(val, cn) {
			parent, err := parentDN(dn)
			if err != nil {
				return err
			}
			rdn := "cn=" + ldap.EscapeDN(cn)
			if err := conn.ModifyDN(ldap.NewModifyDNRequest(dn, rdn, true, "")); err != nil {
				return fmt.Errorf("ldap: rename %q failed: %w", dn, err)
			}
			updated = rdn + "," + parent
		}
		mod := ldap.NewModifyRequest(updated, nil)
		mod.Replace("cn", []string{cn})
		switch {
		case len(members) == 0 && len(rec.Members) > 0:
			mod.Delete(rec.MemberAttr, []string{})
		case len(members) > 0:
			mod.Replace(rec.MemberAttr, members)
		}
		if desc == "" {
			if rec.Description != "" {
				mod.Delete("description", []string{})
			}
		} else if desc != rec.Description {
			mod.Replace("description", []string{desc})
		}
		if err := conn.Modify(mod); err != nil {
			return fmt.Errorf("ldap: update group %q failed: %w", updated, err)
		}
		newDN = updated
		return nil
	})
	return newDN, err
}

// DeleteGroup deletes a group entry. The caller removes Factum rows that
// stored the DN.
func DeleteGroup(cfg Config, wb WritebackConfig, dn string) error {
	dn = strings.TrimSpace(dn)
	if err := requireBelowBase(dn, cfg.BaseDN, "entry"); err != nil {
		return err
	}
	return modifyDirectory(cfg, wb, func(conn *ldap.Conn) error {
		rec, err := readRecord(conn, cfg, dn)
		if err != nil {
			return err
		}
		if rec.Kind != "group" {
			return fmt.Errorf("ldap: %q is not a group", dn)
		}
		if err := conn.Del(ldap.NewDelRequest(dn, nil)); err != nil {
			return fmt.Errorf("ldap: delete group %q failed: %w", dn, err)
		}
		return nil
	})
}

// membershipFilter matches groups that list personDN or uid directly.
func membershipFilter(personDN, uid string) string {
	var clauses []string
	if dn := strings.TrimSpace(personDN); dn != "" {
		esc := ldap.EscapeFilter(dn)
		clauses = append(clauses, "(member="+esc+")", "(uniqueMember="+esc+")")
	}
	if id := strings.TrimSpace(uid); id != "" {
		clauses = append(clauses, "(memberUid="+ldap.EscapeFilter(id)+")")
	}
	if len(clauses) == 0 {
		return ""
	}
	return "(|" + strings.Join(clauses, "") + ")"
}

func directGroups(conn *ldap.Conn, base, personDN, uid string) ([]string, error) {
	filter := membershipFilter(personDN, uid)
	if filter == "" || strings.TrimSpace(base) == "" {
		return []string{}, nil
	}
	req := ldap.NewSearchRequest(
		base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter, []string{"objectClass"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ldap: group search for %q failed: %w", personDN, err)
	}
	self := NormalizeDN(personDN)
	seen := map[string]bool{}
	out := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		key := NormalizeDN(e.DN)
		if key == "" || key == self || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e.DN)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out, nil
}

func resolveMemberUIDs(conn *ldap.Conn, base string, uids []string) []string {
	out := make([]string, 0, len(uids))
	for _, uid := range uids {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if strings.Contains(uid, "=") {
			out = append(out, uid)
			continue
		}
		if dn := findUID(conn, base, uid); dn != "" {
			out = append(out, dn)
			continue
		}
		out = append(out, uid)
	}
	return out
}

func findUID(conn *ldap.Conn, base, uid string) string {
	if strings.TrimSpace(base) == "" || strings.TrimSpace(uid) == "" {
		return ""
	}
	filter := "(uid=" + ldap.EscapeFilter(uid) + ")"
	req := ldap.NewSearchRequest(
		base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		filter, []string{"dn"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil || len(res.Entries) != 1 {
		return ""
	}
	return res.Entries[0].DN
}

func memberUIDs(conn *ldap.Conn, cfg Config, members []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(members))
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		uid := m
		if strings.Contains(m, "=") {
			if err := requireBelowBase(m, cfg.BaseDN, "member"); err != nil {
				return nil, err
			}
			attr, val, err := rdnTypeValue(m)
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(attr, "uid") {
				uid = val
			} else {
				rec, err := readRecord(conn, cfg, m)
				if err != nil {
					return nil, err
				}
				uid = strings.TrimSpace(rec.UID)
				if uid == "" {
					return nil, fmt.Errorf("ldap: %q has no uid for memberUid", m)
				}
			}
		}
		key := strings.ToLower(uid)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, uid)
	}
	return out, nil
}

// syncPersonGroups makes personDN a direct member of exactly want.
// groupOfNames and groupOfUniqueNames keep at least one member.
func syncPersonGroups(conn *ldap.Conn, cfg Config, personDN, uid string, want []string) error {
	current, err := directGroups(conn, cfg.BaseDN, personDN, uid)
	if err != nil {
		return err
	}
	wanted := map[string]string{}
	for _, g := range want {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if _, err := ldap.ParseDN(g); err != nil {
			return fmt.Errorf("ldap: group %q is not a DN", g)
		}
		if err := requireBelowBase(g, cfg.BaseDN, "group"); err != nil {
			return err
		}
		if NormalizeDN(g) == NormalizeDN(personDN) {
			return fmt.Errorf("ldap: %q cannot be a member of itself", personDN)
		}
		wanted[NormalizeDN(g)] = g
	}
	have := map[string]string{}
	for _, g := range current {
		have[NormalizeDN(g)] = g
	}
	var remove, add []string
	for key, g := range have {
		if _, ok := wanted[key]; !ok {
			remove = append(remove, g)
		}
	}
	for key, g := range wanted {
		if _, ok := have[key]; !ok {
			add = append(add, g)
		}
	}
	for _, g := range remove {
		if err := setMembership(conn, cfg, g, personDN, uid, false); err != nil {
			return err
		}
	}
	for _, g := range add {
		if err := setMembership(conn, cfg, g, personDN, uid, true); err != nil {
			return err
		}
	}
	return nil
}

func setMembership(conn *ldap.Conn, cfg Config, groupDN, personDN, uid string, present bool) error {
	rec, err := readRecord(conn, cfg, groupDN)
	if err != nil {
		return err
	}
	if rec.Kind != "group" || rec.MemberAttr == "" {
		return fmt.Errorf("ldap: %q is not a groupOfNames, groupOfUniqueNames, or posixGroup group", groupDN)
	}
	value := personDN
	if rec.MemberAttr == "memberUid" {
		value = strings.TrimSpace(uid)
		if value == "" {
			return fmt.Errorf("ldap: %q has no uid to store on %q", personDN, groupDN)
		}
	}
	found, exact := membershipValue(rec.MemberAttr, rec.Members, personDN, value)
	if present == found {
		return nil
	}
	if !present && (rec.MemberAttr == "member" || rec.MemberAttr == "uniqueMember") && len(rec.Members) <= 1 {
		return fmt.Errorf("ldap: %q must keep at least one member", groupDN)
	}
	mod := ldap.NewModifyRequest(groupDN, nil)
	if present {
		mod.Add(rec.MemberAttr, []string{value})
	} else {
		mod.Delete(rec.MemberAttr, []string{exact})
	}
	if err := conn.Modify(mod); err != nil {
		return fmt.Errorf("ldap: update %s on %q failed: %w", rec.MemberAttr, groupDN, err)
	}
	return nil
}

func membershipValue(attr string, values []string, personDN, uid string) (bool, string) {
	for _, v := range values {
		if attr == "memberUid" {
			if strings.EqualFold(v, uid) {
				return true, v
			}
			continue
		}
		if NormalizeDN(v) == NormalizeDN(personDN) {
			return true, v
		}
	}
	return false, ""
}

func cleanMembers(members []string, base string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(members))
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, err := ldap.ParseDN(m); err != nil {
			return nil, fmt.Errorf("ldap: member %q is not a DN", m)
		}
		if err := requireBelowBase(m, base, "member"); err != nil {
			return nil, err
		}
		key := NormalizeDN(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out, nil
}

// applyAttrDiff adds replace or delete operations for keys whose value
// changed. An empty next value deletes the attribute. It returns whether
// any change was added.
func applyAttrDiff(mod *ldap.ModifyRequest, current, next map[string]string) bool {
	changed := false
	for k, want := range next {
		have := current[k]
		if have == want {
			continue
		}
		changed = true
		if want == "" {
			mod.Delete(k, []string{})
		} else {
			mod.Replace(k, []string{want})
		}
	}
	return changed
}

func rewriteMemberDN(conn *ldap.Conn, base, oldDN, newDN string) error {
	if err := rewriteAttrValue(conn, base, "member", oldDN, newDN, true); err != nil {
		return err
	}
	return rewriteAttrValue(conn, base, "uniqueMember", oldDN, newDN, true)
}

func rewriteMemberUID(conn *ldap.Conn, base, oldUID, newUID string) error {
	return rewriteAttrValue(conn, base, "memberUid", oldUID, newUID, false)
}

func rewriteAttrValue(conn *ldap.Conn, base, attr, oldVal, newVal string, dnValue bool) error {
	filter := fmt.Sprintf("(%s=%s)", attr, ldap.EscapeFilter(oldVal))
	req := ldap.NewSearchRequest(
		base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter, []string{attr}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return fmt.Errorf("ldap: search %s=%q failed: %w", attr, oldVal, err)
	}
	match := func(v string) bool {
		if dnValue {
			return NormalizeDN(v) == NormalizeDN(oldVal)
		}
		return strings.EqualFold(v, oldVal)
	}
	for _, e := range res.Entries {
		mod := ldap.NewModifyRequest(e.DN, nil)
		n := 0
		for _, v := range e.GetEqualFoldAttributeValues(attr) {
			if !match(v) {
				continue
			}
			mod.Delete(attr, []string{v})
			if newVal != "" {
				mod.Add(attr, []string{newVal})
			}
			n++
		}
		if n == 0 {
			continue
		}
		if err := conn.Modify(mod); err != nil {
			return fmt.Errorf("ldap: update %s on %q failed: %w", attr, e.DN, err)
		}
	}
	return nil
}
