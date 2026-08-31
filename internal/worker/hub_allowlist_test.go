package worker

import (
	"net/http"
	"testing"
)

func TestAllowHubAPI(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/api/common-config", true},
		{http.MethodGet, "/api/librenms-config", true},
		{http.MethodGet, "/api/icinga-config", true},
		{http.MethodGet, "/api/dns-config", true},
		{http.MethodGet, "/api/oxidized-config", true},
		{http.MethodGet, "/api/prometheus-config", true},
		{http.MethodGet, "/api/netbox-config", true},
		{http.MethodGet, "/api/device-sync-config", true},
		{http.MethodGet, "/api/device", true},
		{http.MethodGet, "/api/device/name/core-sw1", true},
		{http.MethodGet, "/api/librenms/pending-deletes", true},
		{http.MethodPut, "/api/librenms/pending-deletes/7", true},
		{http.MethodDelete, "/api/librenms/pending-deletes/7", true},
		{http.MethodGet, "/api/sync/targets", true},
		{http.MethodPost, "/api/sync/all", true},
		{http.MethodPost, "/api/sync/dns", true},
		{http.MethodGet, "/api/jobs", true},
		{http.MethodPost, "/api/librenms-config", false},
		{http.MethodGet, "/api/librenms-config/extra", false},
		{http.MethodGet, "/api/dns-config/extra", false},
		{http.MethodGet, "/api/admin/settings", false},
		{http.MethodPost, "/api/worker/run", false},
		{http.MethodGet, "/api/device/1", false},
		{http.MethodGet, "/api/device/1/impact", false},
		{http.MethodGet, "/api/jobs/1", false},
		{http.MethodGet, "/api/jobs/1/tasks/2/events", false},
		{http.MethodPost, "/api/device/1/interfaces/refresh", false},
		{http.MethodPost, "/api/librenms/pending-deletes/7/delete-next-sync", false},
		{http.MethodPost, "/api/netbox-webhook", false},
		{http.MethodGet, "/api/device/name/core-sw1/extra", false},
	}
	for _, tc := range cases {
		got := AllowHubAPI(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("AllowHubAPI(%s, %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestNormalizeHubPath(t *testing.T) {
	t.Parallel()
	ok := []struct {
		raw, cleaned, withQuery string
	}{
		{"/api/librenms-config", "/api/librenms-config", "/api/librenms-config"},
		{"/api/librenms-config?x=1", "/api/librenms-config", "/api/librenms-config?x=1"},
		{"/api/device?include=interfaces", "/api/device", "/api/device?include=interfaces"},
	}
	for _, tc := range ok {
		cleaned, withQuery, err := normalizeHubPath(tc.raw)
		if err != nil {
			t.Errorf("normalizeHubPath(%q) unexpected err %v", tc.raw, err)
			continue
		}
		if cleaned != tc.cleaned || withQuery != tc.withQuery {
			t.Errorf("normalizeHubPath(%q) = %q, %q; want %q, %q", tc.raw, cleaned, withQuery, tc.cleaned, tc.withQuery)
		}
	}

	bad := []string{
		"",
		"api/librenms-config",
		"/api/librenms-config#frag",
		"http://evil/api/librenms-config",
		"https://evil/api/librenms-config",
		"//evil/api/librenms-config",
		"/api/foo/../librenms-config",
		"/api//librenms-config",
		"/api/./librenms-config",
		"/api/librenms-config/%2e%2e/admin/settings",
		"/notapi/librenms-config",
		"/api",
	}
	for _, raw := range bad {
		if _, _, err := normalizeHubPath(raw); err == nil {
			t.Errorf("normalizeHubPath(%q) = nil, want error", raw)
		}
	}
}

func TestHubAPISyncAllRegisteredBeforeTarget(t *testing.T) {
	t.Parallel()
	allIdx, targetIdx := -1, -1
	for i, p := range hubAPIPatterns {
		if p.method == http.MethodPost && p.pattern == `/api/sync/all` {
			allIdx = i
		}
		if p.method == http.MethodPost && p.pattern == `/api/sync/[^/]+` {
			targetIdx = i
		}
	}
	if allIdx < 0 || targetIdx < 0 {
		t.Fatalf("missing sync rows: all=%d target=%d", allIdx, targetIdx)
	}
	if allIdx >= targetIdx {
		t.Fatalf("/api/sync/all at %d must be registered before /api/sync/[^/]+ at %d", allIdx, targetIdx)
	}
}
