package web

import (
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/mail"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// --------------------------------------------------------------------------
//
// # Admin settings
//
// Settings is stored as a single row (id=1); util.GetOrCreateSettings gives
// the frontend a row to edit even before anyone has saved once. It's shared
// with non-web callers (e.g. internal/netbox) that need the same
// database-backed integration credentials this page edits.
//
// --------------------------------------------------------------------------

func (ctrl *Controller) ApiSettingsGet(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, settings)
}

func (ctrl *Controller) ApiSettingsUpdate(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	id := settings.ID
	if err := c.Bind(settings); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	settings.ID = id

	if err := ctrl.DB.Save(settings).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, settings)
}

// SettingsTestEmailRequest carries the (possibly-unsaved, currently-in-form)
// SMTP fields plus the recipient address(es) for the admin "Send test email"
// button. To is free-form textarea content, one address (or comma-separated
// addresses) per line - same "one item per line" convention as the
// Ignore*/Roles* textareas elsewhere on this page. SmtpPass is optional: if
// blank, the currently-saved Settings.SmtpPass is used instead, same "omit
// to keep existing value" pattern as LdapTestRequest.BindPassword.
type SettingsTestEmailRequest struct {
	To          string `json:"to"`
	SmtpHost    string `json:"smtp_host"`
	SmtpPort    uint16 `json:"smtp_port"`
	SmtpUser    string `json:"smtp_user"`
	SmtpPass    string `json:"smtp_pass,omitempty"`
	SmtpTLSMode string `json:"smtp_tls_mode"`
	EmailSender string `json:"email_sender"`
}

// parseEmailRecipients splits textarea content into individual addresses,
// one per line and/or comma-separated within a line.
func parseEmailRecipients(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		for _, addr := range strings.Split(line, ",") {
			if addr = strings.TrimSpace(addr); addr != "" {
				out = append(out, addr)
			}
		}
	}
	return out
}

// ApiSettingsTestEmail sends a test email using the submitted (or saved) SMTP
// settings and reports ok/error as a 200 JSON body rather than a 4xx/5xx
// status - a failed send isn't an application error, it's the answer to the
// question the admin is asking. Same shape as ApiLdapTestConnection.
func (ctrl *Controller) ApiSettingsTestEmail(c *echo.Context) error {
	var req SettingsTestEmailRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	recipients := parseEmailRecipients(req.To)
	if len(recipients) == 0 {
		return c.JSON(http.StatusOK, map[string]any{"ok": false, "error": "at least one recipient address is required"})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	cfg := util.CommonConfig{
		SmtpHost:    req.SmtpHost,
		SmtpPort:    req.SmtpPort,
		SmtpUser:    req.SmtpUser,
		SmtpPass:    req.SmtpPass,
		SmtpTLSMode: req.SmtpTLSMode,
		EmailSender: req.EmailSender,
	}
	if cfg.SmtpPass == "" {
		cfg.SmtpPass = settings.SmtpPass
	}

	for _, to := range recipients {
		if err := mail.Send(cfg, cfg.EmailSender, to, "Factum test email", "This is a test email sent from Factum's admin settings page."); err != nil {
			return c.JSON(http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// --------------------------------------------------------------------------
//
// # API
//
// --------------------------------------------------------------------------

// Fetch all users
func (ctrl *Controller) ApiListUsers(c *echo.Context) error {
	var response []models.UserDTO
	response = []models.UserDTO{}
	// data := ctrl.GetUser(c)

	var items []models.User
	ctrl.DB.Find(&items)
	for _, item := range items {
		var c models.UserDTO
		c.ID = item.ID
		c.Username = item.Username
		c.Name = item.Name
		c.Mobile = item.Mobile
		response = append(response, c)
	}
	return c.JSON(http.StatusOK, response)
}

// Fetch all roles
func (ctrl *Controller) ApiListRoles(c *echo.Context) error {
	var response []models.RoleDTO
	response = []models.RoleDTO{}
	// data := ctrl.GetUser(c)

	var items []models.Role
	ctrl.DB.Find(&items)
	for _, item := range items {
		var c models.RoleDTO
		c.ID = item.ID
		c.Name = item.Name
		response = append(response, c)
	}
	return c.JSON(http.StatusOK, response)
}
