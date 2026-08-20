package web

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// httpErrorHandler replaces Echo's bare default error body (just
// `{"message":"Not Found"}`, no matter what actually went wrong) with
// something a human can act on: JSON for API callers, a readable HTML page
// for everyone else.
//
// The most common real-world trigger for a 404 on a non-API path is the "/*"
// SPA catch-all itself failing to find web/static/vue/index.html - e.g. the
// frontend hasn't been built yet, or the binary is running from the wrong
// working directory - so that case gets a specific, actionable hint instead
// of a generic message.
func httpErrorHandler(c *echo.Context, err error) {
	if resp, rerr := echo.UnwrapResponse(c.Response()); rerr == nil && resp.Committed {
		return
	}

	code := http.StatusInternalServerError
	message := "Internal server error"
	// echo's built-in sentinel errors (echo.ErrNotFound etc.) aren't
	// *echo.HTTPError - they only implement HTTPStatusCoder - so the status
	// code has to come from echo.StatusCode, not a type assertion.
	if sc := echo.StatusCode(err); sc != 0 {
		code = sc
		message = http.StatusText(sc)
	}
	var he *echo.HTTPError
	if errors.As(err, &he) {
		code = he.Code
		message = he.Message
	}

	slog.Error("request error", "method", c.Request().Method, "path", c.Request().URL.Path, "status", code, "err", err)

	if strings.HasPrefix(c.Request().URL.Path, "/api/") {
		if writeErr := c.JSON(code, map[string]any{"error": message}); writeErr != nil {
			slog.Error("failed to write error response", "err", writeErr)
		}
		return
	}

	if code == http.StatusNotFound {
		message = "The application could not be loaded. This usually means the frontend hasn't been built yet " +
			`(run "npm run build" in web/frontend, or "make frontend"), or this binary isn't running from the ` +
			"factum repository root, so web/static/vue can't be found."
	}

	if writeErr := c.HTML(code, errorPageHTML(code, message)); writeErr != nil {
		slog.Error("failed to write error response", "err", writeErr)
	}
}

func errorPageHTML(code int, message string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Factum - Error %d</title>
<style>
  body { font-family: system-ui, sans-serif; background: #0f172a; color: #e2e8f0; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
  .card { max-width: 32rem; padding: 2rem; text-align: center; }
  .code { font-size: 3rem; font-weight: 700; color: #64748b; margin-bottom: 0.5rem; }
  p { line-height: 1.5; color: #cbd5e1; }
  a { color: #60a5fa; }
</style>
</head>
<body>
  <div class="card">
    <div class="code">%d</div>
    <p>%s</p>
    <p><a href="/">Go to home</a></p>
  </div>
</body>
</html>`, code, code, html.EscapeString(message))
}
