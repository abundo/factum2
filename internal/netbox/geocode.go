package netbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// nominatimReverseURL and nominatimSearchURL are the OSM Nominatim
// endpoints. Tests replace them with a httptest server. The map tiles are
// OSM-derived, so this is the matching address source — no API key.
// Nominatim's usage policy requires an identifying User-Agent and at most
// one request per second. Interactive map clicks and place search stay
// under that via paceNominatim.
var nominatimReverseURL = "https://nominatim.openstreetmap.org/reverse"
var nominatimSearchURL = "https://nominatim.openstreetmap.org/search"

var geocodeHTTPClient = &http.Client{Timeout: 8 * time.Second}

var (
	nominatimMu   sync.Mutex
	nominatimNext time.Time
)

// paceNominatim spaces calls to the public Nominatim service. Tests that
// point at a local server are not delayed.
func paceNominatim(ctx context.Context, target string) error {
	if !strings.Contains(target, "nominatim.openstreetmap.org") {
		return nil
	}
	nominatimMu.Lock()
	wait := time.Until(nominatimNext)
	if wait < 0 {
		wait = 0
	}
	nominatimNext = time.Now().Add(wait + time.Second)
	nominatimMu.Unlock()
	if wait == 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type nominatimReverseResponse struct {
	DisplayName string           `json:"display_name"`
	Error       string           `json:"error"`
	Address     nominatimAddress `json:"address"`
}

type nominatimAddress struct {
	HouseNumber  string `json:"house_number"`
	Road         string `json:"road"`
	Pedestrian   string `json:"pedestrian"`
	City         string `json:"city"`
	Town         string `json:"town"`
	Village      string `json:"village"`
	Municipality string `json:"municipality"`
	Postcode     string `json:"postcode"`
	Country      string `json:"country"`
}

// ReverseGeocode turns lat/lng into a Netbox physical_address string using
// OSM Nominatim. Empty string, nil means nothing useful came back (ocean,
// no coverage) — not an error. Transport / HTTP failures are errors.
func ReverseGeocode(lat, lng float64) (string, error) {
	u, err := url.Parse(nominatimReverseURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("lat", strconv.FormatFloat(lat, 'f', 7, 64))
	q.Set("lon", strconv.FormatFloat(lng, 'f', 7, 64))
	q.Set("format", "jsonv2")
	q.Set("addressdetails", "1")
	q.Set("zoom", "18")
	u.RawQuery = q.Encode()

	resp, err := nominatimGet(context.Background(), u)
	if err != nil {
		return "", fmt.Errorf("nominatim reverse: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("nominatim reverse: %s", resp.Status)
	}

	var body nominatimReverseResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("nominatim reverse: %w", err)
	}
	if body.Error != "" {
		return "", nil
	}
	return formatNominatimAddress(body.Address, body.DisplayName), nil
}

func formatNominatimAddress(addr nominatimAddress, displayName string) string {
	street := strings.TrimSpace(joinNonEmpty(" ", addr.HouseNumber, addr.Road))
	if street == "" {
		street = strings.TrimSpace(addr.Pedestrian)
	}
	city := firstNonEmpty(addr.City, addr.Town, addr.Village, addr.Municipality)
	locality := strings.TrimSpace(joinNonEmpty(" ", addr.Postcode, city))

	var lines []string
	if street != "" {
		lines = append(lines, street)
	}
	if locality != "" {
		lines = append(lines, locality)
	}
	if c := strings.TrimSpace(addr.Country); c != "" {
		lines = append(lines, c)
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n")
	}
	return strings.TrimSpace(displayName)
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, sep)
}

func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			return s
		}
	}
	return ""
}

// Place is one Nominatim search hit the network map can fly to.
// BBox is west, south, east, north in degrees. It is omitted when
// Nominatim did not return a usable box.
type Place struct {
	Name  string    `json:"name"`
	Label string    `json:"label"`
	Lat   float64   `json:"lat"`
	Lng   float64   `json:"lng"`
	BBox  []float64 `json:"bbox,omitempty"`
}

type nominatimSearchHit struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Lat         string   `json:"lat"`
	Lon         string   `json:"lon"`
	BoundingBox []string `json:"boundingbox"`
}

// SearchPlaces looks up streets, cities, and other places. viewbox is an
// optional "west,north,east,south" bias (current map view). acceptLanguage
// is forwarded when it is a short language-tag list. An empty result is
// not an error.
func SearchPlaces(ctx context.Context, query, viewbox, acceptLanguage string) ([]Place, error) {
	u, err := url.Parse(nominatimSearchURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "jsonv2")
	q.Set("limit", "8")
	q.Set("dedupe", "1")
	if validViewbox(viewbox) {
		q.Set("viewbox", viewbox)
	}
	if lang := nominatimLanguage(acceptLanguage); lang != "" {
		q.Set("accept-language", lang)
	}
	u.RawQuery = q.Encode()

	resp, err := nominatimGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("nominatim search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nominatim search: %s", resp.Status)
	}

	var hits []nominatimSearchHit
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&hits); err != nil {
		return nil, fmt.Errorf("nominatim search: %w", err)
	}
	out := make([]Place, 0, len(hits))
	for _, hit := range hits {
		lat, errLat := strconv.ParseFloat(hit.Lat, 64)
		lng, errLng := strconv.ParseFloat(hit.Lon, 64)
		if errLat != nil || errLng != nil {
			continue
		}
		name := strings.TrimSpace(hit.Name)
		label := strings.TrimSpace(hit.DisplayName)
		if name == "" {
			name = label
			if i := strings.IndexByte(name, ','); i > 0 {
				name = strings.TrimSpace(name[:i])
			}
		}
		if name == "" {
			continue
		}
		place := Place{Name: name, Label: label, Lat: lat, Lng: lng}
		if bbox := nominatimBBox(hit.BoundingBox); bbox != nil {
			place.BBox = bbox
		}
		out = append(out, place)
	}
	return out, nil
}

func nominatimGet(ctx context.Context, u *url.URL) (*http.Response, error) {
	if err := paceNominatim(ctx, u.String()); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "factum2/topology")
	req.Header.Set("Accept", "application/json")
	return geocodeHTTPClient.Do(req)
}

// nominatim boundingbox is south, north, west, east.
func nominatimBBox(raw []string) []float64 {
	if len(raw) != 4 {
		return nil
	}
	nums := make([]float64, 4)
	for i, s := range raw {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	south, north, west, east := nums[0], nums[1], nums[2], nums[3]
	if east < west || north < south {
		return nil
	}
	return []float64{west, south, east, north}
}

func validViewbox(s string) bool {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err != nil {
			return false
		}
	}
	return true
}

func nominatimLanguage(header string) string {
	header = strings.TrimSpace(header)
	if header == "" || len(header) > 80 {
		return ""
	}
	for _, r := range header {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("-,;=. *", r):
		default:
			return ""
		}
	}
	return header
}
