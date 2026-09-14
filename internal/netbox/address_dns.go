package netbox

import (
	"fmt"

	"github.com/abundo/factum2/internal/dns"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/netboxtool"
)

type restIPAddressPage struct {
	Next    *string `json:"next"`
	Results []struct {
		ID      uint   `json:"id"`
		DNSName string `json:"dns_name"`
	} `json:"results"`
}

// fetchAddressDNSNames loads ipam.IPAddress.dns_name for every address in
// Netbox, dropping names that fail DNS hostname validation.
func fetchAddressDNSNames(nb *netboxtool.NetboxClient, reporter jobevent.Reporter) (map[uint]string, error) {
	out := make(map[uint]string)
	const limit = 1000
	offset := 0
	var invalid int
	for {
		var page restIPAddressPage
		ep := fmt.Sprintf("/api/ipam/ip-addresses/?limit=%d&offset=%d", limit, offset)
		if err := nb.RestGet(ep, &page); err != nil {
			return nil, err
		}
		if len(page.Results) == 0 {
			break
		}
		for _, row := range page.Results {
			normalized, err := dns.NormalizeDNSName(row.DNSName)
			if err != nil {
				reporter.Emit(jobevent.Warning, "Netbox IP %d: invalid dns_name %q: %v", row.ID, row.DNSName, err)
				invalid++
				continue
			}
			if normalized == "" {
				continue
			}
			out[row.ID] = normalized
		}
		if page.Next == nil || *page.Next == "" {
			break
		}
		offset += len(page.Results)
	}
	reporter.Emit(jobevent.Info, "Netbox IP dns_name: %d valid, %d invalid", len(out), invalid)
	return out, nil
}
