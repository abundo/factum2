package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/abundo/factum2/internal/dns"
	"github.com/abundo/factum2/internal/worker"
	"github.com/labstack/echo/v5"
)

type dnsLeasesResponse struct {
	Leases []dns.DHCPLease `json:"leases"`
}

// ApiDnsDhcpLeases is GET /api/dns/leases. The zone-editor MAC picker
// lists current Kea IPv4 and IPv6 leases (hostname, IP, MAC). DHCP must
// be on. Leases are read from local Kea sockets when present, otherwise
// from the DNS dest worker via the hub.
func (ctrl *Controller) ApiDnsDhcpLeases(c *echo.Context) error {
	if !dns.DhcpEnabled(ctrl.DB) {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "DHCP is disabled"})
	}
	leases, err := ctrl.listDhcpLeases(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	if leases == nil {
		leases = []dns.DHCPLease{}
	}
	return c.JSON(http.StatusOK, dnsLeasesResponse{Leases: leases})
}

func (ctrl *Controller) listDhcpLeases(ctx context.Context) ([]dns.DHCPLease, error) {
	if ctrl.dhcpLeasesFn != nil {
		return ctrl.dhcpLeasesFn(ctx)
	}
	leases, err := dns.ListKeaLeases(ctx, nil)
	if err == nil {
		return leases, nil
	}
	if ctrl.RemoteManager == nil {
		return nil, err
	}
	res, callErr := ctrl.RemoteManager.CallRole(ctx, worker.DNSRole, http.MethodGet, "/dhcp/leases", nil, nil)
	if callErr != nil {
		return nil, callErr
	}
	if res.Status != 0 && res.Status != http.StatusOK {
		msg := stringsFromCallError(res)
		if msg == "" {
			msg = fmt.Sprintf("DNS dest worker returned HTTP %d", res.Status)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var payload dnsLeasesResponse
	if err := json.Unmarshal(res.Body, &payload); err != nil {
		return nil, fmt.Errorf("decode DHCP leases: %w", err)
	}
	return payload.Leases, nil
}

func stringsFromCallError(res worker.CallResultMsg) string {
	if res.Error != "" {
		return res.Error
	}
	var wrap struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(res.Body, &wrap); err == nil && wrap.Error != "" {
		return wrap.Error
	}
	return ""
}
