package ipam

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// PrefixDHCP is the DHCP server config stored on an allocated prefix.
type PrefixDHCP struct {
	Enabled    bool
	RangeStart string
	RangeEnd   string
	Gateway    string
	DnsServers string
}

func dhcpFromRow(row models.IpamPrefix) PrefixDHCP {
	return PrefixDHCP{
		Enabled:    row.DhcpEnabled,
		RangeStart: row.DhcpRangeStart,
		RangeEnd:   row.DhcpRangeEnd,
		Gateway:    row.DhcpGateway,
		DnsServers: row.DhcpDnsServers,
	}
}

func applyDHCP(row *models.IpamPrefix, dhcp PrefixDHCP) error {
	dhcp = dhcp.normalized()
	if err := dhcp.validate(row.Prefix); err != nil {
		return statusErr(400, err.Error())
	}
	row.DhcpEnabled = dhcp.Enabled
	row.DhcpRangeStart = dhcp.RangeStart
	row.DhcpRangeEnd = dhcp.RangeEnd
	row.DhcpGateway = dhcp.Gateway
	row.DhcpDnsServers = dhcp.DnsServers
	return nil
}

func (d PrefixDHCP) normalized() PrefixDHCP {
	d.RangeStart = strings.TrimSpace(d.RangeStart)
	d.RangeEnd = strings.TrimSpace(d.RangeEnd)
	d.Gateway = strings.TrimSpace(d.Gateway)
	d.DnsServers = strings.TrimSpace(d.DnsServers)
	return d
}

func (d PrefixDHCP) validate(prefixStr string) error {
	prefix, err := ParsePrefix(prefixStr)
	if err != nil {
		return err
	}
	startSet := d.RangeStart != ""
	endSet := d.RangeEnd != ""
	if startSet != endSet {
		return fmt.Errorf("DHCP range needs both a start and an end address")
	}
	if startSet {
		start, err := parseAddrInPrefix(d.RangeStart, prefix, "DHCP range start")
		if err != nil {
			return err
		}
		end, err := parseAddrInPrefix(d.RangeEnd, prefix, "DHCP range end")
		if err != nil {
			return err
		}
		if start.Compare(end) > 0 {
			return fmt.Errorf("DHCP range start is after end")
		}
	}
	if d.Gateway != "" {
		if _, err := parseAddrInPrefix(d.Gateway, prefix, "DHCP gateway"); err != nil {
			return err
		}
	}
	for _, s := range SplitLines(d.DnsServers) {
		if _, err := netip.ParseAddr(s); err != nil {
			return fmt.Errorf("DHCP DNS server %q is not an IP address", s)
		}
	}
	return nil
}

func parseAddrInPrefix(raw string, prefix netip.Prefix, what string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%s is not an IP address", what)
	}
	if addr.BitLen() != prefix.Addr().BitLen() {
		return netip.Addr{}, fmt.Errorf("%s address family does not match the prefix", what)
	}
	if !prefix.Contains(addr) {
		return netip.Addr{}, fmt.Errorf("%s %s is outside the prefix", what, addr)
	}
	return addr, nil
}

// ListDhcpPrefixes returns every allocated prefix with DHCP enabled,
// across namespaces. Used by factum2-dns to build dnsmgr2.yaml.
func ListDhcpPrefixes(db *gorm.DB) ([]models.IpamPrefix, error) {
	var rows []models.IpamPrefix
	if err := db.Where("dhcp_enabled = ?", true).Order("prefix").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SplitLines trims a newline-separated list, dropping empty lines.
func SplitLines(s string) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// DefaultGateway is the first usable address: network + 1 for prefixes
// with a network/broadcast pair, otherwise the prefix address itself.
func DefaultGateway(prefix netip.Prefix) netip.Addr {
	addr := prefix.Addr()
	if addr.Is4() && prefix.Bits() <= 30 {
		return addr.Next()
	}
	if addr.Is6() && prefix.Bits() <= 126 {
		return addr.Next()
	}
	return addr
}
