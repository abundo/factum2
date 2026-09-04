package netbox

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormatNominatimAddress(t *testing.T) {
	got := formatNominatimAddress(nominatimAddress{
		HouseNumber: "1",
		Road:        "Drottninggatan",
		City:        "Stockholm",
		Postcode:    "111 51",
		Country:     "Sweden",
	}, "ignored")
	want := "1 Drottninggatan\n111 51 Stockholm\nSweden"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatNominatimAddress_DisplayFallback(t *testing.T) {
	got := formatNominatimAddress(nominatimAddress{}, "North Atlantic Ocean")
	if got != "North Atlantic Ocean" {
		t.Errorf("got %q", got)
	}
}

func TestReverseGeocode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reverse" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("lat") != "59.3293000" {
			t.Errorf("lat = %s", r.URL.Query().Get("lat"))
		}
		if r.Header.Get("User-Agent") != "factum2/topology" {
			t.Errorf("User-Agent = %s", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"display_name": "1 Drottninggatan, Stockholm, Sweden",
			"address": {
				"house_number": "1",
				"road": "Drottninggatan",
				"city": "Stockholm",
				"postcode": "111 51",
				"country": "Sweden"
			}
		}`))
	}))
	t.Cleanup(srv.Close)
	orig := nominatimReverseURL
	nominatimReverseURL = srv.URL + "/reverse"
	t.Cleanup(func() { nominatimReverseURL = orig })

	got, err := ReverseGeocode(59.3293, 18.0686)
	if err != nil {
		t.Fatal(err)
	}
	want := "1 Drottninggatan\n111 51 Stockholm\nSweden"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReverseGeocode_Unable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"Unable to geocode"}`))
	}))
	t.Cleanup(srv.Close)
	orig := nominatimReverseURL
	nominatimReverseURL = srv.URL
	t.Cleanup(func() { nominatimReverseURL = orig })

	got, err := ReverseGeocode(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
