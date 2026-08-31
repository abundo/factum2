package netbox

import (
	"io"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

type fakeCheckAPI struct {
	hooks       []*netboxtool.NBWebhook
	rules       []*netboxtool.NBEventRule
	fields      map[string]*netboxtool.NBCustomField
	sets        map[string]*netboxtool.NBChoiceSet
	ensuredSets []string
	created     []netboxtool.CustomFieldWrite
	updated     []uint
	hookErr     error
	ruleErr     error
	cfErr       error
}

func (f *fakeCheckAPI) GetWebhooks() ([]*netboxtool.NBWebhook, error) {
	return f.hooks, f.hookErr
}
func (f *fakeCheckAPI) GetEventRules() ([]*netboxtool.NBEventRule, error) {
	return f.rules, f.ruleErr
}
func (f *fakeCheckAPI) GetCustomField(name string) (*netboxtool.NBCustomField, error) {
	if f.cfErr != nil {
		return nil, f.cfErr
	}
	if f.fields == nil {
		return nil, nil
	}
	return f.fields[name], nil
}
func (f *fakeCheckAPI) EnsureCustomFieldChoiceSet(name string, choices [][2]string) (*netboxtool.NBChoiceSet, error) {
	f.ensuredSets = append(f.ensuredSets, name)
	if f.sets == nil {
		f.sets = map[string]*netboxtool.NBChoiceSet{}
	}
	if set := f.sets[name]; set != nil {
		return set, nil
	}
	set := &netboxtool.NBChoiceSet{NetboxID: uint(len(f.sets) + 1), Name: name, ExtraChoices: choices}
	f.sets[name] = set
	return set, nil
}
func (f *fakeCheckAPI) CreateCustomField(w netboxtool.CustomFieldWrite) (*netboxtool.NBCustomField, error) {
	f.created = append(f.created, w)
	if f.fields == nil {
		f.fields = map[string]*netboxtool.NBCustomField{}
	}
	cf := &netboxtool.NBCustomField{
		NetboxID:    uint(100 + len(f.created)),
		Name:        w.Name,
		Type:        w.Type,
		Label:       w.Label,
		Description: w.Description,
		GroupName:   w.GroupName,
		ObjectTypes: w.ObjectTypes,
		ChoiceSetID: w.ChoiceSetID,
	}
	f.fields[w.Name] = cf
	return cf, nil
}
func (f *fakeCheckAPI) UpdateCustomField(id uint, changes map[string]any) (*netboxtool.NBCustomField, error) {
	f.updated = append(f.updated, id)
	for _, cf := range f.fields {
		if cf.NetboxID != id {
			continue
		}
		if v, ok := changes["description"].(string); ok {
			cf.Description = v
		}
		if v, ok := changes["label"].(string); ok {
			cf.Label = v
		}
		if v, ok := changes["group_name"].(string); ok {
			cf.GroupName = v
		}
		if v, ok := changes["object_types"].([]string); ok {
			cf.ObjectTypes = v
		}
		if v, ok := changes["choice_set"].(uint); ok {
			cf.ChoiceSetID = v
		}
		return cf, nil
	}
	return nil, nil
}

func boolPtr(v bool) *bool { return &v }

func allDeviceCFs() map[string]*netboxtool.NBCustomField {
	out := map[string]*netboxtool.NBCustomField{}
	id := uint(1)
	add := func(name, typ string, types []string) {
		out[name] = &netboxtool.NBCustomField{NetboxID: id, Name: name, Type: typ, ObjectTypes: types}
		id++
	}
	dev := []string{"dcim.device"}
	add("alarm_destination", "select", dev)
	add("alarm_timeperiod", "select", dev)
	add("alarm_interfaces", "boolean", dev)
	add("additional_name", "text", dev)
	add("backup_oxidized", "boolean", dev)
	add("connection_method", "select", dev)
	add("location", "text", dev)
	add("monitor_icinga", "boolean", dev)
	add("monitor_librenms", "boolean", dev)
	add("monitor_grafana", "boolean", dev)
	add("parents", "text", dev)
	add("role", "select", []string{"dcim.interface"})
	add("orgno", "text", []string{"tenancy.tenant"})
	add("source", "text", []string{"tenancy.tenant"})
	add("source_id", "text", []string{"tenancy.tenant"})
	return out
}

func factumHook() *netboxtool.NBWebhook {
	return &netboxtool.NBWebhook{
		NetboxID:        3,
		Name:            "factum-sync",
		PayloadURL:      "https://factum.example.com/api/netbox-webhook",
		HTTPMethod:      "POST",
		HTTPContentType: "application/json",
	}
}

func fullRule() *netboxtool.NBEventRule {
	return &netboxtool.NBEventRule{
		NetboxID:       7,
		Name:           "factum",
		Enabled:        true,
		ObjectTypes:    append([]string{}, requiredWebhookObjectTypes...),
		EventTypes:     append([]string{}, requiredWebhookEvents...),
		ActionType:     "webhook",
		ActionObjectID: 3,
	}
}

func baseSettings() *models.Settings {
	return &models.Settings{
		NetboxWebhookSecret: "s",
		PublicBaseURL:       "https://factum.example.com",
	}
}

func TestWebhookURLMatches(t *testing.T) {
	const hook = "https://factum.example.com/api/netbox-webhook"
	if !webhookURLMatches(hook, "https://factum.example.com") {
		t.Fatal("expected match against PublicBaseURL")
	}
	if !webhookURLMatches(hook+"/", "https://factum.example.com/") {
		t.Fatal("expected trailing-slash-tolerant match")
	}
	if webhookURLMatches("https://other.example.com/api/netbox-webhook", "https://factum.example.com") {
		t.Fatal("wrong host must not match when PublicBaseURL is set")
	}
	if !webhookURLMatches("https://other.example.com/api/netbox-webhook", "") {
		t.Fatal("path-only match when PublicBaseURL is empty")
	}
	if webhookURLMatches("https://factum.example.com/api/other", "") {
		t.Fatal("wrong path must not match")
	}
}

func TestCheckDB_OK(t *testing.T) {
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{fullRule()},
		fields: allDeviceCFs(),
	}
	err := CheckDB(api, baseSettings(), CheckOptions{}, jobevent.NewConsoleReporter(io.Discard))
	if err != nil {
		t.Fatalf("CheckDB: %v", err)
	}
}

func TestCheckDB_MissingWebhook(t *testing.T) {
	api := &fakeCheckAPI{fields: allDeviceCFs()}
	err := CheckDB(api, baseSettings(), CheckOptions{}, jobevent.NewConsoleReporter(io.Discard))
	if err == nil {
		t.Fatal("expected failure with no webhook")
	}
}

func TestCheckDB_MissingDeviceDeleteEvent(t *testing.T) {
	rule := fullRule()
	rule.EventTypes = []string{"object_created", "object_updated"}
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{rule},
		fields: allDeviceCFs(),
	}
	err := CheckDB(api, baseSettings(), CheckOptions{}, jobevent.NewConsoleReporter(io.Discard))
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "problem") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckDB_DisabledRuleIgnored(t *testing.T) {
	rule := fullRule()
	rule.Enabled = false
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{rule},
		fields: allDeviceCFs(),
	}
	if err := CheckDB(api, baseSettings(), CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err == nil {
		t.Fatal("disabled rule must not count as coverage")
	}
}

func TestCheckDB_UnionCoverageAcrossRules(t *testing.T) {
	device := fullRule()
	device.Name = "devices"
	device.ObjectTypes = []string{"dcim.device"}
	iface := fullRule()
	iface.NetboxID = 8
	iface.Name = "ifaces"
	iface.ObjectTypes = []string{"dcim.interface", "ipam.ipaddress", "dcim.cable", "dcim.site"}
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{device, iface},
		fields: allDeviceCFs(),
	}
	if err := CheckDB(api, baseSettings(), CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("union of rules should pass: %v", err)
	}
}

func TestCheckDB_BecsOIDOnlyWhenEnabled(t *testing.T) {
	fields := allDeviceCFs()
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{fullRule()},
		fields: fields,
	}
	s := baseSettings()
	if err := CheckDB(api, s, CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("becs_oid must be skipped when BECS is off: %v", err)
	}

	s.BecsEnabled = boolPtr(true)
	if err := CheckDB(api, s, CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err == nil {
		t.Fatal("missing becs_oid must fail without --update")
	}
	if api.fields["becs_oid"] != nil {
		t.Fatal("must not create becs_oid without --update")
	}
	if err := CheckDB(api, s, CheckOptions{Update: true}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("becs_oid should be created with --update: %v", err)
	}
	if api.fields["becs_oid"] == nil {
		t.Fatal("expected becs_oid to be created")
	}
}

func TestCheckDB_LibrenmsIDOnlyWhenEnabled(t *testing.T) {
	fields := allDeviceCFs()
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{fullRule()},
		fields: fields,
	}
	s := baseSettings()
	if err := CheckDB(api, s, CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("librenms_id must be skipped when LibreNMS is off: %v", err)
	}

	s.LibrenmsEnabled = boolPtr(true)
	if err := CheckDB(api, s, CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err == nil {
		t.Fatal("missing librenms_id must fail without --update")
	}
	if err := CheckDB(api, s, CheckOptions{Update: true}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("librenms_id should be created with --update: %v", err)
	}
	if api.fields["librenms_id"] == nil {
		t.Fatal("expected librenms_id to be created")
	}
}

func TestCheckDB_BecsOIDMissingObjectType(t *testing.T) {
	fields := allDeviceCFs()
	fields["becs_oid"] = &netboxtool.NBCustomField{
		NetboxID:    50,
		Name:        "becs_oid",
		Type:        "integer",
		ObjectTypes: []string{"dcim.device"},
	}
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{fullRule()},
		fields: fields,
	}
	s := baseSettings()
	s.BecsEnabled = boolPtr(true)
	if err := CheckDB(api, s, CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err == nil {
		t.Fatal("missing object types must fail without --update")
	}
	if err := CheckDB(api, s, CheckOptions{Update: true}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("should add missing object types with --update: %v", err)
	}
	got := api.fields["becs_oid"].ObjectTypes
	if !contains(got, "dcim.interface") || !contains(got, "ipam.ipaddress") {
		t.Fatalf("object_types = %v, want interface and ipaddress added", got)
	}
}

func TestCheckDB_OpticalRoleWhenEnabled(t *testing.T) {
	fields := allDeviceCFs()
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{fullRule()},
		fields: fields,
	}
	s := baseSettings()
	s.OpticalEnabled = boolPtr(true)
	if err := CheckDB(api, s, CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err == nil {
		t.Fatal("missing optical_role must fail without --update")
	}
	if err := CheckDB(api, s, CheckOptions{Update: true}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("optical_role should be created with --update: %v", err)
	}
	if api.fields["optical_role"] == nil {
		t.Fatal("expected optical_role to be created")
	}
}

func TestCheckDB_OpticalKindAliasSatisfiesOpticalRole(t *testing.T) {
	fields := allDeviceCFs()
	fields["optical_kind"] = &netboxtool.NBCustomField{
		NetboxID:    60,
		Name:        "optical_kind",
		Type:        "text",
		ObjectTypes: []string{"dcim.device", "dcim.interface"},
	}
	api := &fakeCheckAPI{
		hooks:  []*netboxtool.NBWebhook{factumHook()},
		rules:  []*netboxtool.NBEventRule{fullRule()},
		fields: fields,
	}
	s := baseSettings()
	s.OpticalEnabled = boolPtr(true)
	if err := CheckDB(api, s, CheckOptions{}, jobevent.NewConsoleReporter(io.Discard)); err != nil {
		t.Fatalf("optical_kind should satisfy optical_role: %v", err)
	}
}

func TestRequiredCustomFields_Conditionals(t *testing.T) {
	s := baseSettings()
	got := names(requiredCustomFields(s))
	if contains(got, "becs_oid") || contains(got, "librenms_id") || contains(got, "optical_role") {
		t.Fatalf("conditional fields present when flags off: %v", got)
	}
	for _, want := range []string{"source", "source_id", "orgno", "additional_name", "monitor_grafana"} {
		if !contains(got, want) {
			t.Errorf("always-on field %s missing: %v", want, got)
		}
	}

	s.BecsEnabled = boolPtr(true)
	s.LibrenmsEnabled = boolPtr(true)
	s.OpticalEnabled = boolPtr(true)
	got = names(requiredCustomFields(s))
	for _, want := range []string{"becs_oid", "librenms_id", "optical_role", "source", "source_id"} {
		if !contains(got, want) {
			t.Errorf("missing %s when flags on: %v", want, got)
		}
	}
}

func names(needs []cfNeed) []string {
	out := make([]string, len(needs))
	for i, n := range needs {
		out[i] = n.name
	}
	return out
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
