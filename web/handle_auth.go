package web

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

// setAuthCookie issues a fresh JWT for userID and stores it in the "token"
// cookie that RequireAPIAuth/JWTCookieMiddleware read back on later requests.
func setAuthCookie(c *echo.Context, userID uint) error {
	tokenString, err := GenerateJWT(userID)
	if err != nil {
		return err
	}
	c.SetCookie(&http.Cookie{
		Name:     "token",
		Value:    tokenString,
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true, // Not needed by JS: the browser attaches it automatically.
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	return nil
}

func clearAuthCookie(c *echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     "token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
		Path:     "/",
	})
}

// --------------------------------------------------------------------------
//
//	API
//
// --------------------------------------------------------------------------

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ApiLogin authenticates the SPA: on success it issues the "token" cookie
// via setAuthCookie; on failure it returns a JSON error body with a 401.
func (ctrl *Controller) ApiLogin(c *echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if req.Username == "" || req.Password == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "username and password are required"})
	}

	user, err := AuthenticateUser(ctrl.DB, req.Username, req.Password)
	if err != nil {
		slog.Error("Database error during login", "username", req.Username, "err", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "an internal error occurred"})
	}
	if user == nil {
		slog.Info("Login failed for", "user", req.Username)
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid username or password"})
	}

	if err := setAuthCookie(c, user.ID); err != nil {
		slog.Warn("Could not create a JWT token for", "user", req.Username)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to create session"})
	}

	slog.Info("User logged in", "username", user.Username, "id", user.ID)
	return c.JSON(http.StatusOK, mePayload(ctrl.DB, *user))
}

func (ctrl *Controller) ApiLogout(c *echo.Context) error {
	clearAuthCookie(c)
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}
