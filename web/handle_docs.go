package web

import (
	"errors"
	"net/http"

	"github.com/abundo/factum2/docs"
	"github.com/labstack/echo/v5"
)

// ApiDocsList returns the operator doc catalog (slug + title, no bodies).
// Authenticated so it matches the GUI /doc page; any logged-in user can
// read these — they are the same Markdown GitHub Pages publishes.
func (ctrl *Controller) ApiDocsList(c *echo.Context) error {
	pages, err := docs.List()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "Failed to list documentation"})
	}
	return c.JSON(http.StatusOK, pages)
}

// ApiDocsGet returns one docs/user/<slug>.md page. Invalid slugs are 400
// (never a filesystem lookup); unknown pages are 404.
func (ctrl *Controller) ApiDocsGet(c *echo.Context) error {
	page, err := docs.Get(c.Param("slug"))
	if errors.Is(err, docs.ErrInvalidSlug) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "Invalid document"})
	}
	if errors.Is(err, docs.ErrNotFound) {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Document not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "Failed to load documentation"})
	}
	return c.JSON(http.StatusOK, page)
}
