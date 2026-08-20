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

// checkAPI is the Netbox surface Check needs — a subset of NetboxClient so
// tests can stub list/lookup without a live Netbox.
type checkAPI interface {
	GetWebhooks() ([]*netboxtool.NBWebhook, error)
	GetEventRules() ([]*netboxtool.NBEventRule, error)
	GetCustomField(name string) (*netboxtool.NBCustomField, error)
	EnsureCustomFieldChoiceSet(name string, choices [][2]string) (*netboxtool.NBChoiceSet, error)
	CreateCustomField(w netboxtool.CustomFieldWrite) (*netboxtool.NBCustomField, error)
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
// Update=true creates/updates custom fields that drift from the catalogue.
type CheckOptions struct {
	Update bool
}

// Check verifies Netbox webhooks/event rules and the custom fields factum
// needs. Without Update it only reports. With Update it creates missing
// fields and patches mutable attributes (type is never changed).
func Check(c *util.ConfigRoot, opts CheckOptions, reporter jobevent.Reporter) error {
	db, err := util.ConnectMigrate(&c.DB)
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
	return CheckDB(nb, settings, opts, reporter)
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

	matched := matchingWebhooks(hooks, settings.PublicBaseURL)
	if settings.PublicBaseURL == "" {
		warn("PublicBaseURL is not set; matching webhooks by path %s only", factumWebhookPath)
	}
	if len(matched) == 0 {
		want := expectedWebhookURL(settings.PublicBaseURL)
		if want == "" {
			want = "…" + factumWebhookPath
		}
		fail("no Netbox webhook has payload_url %s", want)
	} else {
		for _, h := range matched {
			if err := webhookEndpointOK(h); err != nil {
				fail("webhook %q: %s", h.Name, err)
				continue
			}
			ok("webhook %q %s %s", h.Name, h.HTTPMethod, h.PayloadURL)
		}
	}

	coverage, ruleNotes := webhookEventCoverage(matched, rules)
	for _, note := range ruleNotes {
		warn("%s", note)
	}
	for _, objectType := range requiredWebhookObjectTypes {
		missing := missingEvents(coverage[objectType], requiredWebhookEvents)
		if len(missing) == 0 {
			ok("%s: %s", objectType, strings.Join(requiredWebhookEvents, ", "))
			continue
		}
		if len(coverage[objectType]) == 0 {
			fail("%s: no enabled webhook event rule", objectType)
			continue
		}
		fail("%s: missing %s", objectType, strings.Join(missing, ", "))
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

func matchingWebhooks(hooks []*netboxtool.NBWebhook, publicBase string) []*netboxtool.NBWebhook {
	var out []*netboxtool.NBWebhook
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

func webhookEndpointOK(h *netboxtool.NBWebhook) error {
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

// webhookEventCoverage unions object_type → event_types from enabled
// webhook-action rules that point at one of matched. Notes are warnings
// about rules that look relevant but are restricted (conditions).
func webhookEventCoverage(matched []*netboxtool.NBWebhook, rules []*netboxtool.NBEventRule) (map[string]map[string]bool, []string) {
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
