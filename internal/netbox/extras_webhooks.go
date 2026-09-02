package netbox

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// factumWebhookName is the extras.Webhook / extras.EventRule name Check
// creates when --update has to provision the factum endpoint.
const factumWebhookName = "factum-sync"

// NBWebhook is extras.Webhook — the destination half of Netbox 3.7+'s
// webhook/event-rule split. Object types and events live on NBEventRule.
type NBWebhook struct {
	NetboxID        uint
	Name            string
	PayloadURL      string
	HTTPMethod      string
	HTTPContentType string
	BodyTemplate    string
}

// NBEventRule is extras.EventRule — binds object types and event types to
// an action (typically a webhook). ActionObjectID is the extras.Webhook id
// when ActionType is "webhook".
type NBEventRule struct {
	NetboxID       uint
	Name           string
	Enabled        bool
	ObjectTypes    []string
	EventTypes     []string
	ActionType     string
	ActionObjectID uint
	// Conditions is the optional JSON condition object; nil/empty means
	// the rule fires for every matching event.
	Conditions any
}

// WebhookWrite is the body for creating extras.Webhook.
type WebhookWrite struct {
	Name            string
	PayloadURL      string
	HTTPMethod      string
	HTTPContentType string
	BodyTemplate    string
	Secret          string
	SSLVerification bool
}

// EventRuleWrite is the body for creating extras.EventRule.
type EventRuleWrite struct {
	Name           string
	Enabled        bool
	ObjectTypes    []string
	EventTypes     []string
	ActionType     string
	ActionObjectID uint
}

// netboxChoiceString accepts either a bare JSON string or Netbox's
// {value,label} choice object (action_type, http_method, ...).
type netboxChoiceString string

func (s *netboxChoiceString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = netboxChoiceString(v)
		return nil
	}
	var c struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	*s = netboxChoiceString(c.Value)
	return nil
}

type restWebhook struct {
	ID              uint               `json:"id"`
	Name            string             `json:"name"`
	PayloadURL      string             `json:"payload_url"`
	HTTPMethod      netboxChoiceString `json:"http_method"`
	HTTPContentType string             `json:"http_content_type"`
	BodyTemplate    string             `json:"body_template"`
}

func (w restWebhook) toNB() *NBWebhook {
	method := string(w.HTTPMethod)
	if method == "" {
		method = "POST"
	}
	return &NBWebhook{
		NetboxID:        w.ID,
		Name:            w.Name,
		PayloadURL:      w.PayloadURL,
		HTTPMethod:      method,
		HTTPContentType: w.HTTPContentType,
		BodyTemplate:    w.BodyTemplate,
	}
}

type restEventRule struct {
	ID             uint               `json:"id"`
	Name           string             `json:"name"`
	Enabled        bool               `json:"enabled"`
	ObjectTypes    []string           `json:"object_types"`
	EventTypes     []string           `json:"event_types"`
	ActionType     netboxChoiceString `json:"action_type"`
	ActionObjectID uint               `json:"action_object_id"`
	Conditions     any                `json:"conditions"`
}

func (r restEventRule) toNB() *NBEventRule {
	return &NBEventRule{
		NetboxID:       r.ID,
		Name:           r.Name,
		Enabled:        r.Enabled,
		ObjectTypes:    r.ObjectTypes,
		EventTypes:     r.EventTypes,
		ActionType:     string(r.ActionType),
		ActionObjectID: r.ActionObjectID,
		Conditions:     r.Conditions,
	}
}

func (c *extrasClient) GetWebhooks() ([]*NBWebhook, error) {
	rows, err := restListAll[restWebhook](c, "/api/extras/webhooks/")
	if err != nil {
		return nil, err
	}
	out := make([]*NBWebhook, 0, len(rows))
	for _, w := range rows {
		out = append(out, w.toNB())
	}
	return out, nil
}

func (c *extrasClient) GetEventRules() ([]*NBEventRule, error) {
	rows, err := restListAll[restEventRule](c, "/api/extras/event-rules/")
	if err != nil {
		return nil, err
	}
	out := make([]*NBEventRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toNB())
	}
	return out, nil
}

func (w WebhookWrite) toPayload() map[string]any {
	method := w.HTTPMethod
	if method == "" {
		method = "POST"
	}
	ct := w.HTTPContentType
	if ct == "" {
		ct = "application/json"
	}
	return map[string]any{
		"name":              w.Name,
		"payload_url":       w.PayloadURL,
		"http_method":       method,
		"http_content_type": ct,
		"body_template":     w.BodyTemplate,
		"secret":            w.Secret,
		"ssl_verification":  w.SSLVerification,
	}
}

func (c *extrasClient) CreateWebhook(w WebhookWrite) (*NBWebhook, error) {
	var created restWebhook
	if err := c.RestPost("/api/extras/webhooks/", w.toPayload(), &created); err != nil {
		return nil, err
	}
	return created.toNB(), nil
}

func (c *extrasClient) UpdateWebhook(id uint, changes map[string]any) (*NBWebhook, error) {
	if id == 0 {
		return nil, fmt.Errorf("netbox: update webhook: id is 0")
	}
	var updated restWebhook
	if err := c.RestPatch("/api/extras/webhooks/"+strconv.FormatUint(uint64(id), 10)+"/", changes, &updated); err != nil {
		return nil, err
	}
	return updated.toNB(), nil
}

func (w EventRuleWrite) toPayload() map[string]any {
	action := w.ActionType
	if action == "" {
		action = "webhook"
	}
	return map[string]any{
		"name":               w.Name,
		"enabled":            w.Enabled,
		"object_types":       w.ObjectTypes,
		"event_types":        w.EventTypes,
		"action_type":        action,
		"action_object_type": "extras.webhook",
		"action_object_id":   w.ActionObjectID,
	}
}

func (c *extrasClient) CreateEventRule(w EventRuleWrite) (*NBEventRule, error) {
	var created restEventRule
	if err := c.RestPost("/api/extras/event-rules/", w.toPayload(), &created); err != nil {
		return nil, err
	}
	return created.toNB(), nil
}

func (c *extrasClient) UpdateEventRule(id uint, changes map[string]any) (*NBEventRule, error) {
	if id == 0 {
		return nil, fmt.Errorf("netbox: update event rule: id is 0")
	}
	var updated restEventRule
	if err := c.RestPatch("/api/extras/event-rules/"+strconv.FormatUint(uint64(id), 10)+"/", changes, &updated); err != nil {
		return nil, err
	}
	return updated.toNB(), nil
}

// HasEvent reports whether the rule lists eventType (e.g. "object_deleted").
func (r *NBEventRule) HasEvent(eventType string) bool {
	if r == nil {
		return false
	}
	for _, e := range r.EventTypes {
		if e == eventType {
			return true
		}
	}
	return false
}

// HasObjectType reports whether the rule lists objectType (e.g. "dcim.device").
func (r *NBEventRule) HasObjectType(objectType string) bool {
	if r == nil {
		return false
	}
	for _, t := range r.ObjectTypes {
		if t == objectType {
			return true
		}
	}
	return false
}

// HasConditions reports whether the rule has a non-empty condition object
// that would restrict which events actually fire.
func (r *NBEventRule) HasConditions() bool {
	if r == nil || r.Conditions == nil {
		return false
	}
	switch v := r.Conditions.(type) {
	case map[string]any:
		return len(v) > 0
	case []any:
		return len(v) > 0
	case string:
		s := strings.TrimSpace(v)
		return s != "" && s != "null" && s != "{}"
	default:
		return true
	}
}
