package util

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/abundo/factum2/models"
)

// CommonConfig holds settings shared by every remote-config-fetching CLI
// tool, not just one of them: DefaultDomain (Settings.DefaultDomain, edited
// in the admin UI's Factum tab) is needed by DNS (the $ORIGIN of the
// generated zone file) as well as Icinga/LibreNMS/Oxidized sync (matching
// factum device names against fully-qualified DNS names).
//
// Every service's REST response and runtime Config type embeds this instead
// of each declaring its own copy of the fields. Tools that don't need any
// other service-specific config (currently just factum-worker agents) can
// fetch this alone from web.ApiCommonConfig (GET /api/common-config).
type CommonConfig struct {
	DefaultDomain string `json:"default_domain"`

	// SmtpHost/Port/User/Pass/TLSMode and EmailSender are the shared
	// outbound mail relay settings (Settings.Smtp*/EmailSender, edited on
	// the admin UI's "Email" destination tab) - factum-icinga-notifications
	// is the first consumer, but any tool needing to send mail can reuse
	// these instead of having its own copy.
	SmtpHost    string `json:"smtp_host"`
	SmtpPort    uint16 `json:"smtp_port"`
	SmtpUser    string `json:"smtp_user"`
	SmtpPass    string `json:"smtp_pass"`
	SmtpTLSMode string `json:"smtp_tls_mode"`
	EmailSender string `json:"email_sender"`
}

// NewCommonConfig builds a CommonConfig from the Settings row. Every
// web.ApiXxxConfig handler uses this instead of repeating the same lines.
func NewCommonConfig(settings *models.Settings) CommonConfig {
	return CommonConfig{
		DefaultDomain: settings.DefaultDomain,

		SmtpHost:    settings.SmtpHost,
		SmtpPort:    settings.SmtpPort,
		SmtpUser:    settings.SmtpUser,
		SmtpPass:    settings.SmtpPass,
		SmtpTLSMode: settings.SmtpTLSMode,
		EmailSender: settings.EmailSender,
	}
}

// FetchRemoteConfig performs an authenticated GET against the primary
// (factumConfig.URL+path, "Authorization: Bearer "+factumConfig.Token - see
// web.Controller.checkServiceToken) and unmarshals the JSON response into a
// new T. Shared by CLI tools that typically run on a different host than the
// primary (factum-icinga, factum-oxidized, factum-dns,
// factum-librenms-cli) to fetch their database-backed settings instead of
// needing a local copy - each package wraps this in its own
// FetchRemoteConfig returning its own config type, see e.g.
// internal/librenms/remote_config.go.
func FetchRemoteConfig[T any](factumConfig *ConfigFactum, path string) (*T, error) {
	req, err := http.NewRequest(http.MethodGet, factumConfig.URL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+factumConfig.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch config from %s%s: %s", factumConfig.URL, path, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
