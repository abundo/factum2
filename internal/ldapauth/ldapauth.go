// Package ldapauth implements the search-and-bind flow used to authenticate
// a user against an LDAP/Active Directory server and to read the attributes
// (email, display name, group membership) needed for local auto-provisioning
// and role sync. It has no DB dependency beyond the Config it's handed - see
// web/auth.go for how it's wired into the local-first login flow, and
// models.Settings for where the connection config is stored.
package ldapauth

import (
	"crypto/tls"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/abundo/factum2/models"
	"github.com/go-ldap/ldap/v3"
)

// Config is a projection of models.Settings' LDAP fields, kept separate so
// this package doesn't need to import gorm or know about the DB row shape.
type Config struct {
	Host          string
	Port          uint16
	TLSMode       string // "none" | "starttls" | "ldaps"
	SkipTLSVerify bool
	// ServerType is "ad" | "generic" (OpenLDAP-compatible). Server-specific
	// behavior that can't be expressed the same way on both (e.g. how a
	// password change is written back) branches on this.
	ServerType   string
	BindDN       string
	BindPassword string
	BaseDN       string
	// UserFilter is a fmt.Sprintf template with one %s for the escaped
	// username, e.g. "(sAMAccountName=%s)" for AD or "(uid=%s)" for
	// OpenLDAP.
	UserFilter string
	// AttrUsername is the attribute holding the value used as the login
	// username (the value UserFilter's %s is matched against) -
	// "sAMAccountName" for AD, "uid" for OpenLDAP by default. Ordinary
	// login already knows the username (it's what was typed into the
	// form); this is only needed by LookupByEmail, which has to derive it
	// from the directory instead.
	AttrUsername    string
	AttrEmail       string
	AttrDisplayName string
	AttrMobile      string
	// AttrGroups is the attribute holding group DNs on the user entry, e.g.
	// "memberOf" for both AD and OpenLDAP (with the memberOf overlay).
	AttrGroups string
}

// ConfigFromSettings builds a Config from the DB-backed Settings row. Blank
// attribute fields fall back to their standard AD/OpenLDAP name rather than
// being left unrequested - an admin who just enables LDAP and fills in the
// connection details (without also typing out every attribute name) still
// gets Name/Email/Mobile/group-based roles populated correctly, instead of
// them silently staying empty (Name then falling back further, to the raw
// login username).
func ConfigFromSettings(s *models.Settings) Config {
	serverType := orDefault(s.LdapServerType, "generic")
	return Config{
		Host:            s.LdapHost,
		Port:            s.LdapPort,
		TLSMode:         s.LdapTLSMode,
		SkipTLSVerify:   s.LdapSkipTLSVerify != nil && *s.LdapSkipTLSVerify,
		ServerType:      serverType,
		BindDN:          s.LdapBindDN,
		BindPassword:    s.LdapBindPassword,
		BaseDN:          s.LdapBaseDN,
		UserFilter:      s.LdapUserFilter,
		AttrUsername:    orDefault(s.LdapAttrUsername, defaultUsernameAttr(serverType)),
		AttrEmail:       orDefault(s.LdapAttrEmail, "mail"),
		AttrDisplayName: orDefault(s.LdapAttrDisplayName, "displayName"),
		AttrMobile:      orDefault(s.LdapAttrMobile, "mobile"),
		AttrGroups:      orDefault(s.LdapAttrGroups, "memberOf"),
	}
}

func orDefault(value, def string) string {
	if value == "" {
		return def
	}
	return value
}

// defaultUsernameAttr is AttrUsername's fallback, server-type-dependent
// unlike the other Attr* defaults (which happen to be the same name on
// both AD and OpenLDAP-with-memberOf-overlay) - AD's login attribute is
// conventionally sAMAccountName, OpenLDAP's is uid, same convention
// already documented on LdapUserFilter's placeholder text.
func defaultUsernameAttr(serverType string) string {
	if serverType == "ad" {
		return "sAMAccountName"
	}
	return "uid"
}

// UserInfo is what a successful directory lookup yields for provisioning and
// role-sync purposes.
type UserInfo struct {
	DN string
	// Username is the value of Config.AttrUsername on the matched entry -
	// only populated by LookupByEmail, which (unlike Authenticate) doesn't
	// already know it from the caller's login input.
	Username    string
	Email       string
	DisplayName string
	Mobile      string
	Groups      []string // raw group DNs from Config.AttrGroups
}

// normalizeHost strips a leading "ldap://"/"ldaps://" scheme and any
// trailing slash an admin may have typed into the Host field - Host is
// meant to be a bare hostname/IP, with the scheme and port added by
// connect() itself. Without this, a Host of "ldaps://dc1.example.com"
// combined with TLSMode "ldaps" would dial "ldaps://ldaps://dc1.example.com",
// which fails DNS resolution on the literal string "ldaps".
func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "ldaps://")
	host = strings.TrimPrefix(host, "ldap://")
	return strings.TrimSuffix(host, "/")
}

// connect dials the configured server and, for TLSMode == "starttls",
// upgrades the connection before returning. Callers own conn.Close().
func connect(cfg Config) (*ldap.Conn, error) {
	host := normalizeHost(cfg.Host)
	scheme := "ldap"
	if cfg.TLSMode == "ldaps" {
		scheme = "ldaps"
	}
	addr := fmt.Sprintf("%s://%s:%d", scheme, host, cfg.Port)

	var opts []ldap.DialOpt
	if cfg.TLSMode == "ldaps" {
		opts = append(opts, ldap.DialWithTLSConfig(&tls.Config{
			ServerName:         host,
			InsecureSkipVerify: cfg.SkipTLSVerify,
		}))
	}
	conn, err := ldap.DialURL(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("ldap: dial %s: %w", addr, err)
	}
	if cfg.TLSMode == "starttls" {
		if err := conn.StartTLS(&tls.Config{
			ServerName:         host,
			InsecureSkipVerify: cfg.SkipTLSVerify,
		}); err != nil {
			conn.Close()
			return nil, fmt.Errorf("ldap: starttls: %w", err)
		}
	}
	return conn, nil
}

// TestConnection dials, binds as the service account and issues a
// zero-result-tolerant search under BaseDN - used by the admin "Test
// Connection" button. Returns nil on success.
func TestConnection(cfg Config) error {
	conn, err := connect(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return fmt.Errorf("ldap: service-account bind failed: %w", err)
	}
	req := ldap.NewSearchRequest(
		cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)", []string{"dn"}, nil,
	)
	if _, err := conn.Search(req); err != nil {
		// Active Directory (unlike OpenLDAP, which truncates and returns
		// success) responds to a search that would exceed SizeLimit with an
		// explicit sizeLimitExceeded error rather than a partial result set.
		// Since BaseDN almost always contains more than one object, that's
		// the expected outcome here and actually confirms the bind+search
		// succeeded - only a genuine failure should be reported.
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return nil
		}
		return fmt.Errorf("ldap: search under base DN failed: %w", err)
	}
	return nil
}

// Authenticate performs the search+bind flow: bind as the configured service
// account, search BaseDN with UserFilter for username, then re-bind (on a
// fresh connection) as the single matched DN with password to verify
// credentials.
//
// Return contract mirrors web.AuthenticateUser's existing split:
//   - (true, info, nil):  credentials verified, info populated.
//   - (false, nil, nil):  bad credentials - service bind ok, but either no
//     user matched the filter or the user-DN bind was rejected
//     (LDAPResultInvalidCredentials). Same shape as a wrong password.
//   - (false, nil, err):  infra/config problem - dial failure, service-account
//     bind failure, malformed filter, search error, ambiguous match (>1
//     entries - almost certainly a misconfigured UserFilter), or any bind
//     failure that isn't LDAPResultInvalidCredentials. Callers should treat
//     this as a real error (e.g. 500), not silently show "wrong password".
func Authenticate(cfg Config, username, password string) (bool, *UserInfo, error) {
	conn, err := connect(cfg)
	if err != nil {
		return false, nil, err
	}
	defer conn.Close()

	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return false, nil, fmt.Errorf("ldap: service-account bind failed: %w", err)
	}

	filter := fmt.Sprintf(cfg.UserFilter, ldap.EscapeFilter(username))
	attrs := []string{"dn"}
	if cfg.AttrEmail != "" {
		attrs = append(attrs, cfg.AttrEmail)
	}
	if cfg.AttrDisplayName != "" {
		attrs = append(attrs, cfg.AttrDisplayName)
	}
	if cfg.AttrMobile != "" {
		attrs = append(attrs, cfg.AttrMobile)
	}
	if cfg.AttrGroups != "" {
		attrs = append(attrs, cfg.AttrGroups)
	}

	// SizeLimit is 2, not 1, so an ambiguous match can be detected and
	// reported instead of silently taking Entries[0] of a truncated result.
	req := ldap.NewSearchRequest(
		cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		filter, attrs, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return false, nil, fmt.Errorf("ldap: user search failed: %w", err)
	}
	if len(res.Entries) == 0 {
		return false, nil, nil // unknown user - not an error
	}
	if len(res.Entries) > 1 {
		return false, nil, fmt.Errorf("ldap: user filter %q matched %d entries, expected 1 - check ldap_user_filter", filter, len(res.Entries))
	}
	entry := res.Entries[0]

	// Second bind, as the found user DN with the supplied password - the
	// actual credential check. Needs a fresh connection: this one is already
	// bound as the service account, and go-ldap has no "verify without
	// changing bind state" call.
	userConn, err := connect(cfg)
	if err != nil {
		return false, nil, err
	}
	defer userConn.Close()

	if err := userConn.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("ldap: user bind failed: %w", err)
	}

	// GetEqualFoldAttributeValue*, not GetAttributeValue*: some servers
	// (notably AD) can echo an attribute name back with different casing
	// than requested, and LDAP attribute names are case-insensitive per
	// RFC 4511 anyway.
	info := &UserInfo{DN: entry.DN}
	if cfg.AttrEmail != "" {
		info.Email = entry.GetEqualFoldAttributeValue(cfg.AttrEmail)
	}
	if cfg.AttrDisplayName != "" {
		info.DisplayName = entry.GetEqualFoldAttributeValue(cfg.AttrDisplayName)
	}
	if cfg.AttrMobile != "" {
		info.Mobile = entry.GetEqualFoldAttributeValue(cfg.AttrMobile)
	}
	if cfg.AttrGroups != "" {
		info.Groups = entry.GetEqualFoldAttributeValues(cfg.AttrGroups)
	}
	return true, info, nil
}

// LookupByEmail searches for a directory entry whose AttrEmail attribute
// matches email, binding as the same read-only service account
// Authenticate/Browse use. Unlike Authenticate, it never verifies a
// password - a match only means "an account with this email exists in the
// directory," not that the caller is that person - so callers must not
// treat the result as an authenticated identity. Used by the
// forgot-password flow to auto-provision an LDAP user's local row before
// they've ever logged in (see web.provisionLDAPUserByEmail), the one case
// where a login attempt (and thus Authenticate, which already knows the
// username) hasn't happened yet.
//
// Return contract mirrors Authenticate's:
//   - (info, nil):  exactly one entry matched, info.Username populated
//     from AttrUsername.
//   - (nil, nil):   no entry matched, or Config.AttrEmail/AttrUsername is
//     blank - not an error.
//   - (nil, err):   infra/config problem, or an ambiguous match (>1
//     entries).
func LookupByEmail(cfg Config, email string) (*UserInfo, error) {
	if cfg.AttrEmail == "" || cfg.AttrUsername == "" || email == "" {
		return nil, nil
	}

	conn, err := connect(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return nil, fmt.Errorf("ldap: service-account bind failed: %w", err)
	}

	filter := fmt.Sprintf("(%s=%s)", cfg.AttrEmail, ldap.EscapeFilter(email))
	attrs := []string{"dn", cfg.AttrUsername, cfg.AttrEmail}
	if cfg.AttrDisplayName != "" {
		attrs = append(attrs, cfg.AttrDisplayName)
	}
	if cfg.AttrMobile != "" {
		attrs = append(attrs, cfg.AttrMobile)
	}
	if cfg.AttrGroups != "" {
		attrs = append(attrs, cfg.AttrGroups)
	}

	// SizeLimit is 2, not 1, so an ambiguous match can be detected and
	// reported instead of silently taking Entries[0] of a truncated result.
	req := ldap.NewSearchRequest(
		cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		filter, attrs, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ldap: email lookup failed: %w", err)
	}
	if len(res.Entries) == 0 {
		return nil, nil // no account with this email - not an error
	}
	if len(res.Entries) > 1 {
		return nil, fmt.Errorf("ldap: email %q matched %d entries, expected 1", email, len(res.Entries))
	}
	entry := res.Entries[0]

	info := &UserInfo{
		DN:       entry.DN,
		Username: entry.GetEqualFoldAttributeValue(cfg.AttrUsername),
		Email:    entry.GetEqualFoldAttributeValue(cfg.AttrEmail),
	}
	if cfg.AttrDisplayName != "" {
		info.DisplayName = entry.GetEqualFoldAttributeValue(cfg.AttrDisplayName)
	}
	if cfg.AttrMobile != "" {
		info.Mobile = entry.GetEqualFoldAttributeValue(cfg.AttrMobile)
	}
	if cfg.AttrGroups != "" {
		info.Groups = entry.GetEqualFoldAttributeValues(cfg.AttrGroups)
	}
	if info.Username == "" {
		return nil, fmt.Errorf("ldap: entry %q matched by email has no value for attribute %q - check ldap_attr_username", entry.DN, cfg.AttrUsername)
	}

	// info.Username is only useful if it actually resolves back through
	// UserFilter to this same entry - Authenticate (at the next login) and
	// ChangePassword (at the next password reset/change) both look a user
	// up via UserFilter, not AttrUsername, so a mismatch here would
	// silently provision a local row that can never be found again. This
	// almost always means AttrUsername names a different attribute than
	// whatever UserFilter's %s actually filters on - e.g. UserFilter is
	// "(mail=%s)" but AttrUsername defaulted to "uid" instead of being set
	// to "mail" to match.
	verifyFilter := fmt.Sprintf(cfg.UserFilter, ldap.EscapeFilter(info.Username))
	verifyReq := ldap.NewSearchRequest(
		cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		verifyFilter, []string{"dn"}, nil,
	)
	verifyRes, err := conn.Search(verifyReq)
	if err != nil {
		return nil, fmt.Errorf("ldap: verifying attribute %q value %q against ldap_user_filter failed: %w", cfg.AttrUsername, info.Username, err)
	}
	if len(verifyRes.Entries) != 1 || NormalizeDN(verifyRes.Entries[0].DN) != NormalizeDN(entry.DN) {
		return nil, fmt.Errorf("ldap: attribute %q value %q (from entry %q) does not resolve back to the same entry via ldap_user_filter %q - ldap_attr_username likely names the wrong attribute for this filter",
			cfg.AttrUsername, info.Username, entry.DN, cfg.UserFilter)
	}

	return info, nil
}

// WritebackConfig is the elevated identity permitted to reset another
// user's password - deliberately separate from Config.BindDN/BindPassword
// (the read-only search/bind service account, DB-stored via
// models.Settings and returned in full by GET /api/admin/settings).
// Password write-back needs much stronger directory permissions than a
// search bind, so this comes from util.ConfigRoot.LdapWriteback (YAML
// config file only, never the DB) and is passed in by the caller, same as
// Config itself - this package still has no DB dependency of its own.
type WritebackConfig struct {
	BindDN   string
	Password string
}

// ChangePassword looks up username's DN using cfg's ordinary search/bind
// service account (same lookup as Authenticate), then rebinds on a fresh
// connection as wb's elevated identity to perform the actual password
// write. The write itself is server-specific, driven by cfg.ServerType:
//   - "generic" (OpenLDAP-compatible): the RFC 3062 password-modify
//     extended operation, which lets the server pick the correct password
//     hash scheme itself.
//   - "ad": Active Directory doesn't implement RFC 3062. The password must
//     be written directly to the unicodePwd attribute, UTF-16LE-encoded
//     and quoted, which AD refuses outside an encrypted connection - so
//     TLSMode == "none" is rejected up front with a clear error instead of
//     an opaque failure from the wire.
func ChangePassword(cfg Config, wb WritebackConfig, username, newPassword string) error {
	if wb.BindDN == "" {
		return fmt.Errorf("ldap: password write-back is enabled but no ldap_writeback.bind_dn is configured")
	}

	conn, err := connect(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return fmt.Errorf("ldap: service-account bind failed: %w", err)
	}

	filter := fmt.Sprintf(cfg.UserFilter, ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(
		cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		filter, []string{"dn"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return fmt.Errorf("ldap: user search failed: %w", err)
	}
	if len(res.Entries) == 0 {
		return fmt.Errorf("ldap: no user matched filter %q", filter)
	}
	if len(res.Entries) > 1 {
		return fmt.Errorf("ldap: user filter %q matched %d entries, expected 1 - check ldap_user_filter", filter, len(res.Entries))
	}
	userDN := res.Entries[0].DN

	wbConn, err := connect(cfg)
	if err != nil {
		return err
	}
	defer wbConn.Close()
	if err := wbConn.Bind(wb.BindDN, wb.Password); err != nil {
		return fmt.Errorf("ldap: write-back account bind failed: %w", err)
	}

	if cfg.ServerType == "ad" {
		return changePasswordAD(wbConn, cfg, userDN, newPassword)
	}
	if _, err := wbConn.PasswordModify(ldap.NewPasswordModifyRequest(userDN, "", newPassword)); err != nil {
		return fmt.Errorf("ldap: password-modify failed: %w", err)
	}
	return nil
}

// changePasswordAD implements Active Directory's unicodePwd convention: the
// attribute value is the new password, quoted, then UTF-16LE-encoded - not
// plaintext, and not any of the hash schemes OpenLDAP/RFC 3062 use.
func changePasswordAD(conn *ldap.Conn, cfg Config, userDN, newPassword string) error {
	if cfg.TLSMode == "none" {
		return fmt.Errorf("ldap: AD password change requires an encrypted connection (starttls or ldaps), current ldap_tls_mode is 'none'")
	}
	modReq := ldap.NewModifyRequest(userDN, nil)
	modReq.Replace("unicodePwd", []string{utf16LEQuoted(newPassword)})
	if err := conn.Modify(modReq); err != nil {
		return fmt.Errorf("ldap: AD password change failed: %w", err)
	}
	return nil
}

// utf16LEQuoted encodes s (wrapped in double quotes, per AD's unicodePwd
// convention) as UTF-16LE, returned as a string so it can be passed through
// go-ldap's string-typed attribute value API - the bytes themselves are
// never valid UTF-8, but ModifyRequest.Replace only cares about the raw
// bytes it sends over the wire, not that they form a valid Go string.
func utf16LEQuoted(s string) string {
	units := utf16.Encode([]rune(`"` + s + `"`))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return string(buf)
}

// NormalizeDN lowercases and trims a DN for case-insensitive comparison
// against LdapRoleMapping.GroupDN - AD/LDAP DN string comparison is
// case-insensitive per RFC 4514 for the attribute types in normal use here.
func NormalizeDN(dn string) string {
	return strings.ToLower(strings.TrimSpace(dn))
}

// Entry is one child node returned by Browse - enough for a tree-browser UI
// to render a label, know whether to offer it as a selectable group, and
// request its own children on expand (by DN).
type Entry struct {
	DN            string
	RDN           string // display label - the value of the DN's leftmost attribute, e.g. "Admins"
	ObjectClasses []string
	IsGroup       bool
}

// groupObjectClasses are the objectClass values (AD and the common OpenLDAP
// schemas) that mark an entry as a group rather than an OU/container/user -
// used to flag Entry.IsGroup so a tree browser can distinguish "selectable
// group" from "container to expand" without the caller needing its own
// schema knowledge.
var groupObjectClasses = map[string]bool{
	"group":              true, // Active Directory
	"groupofnames":       true, // OpenLDAP
	"groupofuniquenames": true, // OpenLDAP
	"posixgroup":         true, // OpenLDAP/RFC 2307
}

func isGroupEntry(objectClasses []string) bool {
	for _, oc := range objectClasses {
		if groupObjectClasses[strings.ToLower(oc)] {
			return true
		}
	}
	return false
}

// rdnValue extracts the value of dn's leftmost RDN attribute for display,
// e.g. "Admins" from "CN=Admins,OU=Groups,DC=example,DC=com". Falls back to
// the raw dn if it doesn't parse, so a malformed entry still renders as
// something rather than being dropped.
func rdnValue(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil || len(parsed.RDNs) == 0 || len(parsed.RDNs[0].Attributes) == 0 {
		return dn
	}
	return parsed.RDNs[0].Attributes[0].Value
}

// Browse lists the immediate children of dn (or cfg.BaseDN if dn is blank)
// for a tree-browser UI - used so an admin setting up a group->role mapping
// can navigate the directory and click the group they mean instead of
// having to know/type its exact DN. Binds as the configured service account,
// same as TestConnection/Authenticate.
func Browse(cfg Config, dn string) ([]Entry, error) {
	conn, err := connect(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return nil, fmt.Errorf("ldap: service-account bind failed: %w", err)
	}

	base := strings.TrimSpace(dn)
	if base == "" {
		base = cfg.BaseDN
	}

	req := ldap.NewSearchRequest(
		base, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=*)", []string{"objectClass"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ldap: browse search under %q failed: %w", base, err)
	}

	entries := make([]Entry, 0, len(res.Entries))
	for _, e := range res.Entries {
		classes := e.GetEqualFoldAttributeValues("objectClass")
		entries = append(entries, Entry{
			DN:            e.DN,
			RDN:           rdnValue(e.DN),
			ObjectClasses: classes,
			IsGroup:       isGroupEntry(classes),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].RDN) < strings.ToLower(entries[j].RDN)
	})
	return entries, nil
}
