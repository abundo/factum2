package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/labstack/echo/v5"
)

const maxConfigBundle = 16 << 20

func (ctrl *Controller) ApiConfigBundleExport(c *echo.Context) error {
	var req cfgmgmt.BundleExportRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	bundle, err := cfgmgmt.ExportBundle(ctrl.DB, req)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, bundle)
}

func (ctrl *Controller) ApiConfigBundleImport(c *echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxConfigBundle)
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{"error": "bundle exceeds 16MiB"})
		}
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "failed to read bundle"})
	}
	var bundle cfgmgmt.ConfigBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	res, err := cfgmgmt.ImportBundle(ctrl.DB, bundle)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, res)
}
