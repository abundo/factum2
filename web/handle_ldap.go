package web

import (
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// LdapTestRequest carries the (possibly-unsaved, currently-in-form) LDAP
// connection fields for the admin "Test Connection" button. BindPassword is
// optional: if blank AND BindDN is set, the currently-saved
// Settings.LdapBindPassword is used instead, so the admin doesn't have to
// retype a secret just to test other field changes - same "omit to keep
// existing value" pattern as WorkerNodeDTO.Token/UserDTO.Password. An empty
// BindDN is an anonymous bind: do not fill in a leftover saved password.
type LdapTestRequest struct {
	Host          string `json:"ldap_host"`
	Port          uint16 `json:"ldap_port"`
	Host2         string `json:"ldap_host2"`
	Port2         uint16 `json:"ldap_port2"`
	TLSMode       string `json:"ldap_tls_mode"`
	SkipTLSVerify bool   `json:"ldap_skip_tls_verify"`
	BindDN        string `json:"ldap_bind_dn"`
	BindPassword  string `json:"ldap_bind_password,omitempty"`
	BaseDN        string `json:"ldap_base_dn"`
}

// LdapTestServerDTO is one host's result in ApiLdapTestConnection's
// response, so a dual-server setup can show which replica answered.
type LdapTestServerDTO struct {
	Host  string `json:"host"`
	Port  uint16 `json:"port"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ApiLdapTestConnection dials+binds with the submitted (or saved) settings
// and reports ok/error as a 200 JSON body rather than a 4xx/5xx status - a
// failed directory bind isn't an application error, it's the answer to the
// question the admin is asking. Each configured server is tested on its
// own (no failover) so a broken secondary isn't hidden by a working
// primary. `ok` is true only if every configured server succeeded.
func (ctrl *Controller) ApiLdapTestConnection(c *echo.Context) error {
	var req LdapTestRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	cfg := ldapauth.Config{
		Host:          req.Host,
		Port:          req.Port,
		Host2:         req.Host2,
		Port2:         req.Port2,
		TLSMode:       req.TLSMode,
		SkipTLSVerify: req.SkipTLSVerify,
		BindDN:        req.BindDN,
		BindPassword:  req.BindPassword,
		BaseDN:        req.BaseDN,
	}
	if cfg.BindPassword == "" && strings.TrimSpace(cfg.BindDN) != "" {
		cfg.BindPassword = settings.LdapBindPassword
	}

	results := ldapauth.TestServers(cfg)
	if len(results) == 0 {
		return c.JSON(http.StatusOK, map[string]any{
			"ok":      false,
			"error":   "ldap: no host configured",
			"servers": []LdapTestServerDTO{},
		})
	}

	dtos := make([]LdapTestServerDTO, 0, len(results))
	ok := true
	var firstErr string
	for _, r := range results {
		dto := LdapTestServerDTO{Host: r.Host, Port: r.Port, OK: r.Error == nil}
		if r.Error != nil {
			ok = false
			dto.Error = r.Error.Error()
			if firstErr == "" {
				firstErr = dto.Error
			}
		}
		dtos = append(dtos, dto)
	}
	body := map[string]any{"ok": ok, "servers": dtos}
	if !ok {
		body["error"] = firstErr
	}
	return c.JSON(http.StatusOK, body)
}

// LdapBrowseEntryDTO is one node in the tree-browser response.
type LdapBrowseEntryDTO struct {
	DN      string `json:"dn"`
	Name    string `json:"name"`
	IsGroup bool   `json:"is_group"`
}

// ApiLdapBrowse lists the immediate children of the ?dn= query param (or the
// configured LdapBaseDN if omitted), for the "browse the directory" picker on
// the LDAP/AD group -> role mapping dialog. Uses the saved Settings
// connection details (unlike ApiLdapTestConnection, which tests possibly-
// unsaved form values) since browsing only makes sense once LDAP is actually
// configured.
func (ctrl *Controller) ApiLdapBrowse(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	cfg := ldapauth.ConfigFromSettings(settings)
	entries, err := ldapauth.Browse(cfg, c.QueryParam("dn"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	dtos := make([]LdapBrowseEntryDTO, 0, len(entries))
	for _, e := range entries {
		dtos = append(dtos, LdapBrowseEntryDTO{DN: e.DN, Name: e.RDN, IsGroup: e.IsGroup})
	}
	return c.JSON(http.StatusOK, dtos)
}
