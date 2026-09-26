package web

import (
	"net"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
)

// authHits is the in-process window for unauthenticated login and password
// reset calls. One process, so a second factum2-web does not share it.
var authHits = &hitWindow{}

type hitWindow struct {
	mu sync.Mutex
	m  map[string][]time.Time
}

func (h *hitWindow) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	cut := now.Add(-window)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = map[string][]time.Time{}
	}
	kept := h.m[key][:0]
	for _, ts := range h.m[key] {
		if ts.After(cut) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= limit {
		h.m[key] = kept
		return false
	}
	h.m[key] = append(kept, now)
	return true
}

func allowAuthAttempt(key string, limit int, window time.Duration) bool {
	return authHits.allow(key, limit, window)
}

// clientIP is the TCP peer, not X-Forwarded-For. A client that can set
// the forwarding header must not pick a fresh limit bucket.
func clientIP(c *echo.Context) string {
	host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
	if err != nil {
		return c.Request().RemoteAddr
	}
	return host
}
