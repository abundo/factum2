package netbox

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

// factumWebhookPath is the path Netbox must POST to (see web.ApiNetboxWebhook).
const factumWebhookPath = "/api/netbox-webhook"

// requiredWebhookEvents are the Event Rule event types that correspond to
// Netbox's created/updated/deleted webhook payload.event values.
var requiredWebhookEvents = []string{"object_created", "object_updated", "object_deleted"}

// requiredWebhookObjectTypes is what ApiNetboxWebhook actually handles.
var requiredWebhookObjectTypes = []string{
	"dcim.device",
	"dcim.interface",
	"ipam.ipaddress",
	"dcim.cable",
	"dcim.site",
}

// checkAPI is the Netbox extras surface Check needs so tests can stub
// list/lookup/create without a live Netbox.
type checkAPI interface {
	GetWebhooks() ([]*NBWebhook, error)
	GetEventRules() ([]*NBEventRule, error)
	CreateWebhook(w WebhookWrite) (*NBWebhook, error)
	UpdateWebhook(id uint, changes map[string]any) (*NBWebhook, error)
	CreateEventRule(w EventRuleWrite) (*NBEventRule, error)
	UpdateEventRule(id uint, changes map[string]any) (*NBEventRule, error)
	GetCustomField(name string) (*netboxtool.NBCustomField, error)
	EnsureCustomFieldChoiceSet(name string, choices [][2]string) (*NBChoiceSet, error)
	CreateCustomField(w CustomFieldWrite) (*netboxtool.NBCustomField, error)
	UpdateCustomField(id uint, changes map[string]any) (*netboxtool.NBCustomField, error)
}

// cfNeed is one custom field factum expects to exist on the given object types.
type cfNeed struct {
	name string
	// aliases are alternate Netbox custom-field names that satisfy this
	// need (e.g. optical_role also accepts the older optical_kind name).
	aliases     []string
	objectTypes []string
	// wantType is a Netbox extras.CustomField type value ("integer",
	// "text", ...) checked when non-empty. Empty means any type is fine.
	wantType string
}

// CheckOptions controls Check. Update=false (default) only reports.
// Update=true creates/updates custom fields, the factum webhook, and its
// event rule when they are missing or have drifted.
type CheckOptions struct {
	Update bool
}

// Check verifies Netbox webhooks/event rules and the custom fields factum
// needs. Without Update it only reports. With Update it creates missing
// webhooks, event rules and fields, and patches mutable attributes (custom
// field type is never changed).
func Check(c *util.ConfigRoot, opts CheckOptions, reporter jobevent.Reporter) error {
	db, err := util.ConnectDatabase(&c.DB)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   settings.NetboxApiURL,
		Token: settings.NetboxApiToken,
	})
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	return CheckDB(&extrasClient{nb}, settings, opts, reporter)
}

// CheckDB is Check against an already-built client and Settings row.
func CheckDB(nb checkAPI, settings *models.Settings, opts CheckOptions, reporter jobevent.Reporter) error {
	var failed int
	fail := func(format string, args ...any) {
		failed++
		reporter.Emit(jobevent.Error, format, args...)
	}
	ok := func(format string, args ...any) {
		reporter.Emit(jobevent.Info, "ok: "+format, args...)
	}
	warn := func(format string, args ...any) {
		reporter.Emit(jobevent.Warning, format, args...)
	}

	if settings.NetboxWebhookSecret == "" {
		fail("webhook secret is not set in factum Settings (NetBox tab)")
	} else {
		ok("webhook secret is set in factum Settings")
	}

	hooks, err := nb.GetWebhooks()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	rules, err := nb.GetEventRules()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	if settings.PublicBaseURL == "" {
		warn("PublicBaseURL is not set; matching webhooks by path %s only", factumWebhookPath)
	}
	matched, err := applyWebhookEndpoint(nb, hooks, settings, opts.Update, fail, ok)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if err := applyWebhookEvents(nb, matched, rules, opts.Update, fail, ok, warn); err != nil {
		reporter.EmitErr(err)
		return err
	}

	for _, spec := range customFieldSpecs(settings) {
		res, err := ensureCustomField(nb, spec, opts.Update)
		if err != nil {
			fail("%s", err.Error())
			continue
		}
		switch res.action {
		case "created":
			ok("created custom field %q (%s)", spec.name, res.detail)
		case "updated":
			ok("updated custom field %q (%s)", spec.name, res.detail)
		case "drift":
			warn("custom field %q would update (%s); pass --update", spec.name, res.detail)
		default:
			ok("custom field %q (%s)", spec.name, res.detail)
		}
	}
	for _, skipped := range skippedCustomFields(settings) {
		warn("skipped custom field %q (%s)", skipped.name, skipped.reason)
	}

	if failed > 0 {
		return fmt.Errorf("netbox check: %d problem(s)", failed)
	}
	reporter.Emit(jobevent.Info, "Netbox check: all required webhooks and custom fields are present")
	return nil
}

func lookupCustomField(nb checkAPI, need cfNeed) (*netboxtool.NBCustomField, string, error) {
	names := append([]string{need.name}, need.aliases...)
	for _, name := range names {
		cf, err := nb.GetCustomField(name)
		if err != nil {
			return nil, "", err
		}
		if cf != nil {
			return cf, name, nil
		}
	}
	return nil, "", nil
}

func settingOn(v *bool) bool {
	return v != nil && *v
}

func expectedWebhookURL(publicBase string) string {
	publicBase = strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if publicBase == "" {
		return ""
	}
	return publicBase + factumWebhookPath
}

func matchingWebhooks(hooks []*NBWebhook, publicBase string) []*NBWebhook {
	var out []*NBWebhook
	for _, h := range hooks {
		if webhookURLMatches(h.PayloadURL, publicBase) {
			out = append(out, h)
		}
	}
	return out
}

// webhookURLMatches reports whether payloadURL is factum's webhook endpoint.
// When publicBase is set, scheme+host+path must match PublicBaseURL plus
// /api/netbox-webhook; otherwise any URL whose path is that route counts.
func webhookURLMatches(payloadURL, publicBase string) bool {
	got, err := url.Parse(payloadURL)
	if err != nil || !strings.EqualFold(strings.TrimRight(got.Path, "/"), factumWebhookPath) {
		return false
	}
	wantURL := expectedWebhookURL(publicBase)
	if wantURL == "" {
		return true
	}
	want, err := url.Parse(wantURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(got.Scheme, want.Scheme) &&
		strings.EqualFold(got.Host, want.Host) &&
		strings.EqualFold(strings.TrimRight(got.Path, "/"), strings.TrimRight(want.Path, "/"))
}

func webhookEndpointOK(h *NBWebhook) error {
	if !strings.EqualFold(h.HTTPMethod, "POST") {
		return fmt.Errorf("http_method is %q, want POST", h.HTTPMethod)
	}
	ct := strings.ToLower(h.HTTPContentType)
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("http_content_type is %q, want application/json", h.HTTPContentType)
	}
	if strings.TrimSpace(h.BodyTemplate) != "" {
		return fmt.Errorf("body_template is set; factum expects Netbox's default payload")
	}
	return nil
}

func webhookEndpointChanges(h *NBWebhook) map[string]any {
	changes := map[string]any{}
	if !strings.EqualFold(h.HTTPMethod, "POST") {
		changes["http_method"] = "POST"
	}
	ct := strings.ToLower(h.HTTPContentType)
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		changes["http_content_type"] = "application/json"
	}
	if strings.TrimSpace(h.BodyTemplate) != "" {
		changes["body_template"] = ""
	}
	return changes
}

type reportFn func(string, ...any)

func applyWebhookEndpoint(nb checkAPI, hooks []*NBWebhook, settings *models.Settings, apply bool, fail, ok reportFn) ([]*NBWebhook, error) {
	matched := matchingWebhooks(hooks, settings.PublicBaseURL)
	if len(matched) == 0 {
		want := expectedWebhookURL(settings.PublicBaseURL)
		if want == "" {
			want = "…" + factumWebhookPath
		}
		if !apply {
			fail("no Netbox webhook has payload_url %s", want)
			return nil, nil
		}
		created, err := createOrRetargetWebhook(nb, hooks, settings)
		if err != nil {
			return nil, err
		}
		action := "created"
		for _, h := range hooks {
			if h.Name == factumWebhookName {
				action = "updated"
				break
			}
		}
		ok("%s webhook %q %s %s", action, created.Name, created.HTTPMethod, created.PayloadURL)
		return []*NBWebhook{created}, nil
	}
	for i, h := range matched {
		if epErr := webhookEndpointOK(h); epErr != nil {
			if !apply {
				fail("webhook %q: %s", h.Name, epErr)
				continue
			}
			updated, err := nb.UpdateWebhook(h.NetboxID, webhookEndpointChanges(h))
			if err != nil {
				return matched, err
			}
			matched[i] = updated
			ok("updated webhook %q (%s)", updated.Name, epErr)
			continue
		}
		ok("webhook %q %s %s", h.Name, h.HTTPMethod, h.PayloadURL)
	}
	return matched, nil
}

func createOrRetargetWebhook(nb checkAPI, hooks []*NBWebhook, settings *models.Settings) (*NBWebhook, error) {
	want := expectedWebhookURL(settings.PublicBaseURL)
	if want == "" {
		return nil, fmt.Errorf("PublicBaseURL is not set; cannot create webhook")
	}
	if settings.NetboxWebhookSecret == "" {
		return nil, fmt.Errorf("webhook secret is not set; cannot create webhook")
	}
	ssl := strings.HasPrefix(strings.ToLower(want), "https://")
	var named *NBWebhook
	for _, h := range hooks {
		if h.Name == factumWebhookName {
			named = h
			break
		}
	}
	if named != nil {
		return nb.UpdateWebhook(named.NetboxID, map[string]any{
			"payload_url":       want,
			"http_method":       "POST",
			"http_content_type": "application/json",
			"body_template":     "",
			"secret":            settings.NetboxWebhookSecret,
			"ssl_verification":  ssl,
		})
	}
	return nb.CreateWebhook(WebhookWrite{
		Name:            factumWebhookName,
		PayloadURL:      want,
		HTTPMethod:      "POST",
		HTTPContentType: "application/json",
		Secret:          settings.NetboxWebhookSecret,
		SSLVerification: ssl,
	})
}

func applyWebhookEvents(nb checkAPI, matched []*NBWebhook, rules []*NBEventRule, apply bool, fail, ok, warn reportFn) error {
	if len(matched) == 0 {
		if apply {
			return nil
		}
		for _, objectType := range requiredWebhookObjectTypes {
			fail("%s: no enabled webhook event rule", objectType)
		}
		return nil
	}
	coverage, notes := webhookEventCoverage(matched, rules)
	for _, note := range notes {
		warn("%s", note)
	}
	var missingTypes []string
	for _, objectType := range requiredWebhookObjectTypes {
		missing := missingEvents(coverage[objectType], requiredWebhookEvents)
		if len(missing) == 0 {
			ok("%s: %s", objectType, strings.Join(requiredWebhookEvents, ", "))
			continue
		}
		missingTypes = append(missingTypes, objectType)
		if apply {
			continue
		}
		if len(coverage[objectType]) == 0 {
			fail("%s: no enabled webhook event rule", objectType)
			continue
		}
		fail("%s: missing %s", objectType, strings.Join(missing, ", "))
	}
	if len(missingTypes) == 0 || !apply {
		return nil
	}
	hook := matched[0]
	rule := pickEventRule(rules, hook.NetboxID)
	if rule == nil {
		created, err := nb.CreateEventRule(EventRuleWrite{
			Name:           factumWebhookName,
			Enabled:        true,
			ObjectTypes:    append([]string{}, requiredWebhookObjectTypes...),
			EventTypes:     append([]string{}, requiredWebhookEvents...),
			ActionType:     "webhook",
			ActionObjectID: hook.NetboxID,
		})
		if err != nil {
			return err
		}
		ok("created event rule %q covering %s", created.Name, strings.Join(requiredWebhookObjectTypes, ", "))
		return nil
	}
	changes := map[string]any{
		"object_types": unionStrings(rule.ObjectTypes, requiredWebhookObjectTypes),
		"event_types":  unionStrings(rule.EventTypes, requiredWebhookEvents),
		"enabled":      true,
	}
	updated, err := nb.UpdateEventRule(rule.NetboxID, changes)
	if err != nil {
		return err
	}
	ok("updated event rule %q (%s)", updated.Name, strings.Join(missingTypes, ", "))
	return nil
}

func pickEventRule(rules []*NBEventRule, hookID uint) *NBEventRule {
	var named, enabled *NBEventRule
	for _, r := range rules {
		if r.ActionType != "webhook" || r.ActionObjectID != hookID {
			continue
		}
		if r.Name == factumWebhookName {
			named = r
		}
		if r.Enabled && enabled == nil {
			enabled = r
		}
	}
	if named != nil {
		return named
	}
	return enabled
}

func unionStrings(have, want []string) []string {
	seen := make(map[string]bool, len(have)+len(want))
	var out []string
	for _, s := range have {
		if seen[s] {
			continue
		}
		out = append(out, s)
		seen[s] = true
	}
	for _, s := range want {
		if seen[s] {
			continue
		}
		out = append(out, s)
		seen[s] = true
	}
	return out
}

// webhookEventCoverage unions object_type → event_types from enabled
// webhook-action rules that point at one of matched. Notes are warnings
// about rules that look relevant but are restricted (conditions).
func webhookEventCoverage(matched []*NBWebhook, rules []*NBEventRule) (map[string]map[string]bool, []string) {
	ids := make(map[uint]string, len(matched))
	for _, h := range matched {
		ids[h.NetboxID] = h.Name
	}
	coverage := make(map[string]map[string]bool)
	var notes []string
	for _, rule := range rules {
		if !rule.Enabled || rule.ActionType != "webhook" {
			continue
		}
		if _, ok := ids[rule.ActionObjectID]; !ok {
			continue
		}
		if rule.HasConditions() {
			notes = append(notes, fmt.Sprintf("event rule %q has conditions; some events may not fire", rule.Name))
		}
		for _, ot := range rule.ObjectTypes {
			if coverage[ot] == nil {
				coverage[ot] = make(map[string]bool)
			}
			for _, ev := range rule.EventTypes {
				coverage[ot][ev] = true
			}
		}
	}
	return coverage, notes
}

func missingEvents(have map[string]bool, want []string) []string {
	var missing []string
	for _, ev := range want {
		if !have[ev] {
			missing = append(missing, ev)
		}
	}
	return missing
}

type skippedCF struct {
	name   string
	reason string
}

func skippedCustomFields(s *models.Settings) []skippedCF {
	var out []skippedCF
	if !settingOn(s.BecsEnabled) {
		out = append(out, skippedCF{name: "becs_oid", reason: "BECS source is disabled"})
	}
	if !settingOn(s.LibrenmsEnabled) {
		out = append(out, skippedCF{name: "librenms_id", reason: "LibreNMS destination is disabled"})
	}
	if !settingOn(s.OpticalEnabled) {
		out = append(out, skippedCF{name: "optical_role", reason: "optical is disabled"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}
