package netbox

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/netboxtool"
)

func newExtrasTestClient(t *testing.T, handler http.HandlerFunc) *extrasClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	return &extrasClient{nb}
}

func TestGetWebhooks(t *testing.T) {
	nb := newExtrasTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/extras/webhooks/" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"next": null,
			"results": [{
				"id": 3,
				"name": "inventory-sync",
				"payload_url": "https://example.com/api/netbox-webhook",
				"http_method": {"value": "POST", "label": "POST"},
				"http_content_type": "application/json",
				"body_template": ""
			}]
		}`))
	})

	hooks, err := nb.GetWebhooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 {
		t.Fatalf("len = %d", len(hooks))
	}
	if hooks[0].NetboxID != 3 || hooks[0].Name != "inventory-sync" {
		t.Fatalf("hook = %+v", hooks[0])
	}
	if hooks[0].HTTPMethod != "POST" {
		t.Fatalf("method = %q", hooks[0].HTTPMethod)
	}
}

func TestGetEventRules(t *testing.T) {
	nb := newExtrasTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/extras/event-rules/" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"next": null,
			"results": [{
				"id": 7,
				"name": "sync",
				"enabled": true,
				"object_types": ["dcim.device", "dcim.interface"],
				"event_types": ["object_created", "object_updated", "object_deleted"],
				"action_type": {"value": "webhook", "label": "Webhook"},
				"action_object_id": 3,
				"conditions": null
			}]
		}`))
	})

	rules, err := nb.GetEventRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("len = %d", len(rules))
	}
	if rules[0].ActionType != "webhook" || rules[0].ActionObjectID != 3 {
		t.Fatalf("rule = %+v", rules[0])
	}
	if !rules[0].HasObjectType("dcim.device") || !rules[0].HasEvent("object_deleted") {
		t.Fatalf("coverage missing: %+v", rules[0])
	}
	if rules[0].HasConditions() {
		t.Fatal("null conditions must be empty")
	}
}

func TestNetboxChoiceString_BareAndObject(t *testing.T) {
	var a, b netboxChoiceString
	if err := json.Unmarshal([]byte(`"POST"`), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"value":"webhook","label":"Webhook"}`), &b); err != nil {
		t.Fatal(err)
	}
	if a != "POST" || b != "webhook" {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestNBEventRule_HasConditions(t *testing.T) {
	if (&NBEventRule{}).HasConditions() {
		t.Fatal("empty rule")
	}
	if (&NBEventRule{Conditions: map[string]any{}}).HasConditions() {
		t.Fatal("empty map")
	}
	if !(&NBEventRule{Conditions: map[string]any{"and": []any{}}}).HasConditions() {
		t.Fatal("non-empty map")
	}
}

func TestCreateCustomField(t *testing.T) {
	nb := newExtrasTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			if r.URL.Path != "/api/extras/custom-fields/" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["name"] != "location" || body["type"] != "text" {
				t.Fatalf("body = %v", body)
			}
			_, _ = w.Write([]byte(`{"id":11,"name":"location","type":"text","object_types":["dcim.device"]}`))
			return
		}
		if r.URL.Path != "/api/extras/custom-fields/" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[{"id":11,"name":"location","type":"text","object_types":["dcim.device"]}]}`))
	})
	cf, err := nb.CreateCustomField(CustomFieldWrite{
		Name: "location", Type: cfTypeText, ObjectTypes: []string{"dcim.device"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cf.NetboxID != 11 {
		t.Fatalf("id = %d", cf.NetboxID)
	}
}

func TestCreateWebhook(t *testing.T) {
	nb := newExtrasTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/extras/webhooks/" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "factum-sync" || body["payload_url"] == nil {
			t.Fatalf("body = %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 4,
			"name": "factum-sync",
			"payload_url": "https://factum.example.com/api/netbox-webhook",
			"http_method": "POST",
			"http_content_type": "application/json",
			"body_template": ""
		}`))
	})
	h, err := nb.CreateWebhook(WebhookWrite{
		Name: "factum-sync", PayloadURL: "https://factum.example.com/api/netbox-webhook",
		Secret: "s", SSLVerification: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.NetboxID != 4 || h.HTTPMethod != "POST" {
		t.Fatalf("hook = %+v", h)
	}
}

func TestEnsureCustomFieldChoiceSet_Creates(t *testing.T) {
	var posts int
	nb := newExtrasTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"results":[]}`))
			return
		}
		posts++
		if r.URL.Path != "/api/extras/custom-field-choice-sets/" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"ssh"`) {
			t.Fatalf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{
			"id": 5,
			"name": "connection_method",
			"extra_choices": [["ssh","ssh"],["telnet","telnet"]]
		}`))
	})
	set, err := nb.EnsureCustomFieldChoiceSet("connection_method", [][2]string{{"ssh", "ssh"}, {"telnet", "telnet"}})
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatalf("posts = %d", posts)
	}
	if set.NetboxID != 5 || len(set.ExtraChoices) != 2 {
		t.Fatalf("set = %+v", set)
	}
}

func TestEnsureCustomFieldChoiceSet_Merges(t *testing.T) {
	var patched bool
	nb := newExtrasTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{
				"results": [{
					"id": 5,
					"name": "connection_method",
					"extra_choices": [["ssh","ssh"]]
				}]
			}`))
			return
		}
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s", r.Method)
		}
		patched = true
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		choices, _ := body["extra_choices"].([]any)
		if len(choices) != 2 {
			t.Fatalf("choices = %v", choices)
		}
		_, _ = w.Write([]byte(`{
			"id": 5,
			"name": "connection_method",
			"extra_choices": [["ssh","ssh"],["telnet","telnet"]]
		}`))
	})
	set, err := nb.EnsureCustomFieldChoiceSet("connection_method", [][2]string{{"ssh", "ssh"}, {"telnet", "telnet"}})
	if err != nil {
		t.Fatal(err)
	}
	if !patched {
		t.Fatal("expected PATCH")
	}
	if len(set.ExtraChoices) != 2 {
		t.Fatalf("choices = %v", set.ExtraChoices)
	}
}

func TestMergeChoices_DoesNotDropExisting(t *testing.T) {
	merged, added := mergeChoices([][2]string{{"ssh", "SSH"}, {"console", "console"}}, [][2]string{{"ssh", "ssh"}, {"telnet", "telnet"}})
	if !added {
		t.Fatal("expected added")
	}
	if len(merged) != 3 || merged[0][1] != "SSH" || merged[2][0] != "telnet" {
		t.Fatalf("merged = %v", merged)
	}
}
