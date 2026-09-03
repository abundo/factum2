package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMain gives every test in this file a fixed jwtKey. In production it's
// set once at startup from util.Config.Web.JWTSecret (see GUI() in web.go);
// tests bypass that plumbing since they never call GUI().
func TestMain(m *testing.M) {
	jwtKey = []byte("test-secret-key-do-not-use-in-prod")
	m.Run()
}

func TestJWTSigningKey(t *testing.T) {
	t.Run("empty secret is an error", func(t *testing.T) {
		key, err := jwtSigningKey("")
		if err == nil {
			t.Fatal("expected error for empty web.jwtsecret")
		}
		if key != nil {
			t.Errorf("key = %q, want nil", key)
		}
	})

	t.Run("configured secret is used as-is", func(t *testing.T) {
		const secret = "a-configured-secret"
		key, err := jwtSigningKey(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(key) != secret {
			t.Errorf("key = %q, want %q", key, secret)
		}
	})
}

// TestGUIRequiresJWTSecret is the issue #14 regression: APP_ENV=development
// used to fall back to a hardcoded signing key. GUI must refuse to start
// without web.jwtsecret in every environment, and must do so before
// opening a database connection.
func TestGUIRequiresJWTSecret(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	origKey := jwtKey
	origConfig := util.Config
	origSecure := secureCookies
	t.Cleanup(func() {
		jwtKey = origKey
		util.Config = origConfig
		secureCookies = origSecure
	})

	err := GUI(&GuiParams{})
	if err == nil {
		t.Fatal("expected GUI to refuse to start without web.jwtsecret, even in development")
	}
}

// newTestDB returns an in-memory SQLite DB with the same schema as
// production (via util.MigrateDatabase), so auth tests don't need a live
// Postgres. "cache=shared" + a single open connection keeps the in-memory
// data visible across the pooled connections gorm may otherwise use - a
// plain ":memory:" DSN would give each connection its own empty database.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

	if err := util.MigrateDatabase(db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return db
}

// createTestUser inserts a user with a real bcrypt hash of password, plus
// the "admin" role if admin is true. Roles are created on first use since
// the schema doesn't seed any.
func createTestUser(t *testing.T, db *gorm.DB, username, password string, admin bool) models.User {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := models.User{Username: username, PasswordHash: hash, Name: username}
	if admin {
		var role models.Role
		if err := db.Where("name = ?", "admin").FirstOrCreate(&role, models.Role{Name: "admin"}).Error; err != nil {
			t.Fatalf("create admin role: %v", err)
		}
		user.Roles = []*models.Role{&role}
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user
}

// createTestUserWithRoles is createTestUser generalized to an arbitrary set
// of role names (e.g. "operator", "viewer"), for RequireRead/RequireWrite
// tests that need roles other than "admin". No roles at all is valid - it's
// the "no role" case those tests must also cover.
func createTestUserWithRoles(t *testing.T, db *gorm.DB, username, password string, roleNames ...string) models.User {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := models.User{Username: username, PasswordHash: hash, Name: username}
	for _, name := range roleNames {
		var role models.Role
		if err := db.Where("name = ?", name).FirstOrCreate(&role, models.Role{Name: name}).Error; err != nil {
			t.Fatalf("create %s role: %v", name, err)
		}
		user.Roles = append(user.Roles, &role)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user
}

func signToken(t *testing.T, key []byte, userID any, exp time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     exp.Unix(),
		"iat":     time.Now().Unix(),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tok
}

// echoRequest builds an Echo context for a GET request, optionally carrying
// a "token" cookie, and returns it alongside the ResponseRecorder so tests
// can assert on the status code the middleware produced.
func echoRequest(cookieValue string) (*echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "token", Value: cookieValue})
	}
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

// echoRequestWithAuthHeader is like echoRequest but sets a raw Authorization
// header instead of a cookie, for exercising checkServiceToken.
func echoRequestWithAuthHeader(authHeader string) (*echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

func noopOK(c *echo.Context) error { return c.NoContent(http.StatusOK) }

func TestRequireAPIAuth(t *testing.T) {
	db := newTestDB(t)
	user := createTestUser(t, db, "alice", "correct-password", false)
	ctrl := &Controller{DB: db}

	t.Run("no cookie", func(t *testing.T) {
		c, rec := echoRequest("")
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("garbage token", func(t *testing.T) {
		c, rec := echoRequest("not-a-jwt")
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("wrong signing key", func(t *testing.T) {
		forged := signToken(t, []byte("attacker-controlled-key"), user.ID, time.Now().Add(time.Hour))
		c, rec := echoRequest(forged)
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d - a token not signed with jwtKey must be rejected", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		expired := signToken(t, jwtKey, user.ID, time.Now().Add(-time.Hour))
		c, rec := echoRequest(expired)
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("user no longer exists", func(t *testing.T) {
		tok := signToken(t, jwtKey, uint(999999), time.Now().Add(time.Hour))
		c, rec := echoRequest(tok)
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		tok, err := GenerateJWT(user.ID)
		if err != nil {
			t.Fatalf("GenerateJWT: %v", err)
		}
		c, rec := echoRequest(tok)

		var gotUser models.User
		handler := func(c *echo.Context) error {
			gotUser = c.Get("user").(models.User)
			return c.NoContent(http.StatusOK)
		}
		if err := ctrl.RequireAPIAuth(handler)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if gotUser.Username != "alice" {
			t.Errorf("context user = %q, want %q", gotUser.Username, "alice")
		}
	})
}

// setFactumApiToken stores token as Settings.FactumApiToken, creating the
// Settings row (id=1) if it doesn't exist yet.
func setFactumApiToken(t *testing.T, db *gorm.DB, token string) {
	t.Helper()
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("GetOrCreateSettings: %v", err)
	}
	settings.FactumApiToken = token
	if err := db.Save(settings).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}
}

func TestRequireAPIAuth_ServiceToken(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	setFactumApiToken(t, db, "correct-service-token")

	t.Run("valid token, no cookie needed", func(t *testing.T) {
		c, rec := echoRequestWithAuthHeader("Bearer correct-service-token")
		var gotAuthMethod any
		handler := func(c *echo.Context) error {
			gotAuthMethod = c.Get("auth_method")
			return c.NoContent(http.StatusOK)
		}
		if err := ctrl.RequireAPIAuth(handler)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if gotAuthMethod != "token" {
			t.Errorf(`auth_method = %v, want "token"`, gotAuthMethod)
		}
		if _, ok := c.Get("user").(models.User); ok {
			t.Error("service-token auth must not set a user in context")
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		c, rec := echoRequestWithAuthHeader("Bearer wrong-token")
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("missing Bearer prefix", func(t *testing.T) {
		c, rec := echoRequestWithAuthHeader("correct-service-token")
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

// TestRequireAPIAuth_ServiceToken_Unconfigured guards against an
// unconfigured (empty) FactumApiToken silently disabling auth - an empty
// presented token must never match an empty configured one.
func TestRequireAPIAuth_ServiceToken_Unconfigured(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	// FactumApiToken left at its zero value ("").

	t.Run("empty bearer token", func(t *testing.T) {
		c, rec := echoRequestWithAuthHeader("Bearer ")
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d - an unconfigured token must never authenticate", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("no Authorization header at all", func(t *testing.T) {
		c, rec := echoRequestWithAuthHeader("")
		if err := ctrl.RequireAPIAuth(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestRequireAdminOrServiceToken(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	admin := createTestUser(t, db, "admin-user", "pw", true)
	plain := createTestUser(t, db, "plain-user", "pw", false)
	setFactumApiToken(t, db, "correct-service-token")

	t.Run("service token, no user needed", func(t *testing.T) {
		c, rec := echoRequestWithAuthHeader("Bearer correct-service-token")
		c.Set("auth_method", "token")
		if err := ctrl.RequireAdminOrServiceToken(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("session admin", func(t *testing.T) {
		c, rec := echoRequest("")
		c.Set("auth_method", "session")
		c.Set("user", admin)
		if err := ctrl.RequireAdminOrServiceToken(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("session non-admin rejected", func(t *testing.T) {
		c, rec := echoRequest("")
		c.Set("auth_method", "session")
		c.Set("user", plain)
		if err := ctrl.RequireAdminOrServiceToken(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d - a non-admin session must not reach admin-level secrets", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("unauthenticated rejected", func(t *testing.T) {
		c, rec := echoRequest("")
		if err := ctrl.RequireAdminOrServiceToken(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestRequireAdmin(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	admin := createTestUser(t, db, "admin-user", "pw", true)
	plain := createTestUser(t, db, "plain-user", "pw", false)

	t.Run("no authenticated user in context", func(t *testing.T) {
		c, rec := echoRequest("")
		if err := ctrl.RequireAdmin(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("authenticated but not admin", func(t *testing.T) {
		c, rec := echoRequest("")
		c.Set("user", plain)
		if err := ctrl.RequireAdmin(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d - non-admin must not pass", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("admin", func(t *testing.T) {
		c, rec := echoRequest("")
		c.Set("user", admin)
		if err := ctrl.RequireAdmin(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestRequireWrite(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	admin := createTestUserWithRoles(t, db, "write-admin", "pw", "admin")
	operator := createTestUserWithRoles(t, db, "write-operator", "pw", "operator")
	viewer := createTestUserWithRoles(t, db, "write-viewer", "pw", "viewer")
	noRole := createTestUserWithRoles(t, db, "write-none", "pw")

	cases := []struct {
		name string
		user models.User
		want int
	}{
		{"admin", admin, http.StatusOK},
		{"operator", operator, http.StatusOK},
		{"viewer must not pass", viewer, http.StatusForbidden},
		{"no role must not pass", noRole, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := echoRequest("")
			c.Set("user", tc.user)
			if err := ctrl.RequireWrite(noopOK)(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}

	t.Run("no authenticated user in context", func(t *testing.T) {
		c, rec := echoRequest("")
		if err := ctrl.RequireWrite(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	// Remote CLIs authenticate with the service token and have no session
	// user - RequireWrite must not 401 them the way RequireAdmin used to.
	t.Run("service token, no user needed", func(t *testing.T) {
		c, rec := echoRequest("")
		c.Set("auth_method", "token")
		if err := ctrl.RequireWrite(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestRequireRead(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	admin := createTestUserWithRoles(t, db, "read-admin", "pw", "admin")
	operator := createTestUserWithRoles(t, db, "read-operator", "pw", "operator")
	viewer := createTestUserWithRoles(t, db, "read-viewer", "pw", "viewer")
	noRole := createTestUserWithRoles(t, db, "read-none", "pw")

	cases := []struct {
		name string
		user models.User
		want int
	}{
		{"admin", admin, http.StatusOK},
		{"operator", operator, http.StatusOK},
		{"viewer", viewer, http.StatusOK},
		{"no role must not pass", noRole, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := echoRequest("")
			c.Set("user", tc.user)
			if err := ctrl.RequireRead(noopOK)(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}

	t.Run("no authenticated user in context", func(t *testing.T) {
		c, rec := echoRequest("")
		if err := ctrl.RequireRead(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	// Regression: factum2-device-sync's GET /api/device/name/:name is gated
	// by RequireRead; a valid service token must pass even without a user.
	t.Run("service token, no user needed", func(t *testing.T) {
		c, rec := echoRequest("")
		c.Set("auth_method", "token")
		if err := ctrl.RequireRead(noopOK)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct-horse-battery-staple" {
		t.Fatal("HashPassword returned the plaintext password unchanged")
	}
	if !CheckPasswordHash("correct-horse-battery-staple", hash) {
		t.Error("CheckPasswordHash rejected the correct password")
	}
	if CheckPasswordHash("wrong-password", hash) {
		t.Error("CheckPasswordHash accepted an incorrect password")
	}
}

func TestAuthenticateUser(t *testing.T) {
	db := newTestDB(t)
	createTestUser(t, db, "bob", "s3cret", false)

	t.Run("correct credentials", func(t *testing.T) {
		user, err := AuthenticateUser(db, "bob", "s3cret")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user == nil || user.Username != "bob" {
			t.Errorf("got %+v, want user \"bob\"", user)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		user, err := AuthenticateUser(db, "bob", "wrong")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user != nil {
			t.Errorf("got %+v, want nil for wrong password", user)
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		user, err := AuthenticateUser(db, "nobody", "whatever")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user != nil {
			t.Errorf("got %+v, want nil for unknown user", user)
		}
	})
}
