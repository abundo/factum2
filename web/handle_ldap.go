package web

import (
	"net/http"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// LdapTestRequest carries the (possibly-unsaved, currently-in-form) LDAP
// connection fields for the admin "Test Connection" button. BindPassword is
// optional: if blank, the currently-saved Settings.LdapBindPassword is used
// instead, so the admin doesn't have to retype a secret just to test other
// field changes - same "omit to keep existing value" pattern as
// WorkerNodeDTO.Token/UserDTO.Password.
type LdapTestRequest struct {
	Host          string `json:"ldap_host"`
	Port          uint16 `json:"ldap_port"`
	TLSMode       string `json:"ldap_tls_mode"`
	SkipTLSVerify bool   `json:"ldap_skip_tls_verify"`
	BindDN        string `json:"ldap_bind_dn"`
	BindPassword  string `json:"ldap_bind_password,omitempty"`
	BaseDN        string `json:"ldap_base_dn"`
}

// ApiLdapTestConnection dials+binds with the submitted (or saved) settings
// and reports ok/error as a 200 JSON body rather than a 4xx/5xx status - a
// failed directory bind isn't an application error, it's the answer to the
// question the admin is asking.
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
		TLSMode:       req.TLSMode,
		SkipTLSVerify: req.SkipTLSVerify,
		BindDN:        req.BindDN,
		BindPassword:  req.BindPassword,
		BaseDN:        req.BaseDN,
	}
	if cfg.BindPassword == "" {
		cfg.BindPassword = settings.LdapBindPassword
	}

	if err := ldapauth.TestConnection(cfg); err != nil {
		return c.JSON(http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
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
