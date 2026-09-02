package netbox

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/abundo/netboxtool"
)

// extrasPageSize is the page size used for paginated extras REST lists
// (webhooks, event rules), matching netboxtool's REST page size.
const extrasPageSize = 1000

// extrasClient is the Netbox extras (custom fields, webhooks, event rules)
// surface Check uses. REST verbs come from netboxtool; the extras resource
// methods live here so they ship in factum2-netbox rather than the
// netboxtool library CLI.
type extrasClient struct {
	*netboxtool.NetboxClient
}

type restListPage[T any] struct {
	Next    *string `json:"next"`
	Results []T     `json:"results"`
}

// restListAll walks a paginated extras REST list endpoint and returns every
// result. listPath is the path without a query (e.g. "/api/extras/webhooks/").
func restListAll[T any](c *extrasClient, listPath string) ([]T, error) {
	var all []T
	endpoint := listPath + "?limit=" + strconv.Itoa(extrasPageSize)
	for endpoint != "" {
		var page restListPage[T]
		if err := c.RestGet(endpoint, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		if page.Next == nil {
			break
		}
		next, err := url.Parse(*page.Next)
		if err != nil {
			return nil, fmt.Errorf("netbox %s: parse next page url: %w", listPath, err)
		}
		endpoint = listPath + "?" + next.RawQuery
	}
	return all, nil
}
