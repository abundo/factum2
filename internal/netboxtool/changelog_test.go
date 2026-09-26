package netboxtool

import (
	"encoding/json"
	"testing"
	"time"
)

func TestObjectChangeRESTShapes(t *testing.T) {
	raw := []byte(`{
		"id": 15,
		"time": "2026-09-26T12:00:00.123456Z",
		"action": {"value": "update", "label": "Updated"},
		"changed_object_type": "dcim.interface",
		"changed_object_id": 9,
		"related_object_type": {"app_label": "dcim", "model": "device"},
		"related_object_id": 4
	}`)
	var row objectChangeREST
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	ch := row.toChange()
	if ch.Action != "update" || ch.ChangedType != "dcim.interface" || ch.ChangedID != 9 {
		t.Fatalf("change = %+v", ch)
	}
	if ch.RelatedType != "dcim.device" || ch.RelatedID != 4 {
		t.Fatalf("related = %s %d", ch.RelatedType, ch.RelatedID)
	}
	if !ch.Time.Equal(time.Date(2026, 9, 26, 12, 0, 0, 123456000, time.UTC)) {
		t.Fatalf("time = %s", ch.Time)
	}

	bare := []byte(`{"id": 2, "time": "2026-09-26T12:00:00Z", "action": "delete", "changed_object_type": "dcim.cable", "changed_object_id": 3, "related_object_type": null, "related_object_id": null}`)
	row = objectChangeREST{}
	if err := json.Unmarshal(bare, &row); err != nil {
		t.Fatal(err)
	}
	ch = row.toChange()
	if ch.Action != "delete" || ch.ChangedType != "dcim.cable" || ch.RelatedType != "" || ch.RelatedID != 0 {
		t.Fatalf("bare change = %+v", ch)
	}
}
