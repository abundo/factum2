package netbox

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/abundo/netboxtool"
)

// Custom field type slugs as accepted by POST/PATCH /api/extras/custom-fields/.
const (
	cfTypeText    = "text"
	cfTypeInteger = "integer"
	cfTypeBoolean = "boolean"
	cfTypeSelect  = "select"
)

// NBChoiceSet is extras.CustomFieldChoiceSet. ExtraChoices are [value, label]
// pairs Netbox stores on the set (not on the custom field).
type NBChoiceSet struct {
	NetboxID     uint
	Name         string
	ExtraChoices [][2]string
}

// CustomFieldWrite is the body for creating a custom field.
type CustomFieldWrite struct {
	Name        string
	Type        string
	Label       string
	Description string
	GroupName   string
	Required    bool
	ObjectTypes []string
	ChoiceSetID uint
}

type restChoiceSet struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	ExtraChoices any    `json:"extra_choices"`
}

func (r restChoiceSet) toNB() *NBChoiceSet {
	return &NBChoiceSet{
		NetboxID:     r.ID,
		Name:         r.Name,
		ExtraChoices: parseExtraChoices(r.ExtraChoices),
	}
}

func parseExtraChoices(raw any) [][2]string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out [][2]string
	for _, item := range list {
		pair, ok := item.([]any)
		if !ok || len(pair) < 2 {
			continue
		}
		val, _ := pair[0].(string)
		lab, _ := pair[1].(string)
		if val == "" {
			continue
		}
		if lab == "" {
			lab = val
		}
		out = append(out, [2]string{val, lab})
	}
	return out
}

type restChoiceSetList struct {
	Results []restChoiceSet `json:"results"`
}

func (c *extrasClient) GetCustomFieldChoiceSet(name string) (*NBChoiceSet, error) {
	var page restChoiceSetList
	if err := c.RestGet("/api/extras/custom-field-choice-sets/?name="+url.QueryEscape(name), &page); err != nil {
		return nil, err
	}
	if len(page.Results) == 0 {
		return nil, nil
	}
	return page.Results[0].toNB(), nil
}

// EnsureCustomFieldChoiceSet returns the named choice set, creating it or
// adding any missing extra_choices. Existing choices are never removed —
// Netbox forbids shrinking a set that objects already use.
func (c *extrasClient) EnsureCustomFieldChoiceSet(name string, choices [][2]string) (*NBChoiceSet, error) {
	set, err := c.GetCustomFieldChoiceSet(name)
	if err != nil {
		return nil, err
	}
	if set == nil {
		payload := map[string]any{
			"name":          name,
			"extra_choices": extraChoicesPayload(choices),
		}
		var created restChoiceSet
		if err := c.RestPost("/api/extras/custom-field-choice-sets/", payload, &created); err != nil {
			return nil, err
		}
		return created.toNB(), nil
	}
	merged, added := mergeChoices(set.ExtraChoices, choices)
	if !added {
		return set, nil
	}
	var updated restChoiceSet
	if err := c.RestPatch("/api/extras/custom-field-choice-sets/"+strconv.FormatUint(uint64(set.NetboxID), 10)+"/",
		map[string]any{"extra_choices": extraChoicesPayload(merged)}, &updated); err != nil {
		return nil, err
	}
	return updated.toNB(), nil
}

func extraChoicesPayload(choices [][2]string) [][]string {
	out := make([][]string, 0, len(choices))
	for _, ch := range choices {
		lab := ch[1]
		if lab == "" {
			lab = ch[0]
		}
		out = append(out, []string{ch[0], lab})
	}
	return out
}

func mergeChoices(have, want [][2]string) (merged [][2]string, added bool) {
	seen := make(map[string]bool, len(have))
	merged = append(merged, have...)
	for _, ch := range have {
		seen[ch[0]] = true
	}
	for _, ch := range want {
		if ch[0] == "" || seen[ch[0]] {
			continue
		}
		merged = append(merged, ch)
		seen[ch[0]] = true
		added = true
	}
	return merged, added
}

func (w CustomFieldWrite) toPayload() map[string]any {
	p := map[string]any{
		"name":         w.Name,
		"type":         w.Type,
		"object_types": w.ObjectTypes,
		"required":     w.Required,
	}
	if w.Label != "" {
		p["label"] = w.Label
	}
	if w.Description != "" {
		p["description"] = w.Description
	}
	if w.GroupName != "" {
		p["group_name"] = w.GroupName
	}
	if w.ChoiceSetID != 0 {
		p["choice_set"] = w.ChoiceSetID
	}
	return p
}

func (c *extrasClient) CreateCustomField(w CustomFieldWrite) (*netboxtool.NBCustomField, error) {
	if err := c.RestPost("/api/extras/custom-fields/", w.toPayload(), nil); err != nil {
		return nil, err
	}
	cf, err := c.GetCustomField(w.Name)
	if err != nil {
		return nil, err
	}
	if cf == nil {
		return nil, fmt.Errorf("netbox: created custom field %q but lookup returned nothing", w.Name)
	}
	return cf, nil
}

func (c *extrasClient) UpdateCustomField(id uint, changes map[string]any) (*netboxtool.NBCustomField, error) {
	if id == 0 {
		return nil, fmt.Errorf("netbox: update custom field: id is 0")
	}
	if err := c.RestPatch("/api/extras/custom-fields/"+strconv.FormatUint(uint64(id), 10)+"/", changes, nil); err != nil {
		return nil, err
	}
	if name, _ := changes["name"].(string); name != "" {
		return c.GetCustomField(name)
	}
	return &netboxtool.NBCustomField{NetboxID: id}, nil
}
