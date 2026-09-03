package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
			if err := setAuthCookie(c, 1); err != nil {
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
