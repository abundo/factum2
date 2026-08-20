package web

import (
	"errors"
	"testing"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// stubLDAP replaces the ldapAuthenticate package var with fn for the
// duration of the test, restoring the original in t.Cleanup - lets
// AuthenticateUser's LDAP-backed paths be exercised without a real
// LDAP/AD server.
func stubLDAP(t *testing.T, fn func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error)) {
	t.Helper()
	orig := ldapAuthenticate
	ldapAuthenticate = fn
	t.Cleanup(func() { ldapAuthenticate = orig })
}

// enableLDAP flips Settings.LdapEnabled on, creating the Settings row if
// needed.
func enableLDAP(t *testing.T, db *gorm.DB) *models.Settings {
	t.Helper()
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("GetOrCreateSettings: %v", err)
	}
	enabled := true
	settings.LdapEnabled = &enabled
	if err := db.Save(settings).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}
	return settings
}

// createTestRole creates (or fetches) a Role by name.
func createTestRole(t *testing.T, db *gorm.DB, name string) models.Role {
	t.Helper()
	var role models.Role
	if err := db.Where("name = ?", name).FirstOrCreate(&role, models.Role{Name: name}).Error; err != nil {
		t.Fatalf("create role %q: %v", name, err)
	}
	return role
}

func TestAuthenticateUser_LDAPSentinel_Success(t *testing.T) {
	db := newTestDB(t)
	enableLDAP(t, db)
	role := createTestRole(t, db, "netops")
	if err := db.Create(&models.LdapRoleMapping{GroupDN: "CN=netops,OU=groups,DC=example,DC=com", RoleID: role.ID}).Error; err != nil {
		t.Fatalf("create mapping: %v", err)
	}
	user := models.User{Username: "carol", PasswordHash: ldapSentinelPassword, Name: "carol"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		if username != "carol" || password != "s3cret" {
			t.Fatalf("unexpected creds passed to LDAP: %q/%q", username, password)
		}
		return true, &ldapauth.UserInfo{
			DN:          "CN=carol,DC=example,DC=com",
			Email:       "carol@example.com",
			DisplayName: "Carol Directory",
			Mobile:      "+46701234567",
			Groups:      []string{"CN=netops,OU=groups,DC=example,DC=com"},
		}, nil
	})

	got, err := AuthenticateUser(db, "carol", "s3cret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil user, want carol")
	}
	if got.Name != "Carol Directory" || got.Email != "carol@example.com" || got.Mobile != "+46701234567" {
		t.Errorf("Name/Email/Mobile not synced from LDAP: %+v", got)
	}
	if len(got.Roles) != 1 || got.Roles[0].Name != "netops" {
		t.Errorf("Roles = %+v, want [netops]", got.Roles)
	}
}

func TestAuthenticateUser_LDAPSentinel_BadCreds(t *testing.T) {
	db := newTestDB(t)
	enableLDAP(t, db)
	user := models.User{Username: "carol", PasswordHash: ldapSentinelPassword, Name: "carol"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		return false, nil, nil
	})

	got, err := AuthenticateUser(db, "carol", "wrong")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil for bad LDAP credentials", got)
	}
}

func TestAuthenticateUser_LDAPSentinel_Disabled(t *testing.T) {
	db := newTestDB(t)
	// LDAP left disabled (Settings row not created / LdapEnabled nil).
	user := models.User{Username: "carol", PasswordHash: ldapSentinelPassword, Name: "carol"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		t.Fatal("ldapAuthenticate must not be called when LDAP is disabled")
		return false, nil, nil
	})

	got, err := AuthenticateUser(db, "carol", "whatever")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil when LDAP disabled", got)
	}
}

func TestAuthenticateUser_LDAPSentinel_ServerError(t *testing.T) {
	db := newTestDB(t)
	enableLDAP(t, db)
	user := models.User{Username: "carol", PasswordHash: ldapSentinelPassword, Name: "carol"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	wantErr := errors.New("dial: connection refused")
	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		return false, nil, wantErr
	})

	got, err := AuthenticateUser(db, "carol", "whatever")
	if err == nil {
		t.Fatal("expected an error to propagate, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil on infra error", got)
	}
}

func TestAuthenticateUser_AutoProvision_Success(t *testing.T) {
	db := newTestDB(t)
	enableLDAP(t, db)

	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		if username != "dave" {
			t.Fatalf("unexpected username: %q", username)
		}
		return true, &ldapauth.UserInfo{
			DN:          "CN=dave,DC=example,DC=com",
			Email:       "dave@example.com",
			DisplayName: "Dave Directory",
		}, nil
	})

	got, err := AuthenticateUser(db, "dave", "s3cret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil user, want auto-provisioned dave")
	}
	if got.PasswordHash != ldapSentinelPassword {
		t.Errorf("PasswordHash = %q, want sentinel", got.PasswordHash)
	}

	var stored models.User
	if err := db.Where("username = ?", "dave").First(&stored).Error; err != nil {
		t.Fatalf("expected a local user row to be created: %v", err)
	}
	if stored.PasswordHash != ldapSentinelPassword {
		t.Errorf("stored PasswordHash = %q, want sentinel", stored.PasswordHash)
	}
}

func TestAuthenticateUser_AutoProvision_Disabled(t *testing.T) {
	db := newTestDB(t)
	// LDAP left disabled.

	got, err := AuthenticateUser(db, "dave", "whatever")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil when LDAP disabled", got)
	}

	var count int64
	db.Model(&models.User{}).Where("username = ?", "dave").Count(&count)
	if count != 0 {
		t.Errorf("expected no user row created, found %d", count)
	}
}

func TestSyncLDAPRoles_DefaultRoleFallback(t *testing.T) {
	db := newTestDB(t)
	settings := enableLDAP(t, db)
	role := createTestRole(t, db, "readonly")
	settings.LdapDefaultRoleID = &role.ID
	if err := db.Save(settings).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}

	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		return true, &ldapauth.UserInfo{Groups: []string{"CN=unmapped,DC=example,DC=com"}}, nil
	})

	got, err := AuthenticateUser(db, "erin", "s3cret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil user")
	}
	if len(got.Roles) != 1 || got.Roles[0].Name != "readonly" {
		t.Errorf("Roles = %+v, want [readonly] (default fallback)", got.Roles)
	}
}

func TestSyncLDAPRoles_ReevaluatedOnEveryLogin(t *testing.T) {
	db := newTestDB(t)
	enableLDAP(t, db)
	roleA := createTestRole(t, db, "role-a")
	roleB := createTestRole(t, db, "role-b")
	if err := db.Create(&models.LdapRoleMapping{GroupDN: "CN=group-a,DC=example,DC=com", RoleID: roleA.ID}).Error; err != nil {
		t.Fatalf("create mapping A: %v", err)
	}
	if err := db.Create(&models.LdapRoleMapping{GroupDN: "CN=group-b,DC=example,DC=com", RoleID: roleB.ID}).Error; err != nil {
		t.Fatalf("create mapping B: %v", err)
	}

	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		return true, &ldapauth.UserInfo{Groups: []string{"CN=group-a,DC=example,DC=com"}}, nil
	})
	first, err := AuthenticateUser(db, "frank", "s3cret")
	if err != nil {
		t.Fatalf("unexpected error on first login: %v", err)
	}
	if len(first.Roles) != 1 || first.Roles[0].Name != "role-a" {
		t.Fatalf("first login roles = %+v, want [role-a]", first.Roles)
	}

	stubLDAP(t, func(cfg ldapauth.Config, username, password string) (bool, *ldapauth.UserInfo, error) {
		return true, &ldapauth.UserInfo{Groups: []string{"CN=group-b,DC=example,DC=com"}}, nil
	})
	second, err := AuthenticateUser(db, "frank", "s3cret")
	if err != nil {
		t.Fatalf("unexpected error on second login: %v", err)
	}
	if len(second.Roles) != 1 || second.Roles[0].Name != "role-b" {
		t.Errorf("second login roles = %+v, want exactly [role-b] (replaced, not accumulated)", second.Roles)
	}
}
