package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/oxidized"
	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// OxidizedConfigResponse is what factum2-oxidized (internal/oxidized's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type OxidizedConfigResponse struct {
	util.CommonConfig
	URL  string `json:"url"`
	User string `json:"user"`
	Pass string `json:"pass"`

	DestFile            string `json:"dest_file"`
	IgnoreDevices       string `json:"ignore_devices"`
	IgnoreManufacturers string `json:"ignore_manufacturers"`
	IgnoreModels        string `json:"ignore_models"`
	IgnorePlatforms     string `json:"ignore_platforms"`
}

// ApiOxidizedConfig returns the Oxidized API connection settings from the
// database-backed Settings row, so factum2-oxidized - which typically runs
// on a different host than the primary - doesn't need its own copy of
// these credentials in a local YAML config file.
func (ctrl *Controller) ApiOxidizedConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, OxidizedConfigResponse{
		CommonConfig: util.NewCommonConfig(settings),
		URL:          settings.OxidizedApiURL,
		User:         settings.OxidizedApiUser,
		Pass:         settings.OxidizedApiPass,

		DestFile:            settings.OxidizedDestFile,
		IgnoreDevices:       settings.OxidizedIgnoreDevices,
		IgnoreManufacturers: settings.OxidizedIgnoreManufacturers,
		IgnoreModels:        settings.OxidizedIgnoreModels,
		IgnorePlatforms:     settings.OxidizedIgnorePlatforms,
	})
}

func (ctrl *Controller) oxidizedAPIClient() (oxidizedAPI, error) {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return nil, err
	}
	return oxidized.NewOxidizedClient(util.ConfigOxidized{
		URL:  settings.OxidizedApiURL,
		User: settings.OxidizedApiUser,
		Pass: settings.OxidizedApiPass,
	}), nil
}

// oxidizedAPI is the subset of internal/oxidized's client the GUI browser
// needs. NewOxidizedClient's concrete type is unexported.
type oxidizedAPI interface {
	ListNodes() ([]oxidized.Node, error)
	FetchConfig(nodeFull string) (string, error)
	ListVersions(nodeFull string) ([]oxidized.Version, error)
	GetVersion(nodeFull, oid string) (string, error)
	GetDiff(nodeFull, oid, oid2 string) (*oxidized.Diff, error)
}

func oxidizedAPIError(c *echo.Context, err error) error {
	if errors.Is(err, oxidized.ErrNotConfigured) {
		return c.JSON(http.StatusServiceUnavailable, map[string]any{
			"error": "Oxidized API URL is not configured. Set it under Admin → Settings → Destinations → Oxidized. The URL must be reachable from this factum-web host.",
		})
	}
	msg := err.Error()
	status := http.StatusBadGateway
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "missing node") || strings.Contains(lower, "missing version") {
		status = http.StatusBadRequest
	} else if strings.Contains(lower, "node not found") ||
		strings.Contains(lower, "unable to find") ||
		strings.Contains(lower, "version not found") ||
		strings.Contains(lower, "http 404") {
		status = http.StatusNotFound
	}
	return c.JSON(status, map[string]any{"error": msg})
}

func nodeFullQuery(c *echo.Context) string {
	if v := strings.TrimSpace(c.QueryParam("node_full")); v != "" {
		return v
	}
	node := strings.TrimSpace(c.QueryParam("node"))
	group := strings.TrimSpace(c.QueryParam("group"))
	if group != "" && node != "" {
		return group + "/" + node
	}
	return node
}

// ApiOxidizedNodes is GET /api/oxidized/nodes - the GUI device browser.
// Proxies oxidized-web GET /nodes.json. Deleted devices are not included;
// see internal/oxidized/api.go.
func (ctrl *Controller) ApiOxidizedNodes(c *echo.Context) error {
	h, err := ctrl.oxidizedAPIClient()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	nodes, err := h.ListNodes()
	if err != nil {
		return oxidizedAPIError(c, err)
	}
	return c.JSON(http.StatusOK, nodes)
}

// ApiOxidizedNodeConfig is GET /api/oxidized/node/config?node_full=.
func (ctrl *Controller) ApiOxidizedNodeConfig(c *echo.Context) error {
	nodeFull := nodeFullQuery(c)
	if nodeFull == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "missing node_full"})
	}
	h, err := ctrl.oxidizedAPIClient()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	text, err := h.FetchConfig(nodeFull)
	if err != nil {
		return oxidizedAPIError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"node_full": nodeFull, "config": text})
}

// ApiOxidizedNodeVersions is GET /api/oxidized/node/versions?node_full=.
func (ctrl *Controller) ApiOxidizedNodeVersions(c *echo.Context) error {
	nodeFull := nodeFullQuery(c)
	if nodeFull == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "missing node_full"})
	}
	h, err := ctrl.oxidizedAPIClient()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	versions, err := h.ListVersions(nodeFull)
	if err != nil {
		return oxidizedAPIError(c, err)
	}
	return c.JSON(http.StatusOK, versions)
}

// ApiOxidizedNodeVersion is GET /api/oxidized/node/version?node_full=&oid=.
func (ctrl *Controller) ApiOxidizedNodeVersion(c *echo.Context) error {
	nodeFull := nodeFullQuery(c)
	oid := strings.TrimSpace(c.QueryParam("oid"))
	if nodeFull == "" || oid == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "missing node_full or oid"})
	}
	h, err := ctrl.oxidizedAPIClient()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	text, err := h.GetVersion(nodeFull, oid)
	if err != nil {
		return oxidizedAPIError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"node_full": nodeFull, "oid": oid, "config": text})
}

// ApiOxidizedNodeDiff is GET /api/oxidized/node/diff?node_full=&oid=&oid2=.
func (ctrl *Controller) ApiOxidizedNodeDiff(c *echo.Context) error {
	nodeFull := nodeFullQuery(c)
	oid := strings.TrimSpace(c.QueryParam("oid"))
	oid2 := strings.TrimSpace(c.QueryParam("oid2"))
	if nodeFull == "" || oid == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "missing node_full or oid"})
	}
	h, err := ctrl.oxidizedAPIClient()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	diff, err := h.GetDiff(nodeFull, oid, oid2)
	if err != nil {
		return oxidizedAPIError(c, err)
	}
	return c.JSON(http.StatusOK, diff)
}
