package netboxtool

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestParseInterfaceTypeChoices(t *testing.T) {
	raw := []byte(`[
		{"value": "virtual", "display_name": "Virtual"},
		{"value": "1000base-t", "display_name": "1000BASE-T (1GE)"}
	]`)
	got, err := parseInterfaceTypeChoices(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != "virtual" || got[0].Label != "Virtual" || got[1].Value != "1000base-t" {
		t.Fatalf("got = %+v", got)
	}
}

func TestParseInterfaceTypeChoicesPairs(t *testing.T) {
	got, err := parseInterfaceTypeChoices([]byte(`[["lag","LAG"],["other","Other"]]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != "lag" || got[0].Label != "LAG" {
		t.Fatalf("got = %+v", got)
	}
}

func TestGetInterfaceTypeChoices(t *testing.T) {
	nb := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodOptions || r.URL.Path != "/api/dcim/interfaces/" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"actions": map[string]any{
				"POST": map[string]any{
					"type": map[string]any{
						"choices": []map[string]string{
							{"value": "virtual", "display_name": "Virtual"},
							{"value": "lag", "display_name": "Link Aggregation Group (LAG)"},
						},
					},
				},
			},
		})
	})
	got, err := nb.GetInterfaceTypeChoices()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Value != "lag" {
		t.Fatalf("got = %+v", got)
	}
}

func TestGetInterfaceTypeChoicesFallsBackToPUT(t *testing.T) {
	nb := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"actions": map[string]any{
				"PUT": map[string]any{
					"type": map[string]any{
						"choices": []map[string]string{
							{"value": "other", "display_name": "Other"},
						},
					},
				},
			},
		})
	})
	got, err := nb.GetInterfaceTypeChoices()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != "other" {
		t.Fatalf("got = %+v", got)
	}
}
