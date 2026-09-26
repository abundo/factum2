package netboxtool

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// ChangelogMark is one NetBox object-change row, reduced to the fields a
// delta sync needs. Time is truncated to microseconds, matching the
// timestamp NetBox stores.
type ChangelogMark struct {
	At time.Time
	ID uint
}

// ObjectChange is one row from /api/core/object-changes/ (NetBox 4).
// Snapshots are ignored: they are flat and are not the inventory factum
// reconciles.
type ObjectChange struct {
	ID          uint
	Time        time.Time
	Action      string
	ChangedType string
	ChangedID   uint
	RelatedType string
	RelatedID   uint
}

type objectChangeREST struct {
	ID                uint      `json:"id"`
	Time              time.Time `json:"time"`
	Action            nbChoice  `json:"action"`
	ChangedObjectType nbType    `json:"changed_object_type"`
	ChangedObjectID   uint      `json:"changed_object_id"`
	RelatedObjectType nbType    `json:"related_object_type"`
	RelatedObjectID   *uint     `json:"related_object_id"`
}

func (c objectChangeREST) toChange() ObjectChange {
	return ObjectChange{
		ID:          c.ID,
		Time:        c.Time.UTC().Truncate(time.Microsecond),
		Action:      normChangeAction(c.Action.Value),
		ChangedType: c.ChangedObjectType.Value,
		ChangedID:   c.ChangedObjectID,
		RelatedType: c.RelatedObjectType.Value,
		RelatedID:   uintPtr(c.RelatedObjectID),
	}
}

// nbChoice accepts NetBox's {"value","label"} choice or a bare string.
type nbChoice struct {
	Value string
}

func (c *nbChoice) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		c.Value = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &c.Value)
	}
	var obj struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	c.Value = obj.Value
	return nil
}

// nbType accepts "dcim.device" or {"app_label","model"}.
type nbType struct {
	Value string
}

func (t *nbType) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		t.Value = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &t.Value)
	}
	var obj struct {
		App   string `json:"app_label"`
		Model string `json:"model"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	if obj.Value != "" {
		t.Value = obj.Value
		return nil
	}
	if obj.App != "" && obj.Model != "" {
		t.Value = obj.App + "." + obj.Model
	}
	return nil
}

func uintPtr(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}

func normChangeAction(s string) string {
	switch s {
	case "created", "create":
		return "create"
	case "updated", "update":
		return "update"
	case "deleted", "delete":
		return "delete"
	default:
		return s
	}
}

// ChangelogHead is the newest object-change, or (now, 0) when the
// changelog is empty. The endpoint is /api/core/object-changes/ (NetBox 4).
func (nb *NetboxClient) ChangelogHead() (ChangelogMark, error) {
	q := url.Values{}
	q.Set("ordering", "-time")
	q.Set("limit", "1")
	var page struct {
		Results []objectChangeREST `json:"results"`
	}
	if err := nb.restGet("/api/core/object-changes/?"+q.Encode(), &page); err != nil {
		return ChangelogMark{}, err
	}
	if len(page.Results) == 0 {
		return ChangelogMark{At: time.Now().UTC().Truncate(time.Microsecond)}, nil
	}
	ch := page.Results[0].toChange()
	return ChangelogMark{At: ch.Time, ID: ch.ID}, nil
}

// ObjectChangesSince lists object-changes at or after since, oldest first.
// time_after is inclusive, so the caller skips the cursor row by id.
func (nb *NetboxClient) ObjectChangesSince(since time.Time) ([]ObjectChange, error) {
	q := url.Values{}
	q.Set("time_after", since.UTC().Format(time.RFC3339Nano))
	q.Set("ordering", "time")
	q.Set("limit", strconv.Itoa(restPageSize))
	endpoint := "/api/core/object-changes/?" + q.Encode()

	var all []ObjectChange
	for endpoint != "" {
		var page struct {
			Next    *string            `json:"next"`
			Results []objectChangeREST `json:"results"`
		}
		if err := nb.restGet(endpoint, &page); err != nil {
			return nil, err
		}
		for _, row := range page.Results {
			all = append(all, row.toChange())
		}
		if page.Next == nil || *page.Next == "" {
			break
		}
		next, err := url.Parse(*page.Next)
		if err != nil {
			return nil, fmt.Errorf("netbox object-changes: parse next page url: %w", err)
		}
		endpoint = "/api/core/object-changes/?" + next.RawQuery
	}
	return all, nil
}

// InterfaceParent returns the device or VM id that owns a dcim.interface
// (vm=false) or virtualization.vminterface (vm=true). ok is false when
// NetBox no longer has the interface.
func (nb *NetboxClient) InterfaceParent(vm bool, ifaceID uint) (parentID uint, ok bool, err error) {
	path := "/api/dcim/interfaces/" + strconv.FormatUint(uint64(ifaceID), 10) + "/"
	key := "device"
	if vm {
		path = "/api/virtualization/interfaces/" + strconv.FormatUint(uint64(ifaceID), 10) + "/"
		key = "virtual_machine"
	}
	var raw map[string]json.RawMessage
	found, err := nb.restGetOptional(path, &raw)
	if err != nil || !found {
		return 0, false, err
	}
	body, exists := raw[key]
	if !exists || string(body) == "null" {
		return 0, false, nil
	}
	var ref struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(body, &ref); err != nil {
		return 0, false, err
	}
	if ref.ID == 0 {
		return 0, false, nil
	}
	return ref.ID, true, nil
}
