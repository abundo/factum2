package netbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// nominatimReverseURL is the OSM Nominatim reverse-geocode endpoint. Tests
// replace it with a httptest server. The map tiles are OSM-derived, so this
// is the matching address source — no API key. Nominatim's usage policy
// requires a identifying User-Agent and at most one request per second;
// interactive map clicks stay well under that.
var nominatimReverseURL = "https://nominatim.openstreetmap.org/reverse"

var geocodeHTTPClient = &http.Client{Timeout: 5 * time.Second}

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

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "factum2/topology")
	req.Header.Set("Accept", "application/json")

	resp, err := geocodeHTTPClient.Do(req)
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
