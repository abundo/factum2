package worker

import (
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// hubAPIPatterns is the HTTP-subset the hub RPC may invoke. Each pattern is
// compiled as ^(?:pattern)$ so it cannot match a prefix of another route
// (e.g. /api/device matching /api/device/1). First-match-wins.
var hubAPIPatterns = []struct {
	method  string
	pattern string
}{
	{http.MethodGet, `/api/common-config`},
	{http.MethodGet, `/api/librenms-config`},
	{http.MethodGet, `/api/icinga-config`},
	{http.MethodGet, `/api/dns-config`},
	{http.MethodGet, `/api/oxidized-config`},
	{http.MethodGet, `/api/prometheus-config`},
	{http.MethodGet, `/api/netbox-config`},
	{http.MethodGet, `/api/device-sync-config`},
	{http.MethodGet, `/api/device`},
	{http.MethodGet, `/api/device/name/[^/]+`},
	{http.MethodGet, `/api/librenms/pending-deletes`},
	{http.MethodPut, `/api/librenms/pending-deletes/[0-9]+`},
	{http.MethodDelete, `/api/librenms/pending-deletes/[0-9]+`},
	{http.MethodGet, `/api/sync/targets`},
	{http.MethodPost, `/api/sync/all`},
	{http.MethodPost, `/api/sync/[^/]+`},
	{http.MethodGet, `/api/jobs`},
}

var hubAPIRoutes []hubAPIRoute

type hubAPIRoute struct {
	method string
	re     *regexp.Regexp
}

func init() {
	hubAPIRoutes = make([]hubAPIRoute, 0, len(hubAPIPatterns))
	for _, p := range hubAPIPatterns {
		hubAPIRoutes = append(hubAPIRoutes, hubAPIRoute{
			method: p.method,
			re:     regexp.MustCompile("^(?:" + p.pattern + ")$"),
		})
	}
}

// AllowHubAPI reports whether method+path (query already stripped, path
// already normalized) is permitted over the hub.
func AllowHubAPI(method, path string) bool {
	for _, r := range hubAPIRoutes {
		if r.method == method && r.re.MatchString(path) {
			return true
		}
	}
	return false
}

// normalizeHubPath parses an attacker-controlled RequestURI the same way
// Echo will (url.Parse unescapes Path) and rejects anything that would
// rewrite into a different route: scheme/host, fragments, "..", collapsed
// slashes, or a path outside /api/.
func normalizeHubPath(raw string) (cleanedPath, pathAndQuery string, err error) {
	if raw == "" || strings.Contains(raw, "#") || !strings.HasPrefix(raw, "/") {
		return "", "", errInvalidHubPath
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", errInvalidHubPath
	}
	if u.Scheme != "" || u.Host != "" || u.Opaque != "" || u.User != nil {
		return "", "", errInvalidHubPath
	}
	rawPath := u.Path
	if rawPath != path.Clean(rawPath) {
		return "", "", errInvalidHubPath
	}
	for _, seg := range strings.Split(rawPath, "/") {
		if seg == ".." {
			return "", "", errInvalidHubPath
		}
	}
	if !strings.HasPrefix(rawPath, "/api/") {
		return "", "", errInvalidHubPath
	}
	pathAndQuery = rawPath
	if u.RawQuery != "" {
		pathAndQuery = rawPath + "?" + u.RawQuery
	}
	return rawPath, pathAndQuery, nil
}
