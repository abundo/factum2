package web

import (
	"fmt"
	"log"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// ldapServerRows lists external directory servers. using is false when the
// table is empty, which keeps the Settings LDAP columns as the directory
// for databases that have not stored a server row yet. A non-empty table
// is the only source, including when every row is disabled.
func ldapServerRows(db *gorm.DB, enabledOnly bool) (rows []models.LdapServer, using bool, err error) {
	var n int64
	if err = db.Model(&models.LdapServer{}).Count(&n).Error; err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	q := db.Order("position, id")
	if enabledOnly {
		q = q.Where("enabled", true)
	}
	err = q.Find(&rows).Error
	return rows, true, err
}

// ldapAuthConfigs is the failover list for a login. Enabled servers with a
// host are tried in position order. An empty table falls back to Settings
// when LDAP is enabled there.
func ldapAuthConfigs(db *gorm.DB) ([]ldapauth.Config, error) {
	rows, using, err := ldapServerRows(db, true)
	if err != nil {
		return nil, err
	}
	if using {
		out := make([]ldapauth.Config, 0, len(rows))
		for i := range rows {
			if rows[i].Host == "" {
				continue
			}
			out = append(out, ldapauth.ConfigFromServer(&rows[i]))
		}
		return out, nil
	}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		return nil, err
	}
	if settings.LdapEnabled == nil || !*settings.LdapEnabled {
		return nil, nil
	}
	return []ldapauth.Config{ldapauth.ConfigFromSettings(settings)}, nil
}

// authenticateLDAP tries each configured server. The first success wins.
// A dial error, a rejected bind, or a missing user moves on to the next
// server. When none succeeds the client is denied: ok and err are both
// false/nil, including when every server was unreachable.
func authenticateLDAP(db *gorm.DB, username, password string) (bool, *ldapauth.UserInfo, error) {
	cfgs, err := ldapAuthConfigs(db)
	if err != nil {
		return false, nil, err
	}
	if len(cfgs) == 0 {
		return false, nil, nil
	}
	for _, cfg := range cfgs {
		ok, info, err := ldapAuthenticate(cfg, username, password)
		if err != nil {
			log.Printf("LDAP server %s failed for '%s': %v", cfg.Host, username, err)
			continue
		}
		if ok {
			return true, info, nil
		}
		log.Printf("LDAP server %s rejected '%s'", cfg.Host, username)
	}
	log.Printf("Authentication failed: no LDAP server accepted '%s'", username)
	return false, nil, nil
}

// lookupLDAPByEmail searches enabled servers until one returns the address.
// A server that is down is skipped. No match is (nil, nil).
func lookupLDAPByEmail(db *gorm.DB, email string) (*ldapauth.UserInfo, error) {
	cfgs, err := ldapAuthConfigs(db)
	if err != nil {
		return nil, err
	}
	var last error
	answered := false
	for _, cfg := range cfgs {
		info, err := ldapauth.LookupByEmail(cfg, email)
		if err != nil {
			log.Printf("LDAP email lookup on %s failed: %v", cfg.Host, err)
			last = err
			continue
		}
		answered = true
		if info != nil {
			return info, nil
		}
	}
	if !answered && last != nil {
		return nil, last
	}
	return nil, nil
}

// changeLDAPPassword writes the new password on the first enabled server
// that allows password changes and accepts the write. Later servers are
// tried when an earlier one fails.
func changeLDAPPassword(db *gorm.DB, wb ldapauth.WritebackConfig, username, newPassword string) error {
	rows, using, err := ldapServerRows(db, true)
	if err != nil {
		return err
	}
	if !using {
		settings, err := util.GetOrCreateSettings(db)
		if err != nil {
			return err
		}
		if settings.LdapAllowPasswordChange == nil || !*settings.LdapAllowPasswordChange {
			return fmt.Errorf("your password is managed by your organization's directory service; contact your administrator to change it")
		}
		return ldapauth.ChangePassword(ldapauth.ConfigFromSettings(settings), wb, username, newPassword)
	}
	var last error
	tried := false
	for i := range rows {
		if rows[i].AllowPasswordChange == nil || !*rows[i].AllowPasswordChange || rows[i].Host == "" {
			continue
		}
		tried = true
		err := ldapauth.ChangePassword(ldapauth.ConfigFromServer(&rows[i]), wb, username, newPassword)
		if err == nil {
			return nil
		}
		log.Printf("LDAP password change on %s failed for '%s': %v", rows[i].Host, username, err)
		last = err
	}
	if !tried {
		return fmt.Errorf("your password is managed by your organization's directory service; contact your administrator to change it")
	}
	if last == nil {
		last = fmt.Errorf("ldap: password change failed")
	}
	return last
}

// ldapPasswordChangeAllowed reports whether a self-service password write
// can be attempted: the write-back account is configured and at least one
// enabled server (or the Settings fallback) allows it.
func ldapPasswordChangeAllowed(db *gorm.DB) (bool, error) {
	if util.Config == nil || util.Config.LdapWriteback.BindDN == "" {
		return false, nil
	}
	rows, using, err := ldapServerRows(db, true)
	if err != nil {
		return false, err
	}
	if !using {
		settings, err := util.GetOrCreateSettings(db)
		if err != nil {
			return false, err
		}
		return settings.LdapEnabled != nil && *settings.LdapEnabled &&
			settings.LdapAllowPasswordChange != nil && *settings.LdapAllowPasswordChange, nil
	}
	for i := range rows {
		if rows[i].AllowPasswordChange != nil && *rows[i].AllowPasswordChange && rows[i].Host != "" {
			return true, nil
		}
	}
	return false, nil
}
