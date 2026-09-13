package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
)

func cookieByName(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("no %q cookie in response", name)
	return nil
}

func withSecureCookies(t *testing.T, on bool) {
	t.Helper()
	orig := secureCookies
	secureCookies = on
	t.Cleanup(func() { secureCookies = orig })
}

func TestSetAuthCookieSecureFlag(t *testing.T) {
	cases := []struct {
		name   string
		secure bool
	}{
		{"production sets Secure", true},
		{"development leaves Secure off", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withSecureCookies(t, tc.secure)
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/login", nil), rec)
			if err := setAuthCookie(c, 1, false); err != nil {
				t.Fatalf("setAuthCookie: %v", err)
			}
			cookie := cookieByName(t, rec, "token")
			if cookie.HttpOnly != true {
				t.Errorf("HttpOnly = %v, want true", cookie.HttpOnly)
			}
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
			}
			if cookie.Secure != tc.secure {
				t.Errorf("Secure = %v, want %v", cookie.Secure, tc.secure)
			}
			if cookie.Value == "" {
				t.Error("cookie value is empty")
			}
			if cookie.MaxAge != int(sessionTTL.Seconds()) {
				t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, int(sessionTTL.Seconds()))
			}
		})
	}
}

func TestClearAuthCookieSecureFlag(t *testing.T) {
	// Clearing must use the same Secure flag as set, otherwise the browser
	// will not overwrite/delete the production cookie.
	withSecureCookies(t, true)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/logout", nil), rec)
	clearAuthCookie(c)
	cookie := cookieByName(t, rec, "token")
	if !cookie.Secure {
		t.Error("clearAuthCookie must set Secure when secureCookies is on, or the browser will not drop the session cookie")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Value != "" {
		t.Errorf("cleared cookie value = %q, want empty", cookie.Value)
	}
}

func TestSetAuthCookieRememberMeTTL(t *testing.T) {
	withSecureCookies(t, false)
	cases := []struct {
		name       string
		rememberMe bool
		wantTTL    time.Duration
	}{
		{"default is one day", false, sessionTTL},
		{"remember me is 30 days", true, rememberTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/login", nil), rec)
			if err := setAuthCookie(c, 1, tc.rememberMe); err != nil {
				t.Fatalf("setAuthCookie: %v", err)
			}
			cookie := cookieByName(t, rec, "token")
			if cookie.MaxAge != int(tc.wantTTL.Seconds()) {
				t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, int(tc.wantTTL.Seconds()))
			}
			skew := time.Until(cookie.Expires) - tc.wantTTL
			if skew < -2*time.Second || skew > 2*time.Second {
				t.Errorf("Expires skew = %v, want within 2s of %v", skew, tc.wantTTL)
			}
			parsed, _, err := jwt.NewParser().ParseUnverified(cookie.Value, jwt.MapClaims{})
			if err != nil {
				t.Fatalf("parse jwt: %v", err)
			}
			claims, ok := parsed.Claims.(jwt.MapClaims)
			if !ok {
				t.Fatal("jwt claims are not MapClaims")
			}
			exp, err := claims.GetExpirationTime()
			if err != nil {
				t.Fatalf("jwt exp: %v", err)
			}
			jwtSkew := time.Until(exp.Time) - tc.wantTTL
			if jwtSkew < -2*time.Second || jwtSkew > 2*time.Second {
				t.Errorf("jwt exp skew = %v, want within 2s of %v", jwtSkew, tc.wantTTL)
			}
		})
	}
}
