package netbox

import (
	"fmt"
	"strings"

	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

// cfSpec is the desired extras.CustomField shape. Check creates the field
// when missing and patches mutable attributes when they drift. Type is
// never changed: Netbox rejects that. Selection fields without Choices
// cannot be created (Netbox requires a choice set of at least two values).
type cfSpec struct {
	name        string
	aliases     []string
	objectTypes []string
	typ         string
	label       string
	description string
	group       string
	// choices are [value, label] pairs used to create a choice set when
	// the field does not exist. Empty means we cannot create a select
	// field (Netbox requires a set of at least two values).
	choices [][2]string
	// choicesCreateOnly: use Choices only when creating the field. Never
	// add/merge choices on an existing field (alarm_destination /
	// alarm_timeperiod — operators own those lists after first create).
	choicesCreateOnly bool
}

func (s cfSpec) toNeed() cfNeed {
	return cfNeed{name: s.name, aliases: s.aliases, objectTypes: s.objectTypes, wantType: s.typ}
}

func (s cfSpec) choiceSetName() string {
	return "factum_" + s.name
}

// customFieldSpecs is the field catalogue factum will create/update.
// Integration-specific rows are omitted when that Settings flag is off.
func customFieldSpecs(s *models.Settings) []cfSpec {
	device := []string{"dcim.device"}
	specs := []cfSpec{
		{name: "alarm_destination", objectTypes: device, typ: netboxtool.CFTypeSelect,
			choicesCreateOnly: true,
			choices: [][2]string{
				{"notify@example.com", "notify@example.com"},
				{"support@example.com", "support@example.com"},
			}},
		{name: "alarm_interfaces", objectTypes: device, typ: netboxtool.CFTypeBoolean,
			description: "If true, LibreNMS will generate alerts for all interfaces"},
		{name: "alarm_timeperiod", objectTypes: device, typ: netboxtool.CFTypeSelect,
			choicesCreateOnly: true,
			choices: [][2]string{
				{"sla1 workhours 07-17", "sla1 workhours 07-17"},
				{"sla2 extended workhours 07-22", "sla2 extended workhours 07-22"},
				{"sla3 24x7", "sla3 24x7"},
			}},
		{name: "additional_name", objectTypes: device, typ: netboxtool.CFTypeText,
			description: "Alias name"},
		{name: "backup_oxidized", objectTypes: device, typ: netboxtool.CFTypeBoolean},
		{name: "connection_method", objectTypes: device, typ: netboxtool.CFTypeSelect,
			description: "How to connect to the device",
			choices:     [][2]string{{"ssh", "ssh"}, {"telnet", "telnet"}}},
		{name: "location", objectTypes: device, typ: netboxtool.CFTypeText,
			description: "Free-text describing the location of the device"},
		{name: "monitor_icinga", objectTypes: device, typ: netboxtool.CFTypeBoolean,
			description: "If true the device will be monitored by Icinga"},
		{name: "monitor_librenms", objectTypes: device, typ: netboxtool.CFTypeBoolean,
			description: "If true the device will be monitored by LibreNMS"},
		{name: "parents", objectTypes: device, typ: netboxtool.CFTypeText,
			description: "Comma-separated list of parents. If all parents are down, no alarms will be generated for this device"},
		{name: "role", objectTypes: []string{"dcim.interface"}, typ: netboxtool.CFTypeSelect},
		{name: "orgno", objectTypes: []string{"tenancy.tenant"}, typ: netboxtool.CFTypeText,
			label: "Organisationnr"},
		{name: "source", objectTypes: []string{"tenancy.tenant"}, typ: netboxtool.CFTypeText,
			label: "Source system", group: "sync",
			description: "Identifies where the data comes from"},
		{name: "source_id", objectTypes: []string{"tenancy.tenant"}, typ: netboxtool.CFTypeText,
			label: "Source system ID", group: "sync",
			description: "ID of the data in the source system"},
	}
	if settingOn(s.BecsEnabled) {
		specs = append(specs, cfSpec{
			name:        "becs_oid",
			objectTypes: []string{"dcim.device", "dcim.interface", "ipam.ipaddress"},
			typ:         netboxtool.CFTypeInteger,
			group:       "sync",
			description: "Identifies the element in BECS",
		})
	}
	if settingOn(s.LibrenmsEnabled) {
		specs = append(specs, cfSpec{
			name:        "librenms_id",
			objectTypes: []string{"dcim.device", "virtualization.virtualmachine"},
			typ:         netboxtool.CFTypeInteger,
			group:       "sync",
			description: "Identifies the element in LibreNMS",
		})
	}
	if settingOn(s.OpticalEnabled) {
		specs = append(specs, cfSpec{
			name:        "optical_role",
			aliases:     []string{"optical_kind"},
			objectTypes: []string{"dcim.device", "dcim.interface"},
			typ:         netboxtool.CFTypeText,
			description: "On a device: chassis kind (roadm, wdm_shelf, ila, passive). On an interface: port role (txp_client, txp_line, roadm_adddrop, roadm_degree, fiber_port).",
		})
	}
	return specs
}

func requiredCustomFields(s *models.Settings) []cfNeed {
	specs := customFieldSpecs(s)
	needs := make([]cfNeed, len(specs))
	for i, spec := range specs {
		needs[i] = spec.toNeed()
	}
	return needs
}

type cfApplyResult struct {
	action string // "ok", "created", "updated"
	detail string
}

func ensureCustomField(nb checkAPI, spec cfSpec, apply bool) (cfApplyResult, error) {
	cf, foundName, err := lookupCustomField(nb, spec.toNeed())
	if err != nil {
		return cfApplyResult{}, err
	}
	if cf == nil {
		if !apply {
			return cfApplyResult{action: "missing"}, fmt.Errorf("custom field %q is not defined (need %s)", spec.name, strings.Join(spec.objectTypes, ", "))
		}
		return createCustomField(nb, spec)
	}
	if spec.typ != "" && cf.Type != "" && cf.Type != spec.typ {
		return cfApplyResult{}, fmt.Errorf("custom field %q has type %q, want %q (Netbox does not allow changing type)", foundName, cf.Type, spec.typ)
	}
	changes, notes := customFieldDiff(cf, spec)
	if !apply {
		if missing := missingStrings(cf.ObjectTypes, spec.objectTypes); len(missing) > 0 {
			return cfApplyResult{action: "missing"}, fmt.Errorf("custom field %q is not assigned to %s", foundName, strings.Join(missing, ", "))
		}
		if len(notes) > 0 {
			return cfApplyResult{action: "drift", detail: strings.Join(notes, ", ")}, nil
		}
		return cfApplyResult{action: "ok", detail: spec.typ + " on " + strings.Join(spec.objectTypes, ", ")}, nil
	}
	return applyCustomFieldUpdate(nb, cf, spec, changes, notes)
}

func createCustomField(nb checkAPI, spec cfSpec) (cfApplyResult, error) {
	if spec.typ == netboxtool.CFTypeSelect && len(spec.choices) == 0 {
		return cfApplyResult{}, fmt.Errorf("custom field %q is a selection field with operator-managed choices; create it in Netbox first", spec.name)
	}
	write := netboxtool.CustomFieldWrite{
		Name:        spec.name,
		Type:        spec.typ,
		Label:       spec.label,
		Description: spec.description,
		GroupName:   spec.group,
		ObjectTypes: spec.objectTypes,
	}
	if len(spec.choices) > 0 {
		set, err := nb.EnsureCustomFieldChoiceSet(spec.choiceSetName(), spec.choices)
		if err != nil {
			return cfApplyResult{}, fmt.Errorf("choice set %s: %w", spec.choiceSetName(), err)
		}
		write.ChoiceSetID = set.NetboxID
	}
	if _, err := nb.CreateCustomField(write); err != nil {
		return cfApplyResult{}, err
	}
	return cfApplyResult{action: "created", detail: spec.typ + " on " + strings.Join(spec.objectTypes, ", ")}, nil
}

func customFieldDiff(cf *netboxtool.NBCustomField, spec cfSpec) (map[string]any, []string) {
	changes := map[string]any{}
	var notes []string
	if len(spec.choices) > 0 && !spec.choicesCreateOnly && cf.ChoiceSetID == 0 {
		notes = append(notes, "choice_set")
	}
	missing := missingStrings(cf.ObjectTypes, spec.objectTypes)
	if len(missing) > 0 {
		changes["object_types"] = append(append([]string{}, cf.ObjectTypes...), missing...)
		notes = append(notes, "object_types +"+strings.Join(missing, ","))
	}
	if spec.description != "" && cf.Description != spec.description {
		changes["description"] = spec.description
		notes = append(notes, "description")
	}
	if spec.label != "" && cf.Label != spec.label {
		changes["label"] = spec.label
		notes = append(notes, "label")
	}
	if spec.group != "" && cf.GroupName != spec.group {
		changes["group_name"] = spec.group
		notes = append(notes, "group")
	}
	return changes, notes
}

func applyCustomFieldUpdate(nb checkAPI, cf *netboxtool.NBCustomField, spec cfSpec, changes map[string]any, notes []string) (cfApplyResult, error) {
	if len(spec.choices) > 0 && !spec.choicesCreateOnly {
		if cf.ChoiceSetID == 0 {
			set, err := nb.EnsureCustomFieldChoiceSet(spec.choiceSetName(), spec.choices)
			if err != nil {
				return cfApplyResult{}, fmt.Errorf("choice set %s: %w", spec.choiceSetName(), err)
			}
			changes["choice_set"] = set.NetboxID
		} else {
			_, _ = nb.EnsureCustomFieldChoiceSet(spec.choiceSetName(), spec.choices)
		}
	}
	if len(changes) == 0 {
		return cfApplyResult{action: "ok", detail: spec.typ + " on " + strings.Join(spec.objectTypes, ", ")}, nil
	}
	if _, err := nb.UpdateCustomField(cf.NetboxID, changes); err != nil {
		return cfApplyResult{}, err
	}
	return cfApplyResult{action: "updated", detail: strings.Join(notes, ", ")}, nil
}

func missingStrings(have, want []string) []string {
	seen := make(map[string]bool, len(have))
	for _, h := range have {
		seen[h] = true
	}
	var missing []string
	for _, w := range want {
		if !seen[w] {
			missing = append(missing, w)
		}
	}
	return missing
}
