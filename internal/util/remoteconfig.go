package util

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/abundo/factum2/models"
)

const (
	factumUnixBaseURL = "http://factum2-worker"
	// unixProbeTimeout bounds Stat/Dial of the hub socket. HubRPCTimeout is
	// 60s and would stall fallback when the inode is stale.
	unixProbeTimeout = 3 * time.Second
)

// osStat is os.Stat, overridable in tests to simulate EACCES/EPERM.
var osStat = os.Stat

// CommonConfig holds settings shared by every remote-config-fetching CLI
// tool, not just one of them: DefaultDomain (Settings.DefaultDomain, edited
// in the admin UI's Factum tab) is needed by DNS (the $ORIGIN of the
// generated zone file) as well as Icinga/LibreNMS/Oxidized/Prometheus sync (matching
// factum device names against fully-qualified DNS names).
//
// Every service's REST response and runtime Config type embeds this instead
// of each declaring its own copy of the fields. Tools that don't need any
// other service-specific config (currently just factum2-worker agents) can
// fetch this alone from web.ApiCommonConfig (GET /api/common-config).
type CommonConfig struct {
	DefaultDomain string `json:"default_domain"`

	// SmtpHost/Port/User/Pass/TLSMode and EmailSender are the shared
	// outbound mail relay settings (Settings.Smtp*/EmailSender, edited on
	// the admin UI's "Email" destination tab) - factum2-icinga-notifications
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

// FactumHTTP picks a client for Factum API calls. The local worker unix
// socket is used when it is present and dialable; otherwise HTTPS to
// cfg.URL. After a successful unix Dial, RPC 502s/timeouts are not retried
// over HTTPS - worker-net :443 may already be closed. Unix requests send no
// Authorization header (the socket ACL is the auth); HTTPS keeps the bearer.
func FactumHTTP(cfg *ConfigFactum) (client *http.Client, baseURL string, viaSocket bool, err error) {
	socket := HubSocketPath(cfg.Socket)
	if socket == "" {
		return httpsFactumClient(cfg, "hub socket disabled and factum.url is not set")
	}

	_, statErr := osStat(socket)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return httpsFactumClient(cfg, fmt.Sprintf("hub socket %s does not exist and factum.url is not set", socket))
		}
		if os.IsPermission(statErr) {
			return httpsFactumClient(cfg, fmt.Sprintf("hub socket %s is not readable and factum.url is not set", socket))
		}
		if cfg.URL != "" {
			return httpsFactumClient(cfg, "")
		}
		return nil, "", false, fmt.Errorf("stat hub socket %s: %w", socket, statErr)
	}

	dialer := net.Dialer{Timeout: unixProbeTimeout}
	ctx, cancel := context.WithTimeout(context.Background(), unixProbeTimeout)
	defer cancel()
	conn, dialErr := dialer.DialContext(ctx, "unix", socket)
	if dialErr != nil {
		if cfg.URL != "" {
			return httpsFactumClient(cfg, "")
		}
		return nil, "", false, fmt.Errorf("dial hub socket %s: %w", socket, dialErr)
	}
	conn.Close()

	return unixFactumClient(socket), factumUnixBaseURL, true, nil
}

func httpsFactumClient(cfg *ConfigFactum, emptyURLErr string) (*http.Client, string, bool, error) {
	if cfg.URL == "" {
		if emptyURLErr == "" {
			emptyURLErr = "hub socket not usable and factum.url is not set"
		}
		return nil, "", false, fmt.Errorf("%s", emptyURLErr)
	}
	return &http.Client{Timeout: HubRPCTimeout}, cfg.URL, false, nil
}

func unixFactumClient(socket string) *http.Client {
	return &http.Client{
		Timeout: HubRPCTimeout,
		// Fresh Transport: do not clone DefaultTransport, which would inherit
		// ProxyFromEnvironment and let HTTP_PROXY steal the unix dial.
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
}

// FetchRemoteConfig GETs path from the Factum API and unmarshals the JSON
// into a new T. Transport is selected by FactumHTTP (unix socket when the
// probe succeeds, otherwise HTTPS to factumConfig.URL with
// "Authorization: Bearer "+factumConfig.Token - see
// web.Controller.checkServiceToken). A unix 502 is returned as an error; it
// is not retried over HTTPS. Shared by CLI tools that typically run on a
// different host than the primary (factum2-icinga, factum2-oxidized,
// factum2-dns, factum2-librenms-cli, factum2-prometheus) to fetch their
// database-backed settings
// instead of needing a local copy - each package wraps this in its own
// FetchRemoteConfig returning its own config type, see e.g.
// internal/librenms/remote_config.go.
func FetchRemoteConfig[T any](factumConfig *ConfigFactum, path string) (*T, error) {
	client, baseURL, viaSocket, err := FactumHTTP(factumConfig)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if !viaSocket {
		req.Header.Set("Authorization", "Bearer "+factumConfig.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch config from %s%s: %s", baseURL, path, resp.Status)
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
