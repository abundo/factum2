package netbox

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

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

type restGetter interface {
	RestGet(endpoint string, out any) error
}

// restListAll walks a paginated REST list endpoint and returns every result.
// listPath may include a query (e.g. "/api/tenancy/contact-assignments/?role_id=1");
// limit is always set, and later pages reuse Netbox's next-page query string
// against the path only.
func restListAll[T any](c restGetter, listPath string) ([]T, error) {
	pathOnly, _, _ := strings.Cut(listPath, "?")
	u, err := url.Parse(listPath)
	if err != nil {
		return nil, fmt.Errorf("netbox %s: parse list url: %w", listPath, err)
	}
	q := u.Query()
	q.Set("limit", strconv.Itoa(extrasPageSize))
	u.RawQuery = q.Encode()
	endpoint := u.String()

	var all []T
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
		endpoint = pathOnly + "?" + next.RawQuery
	}
	return all, nil
}
