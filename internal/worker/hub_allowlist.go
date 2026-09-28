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
//
// roles, when non-empty, is the worker.commands names that may call the
// route. A DNS worker cannot fetch device-sync or NetBox credentials.
// An empty roles list is available to every connected worker.
var hubAPIPatterns = []struct {
	method  string
	pattern string
	roles   []string
}{
	{http.MethodGet, `/api/common-config`, nil},
	{http.MethodGet, `/api/librenms-config`, []string{"librenms"}},
	{http.MethodGet, `/api/icinga-config`, []string{"icinga"}},
	{http.MethodGet, `/api/dns-config`, []string{"dns"}},
	{http.MethodGet, `/api/certs-config`, []string{"certs"}},
	{http.MethodGet, `/api/oxidized-config`, []string{"oxidized"}},
	{http.MethodGet, `/api/prometheus-config`, []string{"prometheus"}},
	{http.MethodGet, `/api/netbox-config`, []string{"netbox", "netbox-delta", "librenms", "becs", "device-sync"}},
	{http.MethodGet, `/api/device-sync-config`, []string{"device-sync", "storage"}},
	{http.MethodGet, `/api/storage-config`, []string{"storage"}},
	{http.MethodGet, `/api/radius-config`, []string{"radius"}},
	{http.MethodPost, `/api/radius-events`, []string{"radius"}},
	{http.MethodGet, `/api/device`, nil},
	{http.MethodGet, `/api/device/name/[^/]+`, nil},
	// Name-based impact for factum2-icinga-notifications. Numeric
	// /api/device/:id and /api/device/:id/impact stay off the hub.
	{http.MethodGet, `/api/device/name/[^/]+/impact`, []string{"icinga"}},
	{http.MethodGet, `/api/librenms/pending-deletes`, []string{"librenms"}},
	{http.MethodPut, `/api/librenms/pending-deletes/[0-9]+`, []string{"librenms"}},
	{http.MethodDelete, `/api/librenms/pending-deletes/[0-9]+`, []string{"librenms"}},
	{http.MethodGet, `/api/sync/targets`, nil},
	{http.MethodPost, `/api/sync/all`, nil},
	{http.MethodPost, `/api/sync/[^/]+`, nil},
	{http.MethodGet, `/api/jobs`, nil},
}

var hubAPIRoutes []hubAPIRoute

type hubAPIRoute struct {
	method string
	re     *regexp.Regexp
	roles  []string
}

func init() {
	hubAPIRoutes = make([]hubAPIRoute, 0, len(hubAPIPatterns))
	for _, p := range hubAPIPatterns {
		hubAPIRoutes = append(hubAPIRoutes, hubAPIRoute{
			method: p.method,
			re:     regexp.MustCompile("^(?:" + p.pattern + ")$"),
			roles:  p.roles,
		})
	}
}

// AllowHubAPI reports whether method+path (query already stripped, path
// already normalized) is permitted over the hub, ignoring the caller's role.
func AllowHubAPI(method, path string) bool {
	return allowHubAPI(method, path, nil, false)
}

// AllowHubAPIForRoles is AllowHubAPI plus the per-role gate. roles are the
// command names from the worker's hello.
func AllowHubAPIForRoles(method, path string, roles []string) bool {
	return allowHubAPI(method, path, roles, true)
}

func allowHubAPI(method, path string, roles []string, checkRoles bool) bool {
	for _, r := range hubAPIRoutes {
		if r.method != method || !r.re.MatchString(path) {
			continue
		}
		if !checkRoles || len(r.roles) == 0 {
			return true
		}
		for _, have := range roles {
			for _, need := range r.roles {
				if have == need {
					return true
				}
			}
		}
		return false
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
