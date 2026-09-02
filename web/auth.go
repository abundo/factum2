package web

import (
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
	"github.com/abundo/factum2/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ldapAuthenticate abstracts internal/ldapauth.Authenticate so
// AuthenticateUser stays unit-testable without a real LDAP/AD server - tests
// reassign this package var to a stub and restore it via t.Cleanup.
var ldapAuthenticate = ldapauth.Authenticate

// jwtKey signs and verifies the "token" auth cookie. It's set once at
// startup from util.Config.Web.JWTSecret (see web.go's GUI()) rather than
// hardcoded here: a secret checked into source would let anyone who reads
// it forge a valid token for any user_id, including an admin.
var jwtKey []byte

// jwtSigningKey returns the HMAC key for the "token" cookie. There is no
// default, including under APP_ENV=development: a hardcoded fallback would
// let anyone who reads the source forge a valid token for any user.
func jwtSigningKey(secret string) ([]byte, error) {
	if secret == "" {
		return nil, errors.New("missing web.jwtsecret in configuration")
	}
	return []byte(secret), nil
}

func GenerateJWT(userID uint) (string, error) {
	// Create the Claims
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtKey)
}

// checkServiceToken authenticates a server-to-server caller (no browser, no
// session cookie - e.g. internal/factum's HTTP client, used by
// factum2-dns/factum2-librenms-cli which may run on a different host than the
// primary) via "Authorization: Bearer <token>", matched against
// Settings.FactumApiToken. An unconfigured (empty) token never matches, even
// against an empty/missing header, so leaving it unset doesn't silently
// disable auth; the comparison is constant-time to avoid leaking the token
// one byte at a time through response-timing.
func (ctrl *Controller) checkServiceToken(c *echo.Context) bool {
	const prefix = "Bearer "
	auth := c.Request().Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	presented := strings.TrimPrefix(auth, prefix)
	if presented == "" {
		return false
	}
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil || settings.FactumApiToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(settings.FactumApiToken)) == 1
}

// RequireAPIAuth identifies the caller either from the "token" cookie (sets
// "user" in the context) or a service token (see checkServiceToken; sets
// "auth_method" to "token" instead, since there's no user behind it),
// responding with a 401 JSON body on failure - API clients can't follow a
// redirect into an HTML page, which is why this doesn't redirect to /login
// like the legacy form-POST handlers do.
func (ctrl *Controller) RequireAPIAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if worker.IsHubAuth(c.Request().Context()) {
			c.Set("auth_method", "token")
			return next(c)
		}

		if ctrl.checkServiceToken(c) {
			c.Set("auth_method", "token")
			return next(c)
		}

		cookie, err := c.Cookie("token")
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
		}

		token, err := jwt.Parse(cookie.Value, func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, echo.ErrUnauthorized
			}
			return jwtKey, nil
		})
		if err != nil || !token.Valid {
			return c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid token"})
		}

		claims, _ := token.Claims.(jwt.MapClaims)
		userID := claims["user_id"]

		var user models.User
		if err := ctrl.DB.Preload("Roles").First(&user, userID).Error; err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]any{"error": "user no longer exists"})
		}
		c.Set("auth_method", "session")
		c.Set("user", user)

		return next(c)
	}
}

// RequireAdmin rejects the request unless the authenticated user (set by RequireAPIAuth)
// has the "admin" role. Must run after RequireAPIAuth.
func (ctrl *Controller) RequireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		user, ok := c.Get("user").(models.User)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
		}
		if !user.HasRole("admin") {
			return c.JSON(http.StatusForbidden, map[string]any{"error": "admin permission required"})
		}
		return next(c)
	}
}

// RequireAdminOrServiceToken allows either an admin user (session cookie) or
// a valid service token through - for endpoints a remote CLI needs to reach
// non-interactively (see checkServiceToken) but which still carry
// admin-level secrets (DB credentials, API tokens), so a plain logged-in
// non-admin user mustn't see them either. Must run after RequireAPIAuth.
func (ctrl *Controller) RequireAdminOrServiceToken(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if c.Get("auth_method") == "token" {
			return next(c)
		}
		user, ok := c.Get("user").(models.User)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
		}
		if !user.HasRole("admin") {
			return c.JSON(http.StatusForbidden, map[string]any{"error": "admin permission required"})
		}
		return next(c)
	}
}

// HashPassword generates a bcrypt hash of the password
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPasswordHash compares a plain text password with a stored hash
// Returns true if they are the same
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ldapSentinelPassword marks a local user whose credentials are checked
// against LDAP/AD instead of a bcrypt hash. Deliberately reuses the existing
// PasswordHash column (models.User.PasswordHash is gorm:"not null") rather
// than a new nullable column: "@LDAP" can never collide with a real bcrypt
// hash (those always start with "$2a$"/"$2b$"/"$2y$"), and if it's ever
// accidentally compared as one (e.g. LDAP disabled while a sentinel user
// still exists), bcrypt.CompareHashAndPassword simply errors and
// CheckPasswordHash returns false - fails safe with no special casing
// needed.
const ldapSentinelPassword = "@LDAP"

// AuthenticateUser checks username and password, trying local auth first and
// falling back to LDAP/AD per the rules below. Returns the user object on
// success, (nil, nil) on any kind of "bad credentials" (mirrors the
// pre-LDAP behavior other callers already depend on), and (nil, err) only
// for a real infrastructure problem (DB error, or LDAP server
// unreachable/misconfigured).
//
//  1. Local user found, PasswordHash == ldapSentinelPassword: authenticate
//     against LDAP/AD instead of bcrypt (authenticateExistingLDAPUser).
//  2. Local user found, PasswordHash is a normal bcrypt hash: unchanged
//     bcrypt check.
//  3. No local user found: try LDAP/AD; on success, auto-provision a local
//     user with PasswordHash = ldapSentinelPassword so subsequent logins
//     take path 1 (authenticateNewLDAPUser).
func AuthenticateUser(db *gorm.DB, username string, password string) (*models.User, error) {
	var user models.User
	// Eager load roles when fetching the user
	err := db.Preload("Roles").Where("username = ?", username).First(&user).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("Database error during authentication for user '%s': %v", username, err)
			return nil, err // Database error
		}
		return authenticateNewLDAPUser(db, username, password)
	}

	if user.PasswordHash == ldapSentinelPassword {
		return authenticateExistingLDAPUser(db, &user, password)
	}

	if CheckPasswordHash(password, user.PasswordHash) {
		log.Printf("User '%s' authenticated successfully", username)
		return &user, nil
	}

	log.Printf("Authentication failed: Invalid password for user '%s'", username)
	return nil, nil // Invalid password
}

// ldapSettingsIfEnabled loads Settings and reports whether LDAP auth is
// turned on - a DB error here is real (propagated), an unconfigured/disabled
// LDAP is not (callers treat it like "no such backend", not an error).
func ldapSettingsIfEnabled(db *gorm.DB) (*models.Settings, bool, error) {
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		return nil, false, err
	}
	return settings, settings.LdapEnabled != nil && *settings.LdapEnabled, nil
}

// authenticateNewLDAPUser handles AuthenticateUser's "no local user found"
// path: if LDAP is enabled and accepts the credentials, a local user is
// auto-provisioned (PasswordHash = ldapSentinelPassword) and its roles are
// computed from the directory's group membership immediately, same as any
// later login.
func authenticateNewLDAPUser(db *gorm.DB, username, password string) (*models.User, error) {
	settings, enabled, err := ldapSettingsIfEnabled(db)
	if err != nil {
		return nil, err
	}
	if !enabled {
		log.Printf("Authentication failed: User '%s' not found", username)
		return nil, nil
	}

	ok, info, err := ldapAuthenticate(ldapauth.ConfigFromSettings(settings), username, password)
	if err != nil {
		log.Printf("LDAP error while authenticating unknown user '%s': %v", username, err)
		return nil, err
	}
	if !ok {
		log.Printf("Authentication failed: LDAP rejected credentials for '%s'", username)
		return nil, nil
	}

	name := info.DisplayName
	if name == "" {
		name = username
	}
	user := models.User{
		Username:     username,
		PasswordHash: ldapSentinelPassword,
		Name:         name,
		Email:        info.Email,
		Mobile:       info.Mobile,
	}
	if err := db.Create(&user).Error; err != nil {
		return nil, err
	}
	if err := syncLDAPRoles(db, &user, settings, info.Groups); err != nil {
		return nil, err
	}
	log.Printf("User '%s' authenticated via LDAP and auto-provisioned locally", username)
	return &user, nil
}

// authenticateExistingLDAPUser handles AuthenticateUser's sentinel-hash path
// for an already-provisioned LDAP user: re-verifies against LDAP/AD, and -
// critically - re-syncs Name/Email/Mobile/Roles on every successful login so
// a directory change propagates without any local admin action, exactly
// like a freshly auto-provisioned user.
func authenticateExistingLDAPUser(db *gorm.DB, user *models.User, password string) (*models.User, error) {
	settings, enabled, err := ldapSettingsIfEnabled(db)
	if err != nil {
		return nil, err
	}
	if !enabled {
		log.Printf("Authentication failed: user '%s' is LDAP-backed but LDAP auth is disabled", user.Username)
		return nil, nil
	}

	ok, info, err := ldapAuthenticate(ldapauth.ConfigFromSettings(settings), user.Username, password)
	if err != nil {
		log.Printf("LDAP error while authenticating '%s': %v", user.Username, err)
		return nil, err
	}
	if !ok {
		log.Printf("Authentication failed: LDAP rejected credentials for '%s'", user.Username)
		return nil, nil
	}

	if info.DisplayName != "" {
		user.Name = info.DisplayName
	}
	user.Email = info.Email
	user.Mobile = info.Mobile
	if err := db.Save(user).Error; err != nil {
		return nil, err
	}
	if err := syncLDAPRoles(db, user, settings, info.Groups); err != nil {
		return nil, err
	}
	log.Printf("User '%s' authenticated via LDAP", user.Username)
	return user, nil
}

// syncLDAPRoles recomputes user.Roles from the LDAP group DNs the directory
// just returned, against the admin-configured LdapRoleMapping table,
// falling back to Settings.LdapDefaultRoleID when no mapping matches. This
// *replaces* the user's role association wholesale, deliberately: a
// manually-granted role on an LDAP-backed user would otherwise "stick"
// forever regardless of AD group membership, defeating the point of syncing
// on every login. Runs from both authenticateNewLDAPUser and
// authenticateExistingLDAPUser, so it fires on every successful LDAP login,
// not just at account creation.
func syncLDAPRoles(db *gorm.DB, user *models.User, settings *models.Settings, groupDNs []string) error {
	var mappings []models.LdapRoleMapping
	if err := db.Find(&mappings).Error; err != nil {
		return err
	}

	groupSet := make(map[string]bool, len(groupDNs))
	for _, g := range groupDNs {
		groupSet[ldapauth.NormalizeDN(g)] = true
	}

	roleIDs := map[uint]bool{}
	for _, m := range mappings {
		if groupSet[ldapauth.NormalizeDN(m.GroupDN)] {
			roleIDs[m.RoleID] = true
		}
	}
	if len(roleIDs) == 0 && settings.LdapDefaultRoleID != nil {
		roleIDs[*settings.LdapDefaultRoleID] = true
	}

	ids := make([]uint, 0, len(roleIDs))
	for id := range roleIDs {
		ids = append(ids, id)
	}
	var roles []*models.Role
	if len(ids) > 0 {
		if err := db.Find(&roles, ids).Error; err != nil {
			return err
		}
	}
	if err := db.Model(user).Association("Roles").Replace(roles); err != nil {
		return err
	}
	user.Roles = roles
	return nil
}

// GetCurrentUser retrieves the authenticated user from the context.
// Returns nil if the user is not authenticated or not found in the context.
func GetCurrentUser(DB *gorm.DB, c *echo.Context) *models.User {
	userID := c.Get("user_id")

	var user *models.User
	if DB.Preload("Roles").First(&user, userID).Error != nil {
		return nil
	}
	return user
}

// GetUserByID fetches a user by ID, preloading roles
func GetUserByID(db *gorm.DB, userID uint) (*models.User, error) {
	var user models.User
	if err := db.Preload("Roles").First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Not found
		}
		return nil, err // Other DB error
	}
	return &user, nil
}

// RequireWrite allows the "admin" or "operator" role - the only roles
// permitted to create/update/delete non-admin resources (customer, contact,
// service, device interfaces, sync triggers, ...). A user with no role, or
// only "viewer", is rejected with 403. A valid service token (auth_method
// "token", set by RequireAPIAuth) is also allowed - remote CLIs such as
// factum2-device-sync have no session user. Must run after RequireAPIAuth.
func (ctrl *Controller) RequireWrite(next echo.HandlerFunc) echo.HandlerFunc {
	return requireAnyRole(next, "admin", "operator")
}

// RequireRead allows the "admin", "operator" or "viewer" role - any
// authenticated user granted at least read access to non-admin resources. A
// user with no role at all is rejected with 403, leaving them only the
// routes that don't use RequireRead/RequireWrite: the dashboard
// (GET /api/links) and their own profile (GET/PUT /api/me). A valid service
// token is also allowed (same rationale as RequireWrite - e.g.
// FactumClient.GetDeviceByName). Must run after RequireAPIAuth.
func (ctrl *Controller) RequireRead(next echo.HandlerFunc) echo.HandlerFunc {
	return requireAnyRole(next, "admin", "operator", "viewer")
}

// requireAnyRole rejects the request unless the caller is a service token
// (auth_method "token" - full-trust server-to-server credential, no user to
// check a role on) or an authenticated user holding at least one of roles.
func requireAnyRole(next echo.HandlerFunc, roles ...string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// Service tokens pass RequireAPIAuth with auth_method "token" but
		// no "user". Without this branch, every RequireRead/RequireWrite
		// route 401s token callers - which is exactly how factum2-device-sync
		// and other remote CLIs authenticate (see checkServiceToken).
		if c.Get("auth_method") == "token" {
			return next(c)
		}
		user, ok := c.Get("user").(models.User)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
		}
		if !slices.ContainsFunc(roles, user.HasRole) {
			log.Printf("Forbidden: User '%s' (ID: %d) does not have any required role (%v). User roles: %v",
				user.Username, user.ID, roles, user.Roles)
			return c.JSON(http.StatusForbidden, map[string]any{"error": "permission required"})
		}
		return next(c)
	}
}

// // AuthenticateUser checks username and password against the database
// // Returns the user object on success, nil otherwise
// func AuthenticateUser(db *gorm.DB, username, password string) (*models.User, error) {
// 	var user models.User
// 	// Eager load roles when fetching the user
// 	if err := db.Preload("Roles").Where("username = ?", username).First(&user).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			log.Printf("Authentication failed: User '%s' not found", username)
// 			return nil, nil // User not found is not a server error, just auth failure
// 		}
// 		log.Printf("Database error during authentication for user '%s': %v", username, err)
// 		return nil, err // Database error
// 	}

// 	if CheckPasswordHash(password, user.PasswordHash) {
// 		log.Printf("User '%s' authenticated successfully", username)
// 		return &user, nil
// 	}

// 	log.Printf("Authentication failed: Invalid password for user '%s'", username)
// 	return nil, nil // Invalid password
// }

// // GetUserByID fetches a user by ID, preloading roles
// func GetUserByID(db *gorm.DB, userID uint) (*models.User, error) {
// 	var user models.User
// 	if err := db.Preload("Roles").First(&user, userID).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			return nil, nil // Not found
// 		}
// 		return nil, err // Other DB error
// 	}
// 	return &user, nil
// }

// const (
// 	UserKey     = "user"       // Key to store user model in context
// 	UserIDKey   = "user_id"    // Key for user ID in session
// 	UsernameKey = "username"   // Key for username in session
// 	RolesKey    = "user_roles" // Key for user roles in session (comma-separated)
// )

// // RequireAuth checks if a user is logged in via session.
// // If logged in, it fetches the user details and stores them in the context.
// // If not logged in, it redirects to the login page.
// func RequireAuth(db *gorm.DB, loginPath string) echo.MiddlewareFunc {
// 	return func(next echo.HandlerFunc) echo.HandlerFunc {
// 		return func(c *echo.Context) error {
// 			sess, _ := session.Get("session", c) // Error ignored for simplicity here, handle properly
// 			userIDVal := sess.Values[UserIDKey]

// 			if userIDVal == nil {
// 				log.Println("Auth required, but no user ID in session. Redirecting to login.")
// 				// Store the intended URL in the session so we can redirect back after login
// 				sess.Values["redirect_after_login"] = c.Request().URL.Path
// 				sess.Save(c.Request(), c.Response())
// 				return c.Redirect(http.StatusFound, loginPath)
// 			}

// 			userID, ok := userIDVal.(uint)
// 			if !ok {
// 				log.Printf("Auth required, but user ID in session is not uint: %T. Clearing session.", userIDVal)
// 				// Clear invalid session data
// 				sess.Options.MaxAge = -1
// 				sess.Save(c.Request(), c.Response())
// 				return c.Redirect(http.StatusFound, loginPath)
// 			}

// 			// Fetch user details from DB to ensure they still exist and have current roles
// 			// (Session roles could be stale)
// 			user, err := GetUserByID(db, userID)
// 			if err != nil {
// 				log.Printf("Error fetching user (ID: %d) from DB: %v. Clearing session.", userID, err)
// 				sess.Options.MaxAge = -1
// 				sess.Save(c.Request(), c.Response())
// 				return c.Redirect(http.StatusFound, loginPath) // Or show an error page
// 			}
// 			if user == nil {
// 				log.Printf("User (ID: %d) found in session but not in DB. Clearing session.", userID)
// 				// User might have been deleted
// 				sess.Options.MaxAge = -1
// 				sess.Save(c.Request(), c.Response())
// 				return c.Redirect(http.StatusFound, loginPath)
// 			}

// 			// Store the user object in the context for downstream handlers
// 			c.Set(UserKey, user)
// 			log.Printf("User '%s' (ID: %d) authenticated via session.", user.Username, user.ID)

// 			return next(c)
// 		}
// 	}
// }

// // RequireRole checks if the authenticated user (from context) has at least one of the specified roles.
// // This middleware MUST run AFTER RequireAuth.
// func RequireRole(requiredRoles ...string) echo.MiddlewareFunc {
// 	return func(next echo.HandlerFunc) echo.HandlerFunc {
// 		return func(c *echo.Context) error {
// 			userVal := c.Get(UserKey)
// 			if userVal == nil {
// 				// This should not happen if RequireAuth runs first
// 				log.Println("Error: RequireRole executed before RequireAuth or user not set in context.")
// 				return echo.NewHTTPError(http.StatusInternalServerError, "Authorization context not properly set up.")
// 			}

// 			user, ok := userVal.(*models.User)
// 			if !ok {
// 				log.Printf("Error: User object in context has unexpected type: %T", userVal)
// 				return echo.NewHTTPError(http.StatusInternalServerError, "Authorization context has invalid data.")
// 			}

// 			userRoles := make(map[string]bool)
// 			for _, role := range user.Roles {
// 				userRoles[role.Name] = true
// 			}

// 			hasRequiredRole := false
// 			for _, reqRole := range requiredRoles {
// 				if userRoles[reqRole] {
// 					hasRequiredRole = true
// 					break
// 				}
// 			}

// 			if !hasRequiredRole {
// 				log.Printf("Forbidden: User '%s' (ID: %d) does not have required roles (%v). User roles: %v",
// 					user.Username, user.ID, requiredRoles, user.Roles) // Be careful logging roles in production
// 				// You could render a custom forbidden page
// 				// return c.Render(http.StatusForbidden, "forbidden.html", map[string]interface{}{"Username": user.Username})
// 				return echo.NewHTTPError(http.StatusForbidden, "You do not have permission to access this resource.")
// 			}

// 			log.Printf("Authorization successful for user '%s' (ID: %d) for roles (%v).", user.Username, user.ID, requiredRoles)
// 			return next(c)
// 		}
// 	}
// }
