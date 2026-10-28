package netbox

import (
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/abundo/factum2/internal/netboxtool"
)

func TestEnsureCustomField_CreatesText(t *testing.T) {
	api := &fakeCheckAPI{}
	res, err := ensureCustomField(api, cfSpec{
		name: "location", typ: cfTypeText,
		objectTypes: []string{"dcim.device"},
		description: "where",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "created" {
		t.Fatalf("action = %q, want created", res.action)
	}
	if api.fields["location"] == nil {
		t.Fatal("field not stored")
	}
}

func TestEnsureCustomField_RefusesSelectWithoutChoices(t *testing.T) {
	api := &fakeCheckAPI{}
	_, err := ensureCustomField(api, cfSpec{
		name: "role", typ: cfTypeSelect,
		objectTypes: []string{"dcim.interface"},
	}, true)
	if err == nil || !strings.Contains(err.Error(), "operator-managed") {
		t.Fatalf("want operator-managed error, got %v", err)
	}
	if len(api.created) != 0 {
		t.Fatal("must not create a select field without choices")
	}
}

func TestEnsureCustomField_CreatesRoleWithSeedChoices(t *testing.T) {
	api := &fakeCheckAPI{}
	spec := customFieldSpecs(&models.Settings{})
	var role cfSpec
	for _, s := range spec {
		if s.name == "role" {
			role = s
		}
	}
	if len(role.choices) < 2 {
		t.Fatal("role must ship seed choices so check --update can create it")
	}
	res, err := ensureCustomField(api, role, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "created" {
		t.Fatalf("action = %q, want created", res.action)
	}
	if len(api.ensuredSets) != 1 || api.ensuredSets[0] != "factum_role" {
		t.Fatalf("ensuredSets = %v", api.ensuredSets)
	}
	if api.fields["role"] == nil || api.fields["role"].ChoiceSetID == 0 {
		t.Fatal("expected role field with a choice set")
	}
}

func TestEnsureCustomField_CreatesAlarmTimeperiodWithSeedChoices(t *testing.T) {
	api := &fakeCheckAPI{}
	spec := customFieldSpecs(&models.Settings{})
	var timeperiod cfSpec
	for _, s := range spec {
		if s.name == "alarm_timeperiod" {
			timeperiod = s
		}
	}
	res, err := ensureCustomField(api, timeperiod, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "created" {
		t.Fatalf("action = %q, want created", res.action)
	}
	if len(api.ensuredSets) != 1 || api.ensuredSets[0] != "factum_alarm_timeperiod" {
		t.Fatalf("ensuredSets = %v", api.ensuredSets)
	}
	if got := api.sets["factum_alarm_timeperiod"]; got == nil || len(got.ExtraChoices) != 3 {
		t.Fatalf("choices = %v, want 3 seed values", got)
	}
}

func TestEnsureCustomField_DoesNotTouchExistingAlarmDestinationChoices(t *testing.T) {
	api := &fakeCheckAPI{fields: map[string]*netboxtool.NBCustomField{
		"alarm_destination": {
			NetboxID: 3, Name: "alarm_destination", Type: "select",
			ObjectTypes: []string{"dcim.device"}, ChoiceSetID: 9,
		},
	}}
	spec := customFieldSpecs(&models.Settings{})
	var dest cfSpec
	for _, s := range spec {
		if s.name == "alarm_destination" {
			dest = s
		}
	}
	res, err := ensureCustomField(api, dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "ok" {
		t.Fatalf("action = %q, want ok (no metadata drift)", res.action)
	}
	if len(api.ensuredSets) != 0 {
		t.Fatalf("must not update selections on an existing field, ensured %v", api.ensuredSets)
	}
}

func TestEnsureCustomField_CreatesSelectWithChoices(t *testing.T) {
	api := &fakeCheckAPI{}
	res, err := ensureCustomField(api, cfSpec{
		name: "connection_method", typ: cfTypeSelect,
		objectTypes: []string{"dcim.device"},
		choices:     [][2]string{{"ssh", "ssh"}, {"telnet", "telnet"}},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "created" {
		t.Fatalf("action = %q, want created", res.action)
	}
	if api.fields["connection_method"].ChoiceSetID == 0 {
		t.Fatal("expected choice set attached")
	}
}

func TestEnsureCustomField_TypeMismatch(t *testing.T) {
	api := &fakeCheckAPI{fields: map[string]*netboxtool.NBCustomField{
		"becs_oid": {NetboxID: 1, Name: "becs_oid", Type: "text", ObjectTypes: []string{"dcim.device"}},
	}}
	_, err := ensureCustomField(api, cfSpec{
		name: "becs_oid", typ: cfTypeInteger,
		objectTypes: []string{"dcim.device"},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("want type error, got %v", err)
	}
}

func TestEnsureCustomField_UpdatesDescriptionAndTypes(t *testing.T) {
	api := &fakeCheckAPI{fields: map[string]*netboxtool.NBCustomField{
		"location": {NetboxID: 2, Name: "location", Type: "text", ObjectTypes: []string{"dcim.device"}},
	}}
	res, err := ensureCustomField(api, cfSpec{
		name: "location", typ: cfTypeText,
		objectTypes: []string{"dcim.device"},
		description: "Free-text describing the location of the device",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "updated" {
		t.Fatalf("action = %q, want updated", res.action)
	}
	if api.fields["location"].Description == "" {
		t.Fatal("description not written")
	}
}

func TestEnsureCustomField_RewritesStringBooleanDefault(t *testing.T) {
	api := &fakeCheckAPI{fields: map[string]*netboxtool.NBCustomField{
		"alarm_interfaces": {
			NetboxID: 3, Name: "alarm_interfaces", Type: cfTypeBoolean,
			ObjectTypes: []string{"dcim.device"},
			Description: "If true, LibreNMS will generate alerts for all interfaces",
			Default:     "false",
		},
		"monitor_librenms": {
			NetboxID: 9, Name: "monitor_librenms", Type: cfTypeBoolean,
			ObjectTypes: []string{"dcim.device"},
			Description: "If true the device will be monitored by LibreNMS",
			Default:     "TRUE",
		},
		"backup_oxidized": {
			NetboxID: 5, Name: "backup_oxidized", Type: cfTypeBoolean,
			ObjectTypes: []string{"dcim.device"},
			Default:     false,
		},
	}}
	specs := map[string]cfSpec{}
	for _, spec := range customFieldSpecs(&models.Settings{}) {
		specs[spec.name] = spec
	}

	res, err := ensureCustomField(api, specs["alarm_interfaces"], true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "updated" || !strings.Contains(res.detail, "default") {
		t.Fatalf("alarm_interfaces = %+v, want updated default", res)
	}
	if api.fields["alarm_interfaces"].Default != false {
		t.Fatalf("alarm_interfaces default = %#v, want false", api.fields["alarm_interfaces"].Default)
	}

	res, err = ensureCustomField(api, specs["monitor_librenms"], false)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "drift" || !strings.Contains(res.detail, "default") {
		t.Fatalf("monitor_librenms verify = %+v, want drift default", res)
	}
	if api.fields["monitor_librenms"].Default != "TRUE" {
		t.Fatal("verify-only must not write the default")
	}

	res, err = ensureCustomField(api, specs["backup_oxidized"], true)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "ok" {
		t.Fatalf("backup_oxidized = %+v, want ok", res)
	}
}

func TestEnsureCustomField_VerifyOnlyDoesNotCreate(t *testing.T) {
	api := &fakeCheckAPI{}
	_, err := ensureCustomField(api, cfSpec{
		name: "location", typ: cfTypeText,
		objectTypes: []string{"dcim.device"},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("want not defined, got %v", err)
	}
	if len(api.created) != 0 {
		t.Fatal("verify-only must not create")
	}
}
