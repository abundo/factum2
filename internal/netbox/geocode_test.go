package netbox

import (
	"context"
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

func TestSearchPlaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "Drottninggatan" {
			t.Errorf("q = %s", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("viewbox") != "17,60,19,58" {
			t.Errorf("viewbox = %s", r.URL.Query().Get("viewbox"))
		}
		if r.URL.Query().Get("accept-language") != "sv" {
			t.Errorf("accept-language = %s", r.URL.Query().Get("accept-language"))
		}
		if r.Header.Get("User-Agent") != "factum2/topology" {
			t.Errorf("User-Agent = %s", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"name": "Drottninggatan",
				"display_name": "Drottninggatan, Norrmalm, Stockholm, Sweden",
				"lat": "59.332",
				"lon": "18.063",
				"boundingbox": ["59.33", "59.34", "18.05", "18.07"]
			},
			{"name": "", "display_name": "", "lat": "bad", "lon": "18"}
		]`))
	}))
	t.Cleanup(srv.Close)
	orig := nominatimSearchURL
	nominatimSearchURL = srv.URL
	t.Cleanup(func() { nominatimSearchURL = orig })

	got, err := SearchPlaces(context.Background(), "Drottninggatan", "17,60,19,58", "sv")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Name != "Drottninggatan" || got[0].Lat != 59.332 || got[0].Lng != 18.063 {
		t.Errorf("hit = %+v", got[0])
	}
	wantBox := []float64{18.05, 59.33, 18.07, 59.34}
	if len(got[0].BBox) != 4 {
		t.Fatalf("bbox = %v", got[0].BBox)
	}
	for i := range wantBox {
		if got[0].BBox[i] != wantBox[i] {
			t.Errorf("bbox[%d] = %v, want %v", i, got[0].BBox[i], wantBox[i])
		}
	}
}

func TestSearchPlaces_IgnoresBadViewboxAndLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("viewbox") != "" {
			t.Errorf("viewbox = %s", r.URL.Query().Get("viewbox"))
		}
		if r.URL.Query().Get("accept-language") != "" {
			t.Errorf("accept-language = %s", r.URL.Query().Get("accept-language"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"display_name": "Stockholm, Sweden", "lat": "59.3", "lon": "18.0"}]`))
	}))
	t.Cleanup(srv.Close)
	orig := nominatimSearchURL
	nominatimSearchURL = srv.URL
	t.Cleanup(func() { nominatimSearchURL = orig })

	got, err := SearchPlaces(context.Background(), "Stockholm", "not-a-box", "sv\nInjected: yes")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Stockholm" || got[0].BBox != nil {
		t.Fatalf("got %+v", got)
	}
}
