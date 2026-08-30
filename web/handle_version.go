package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/labstack/echo/v5"
)

// ApiVersion returns the running binary's release identity (ldflags-stamped
// version/commit/date, plus Go version). Unauthenticated so the login page
// can show it. `go run` builds stay at the unstamped defaults.
func (ctrl *Controller) ApiVersion(c *echo.Context) error {
	return c.JSON(http.StatusOK, buildinfo.Snapshot())
}
