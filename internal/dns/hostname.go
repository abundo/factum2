package dns

import (
	"strings"

	"github.com/abundo/dnsmgr2/dnsmgr"
)

// NormalizeDNSName trims, strips a trailing dot, and checks the name is a
// valid DNS hostname (same rules dnsmgr2 uses for records). Empty input
// is valid and returns empty (Netbox dns_name is optional).
func NormalizeDNSName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if err := dnsmgr.VerifyDnsname(name); err != nil {
		return "", err
	}
	name = strings.TrimSuffix(name, ".")
	return strings.ToLower(name), nil
}

// dnsNameRelative returns name as a record owner relative to zone, or ""
// if name does not belong in that zone. Unqualified names (no dots) are
// only accepted when allowUnqualified is true (the default domain).
func dnsNameRelative(name, zone string, allowUnqualified bool) string {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	if name == "" || zone == "" {
		return ""
	}
	if name == zone {
		return "@"
	}
	suffix := "." + zone
	if strings.HasSuffix(name, suffix) {
		rel := name[:len(name)-len(suffix)]
		if rel == "" || strings.Contains(rel, ".") && strings.HasPrefix(rel, ".") {
			return ""
		}
		return rel
	}
	if allowUnqualified && !strings.Contains(name, ".") {
		return name
	}
	return ""
}
