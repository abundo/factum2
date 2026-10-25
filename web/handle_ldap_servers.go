package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// LdapServerDTO is one external directory server as the Directory page
// edits it. BindPassword is accepted on write and omitted on read.
type LdapServerDTO struct {
	ID                  uint   `json:"id"`
	Position            int    `json:"position"`
	Enabled             bool   `json:"enabled"`
	Name                string `json:"name"`
	ServerType          string `json:"server_type"`
	Host                string `json:"host"`
	Port                uint16 `json:"port"`
	TLSMode             string `json:"tls_mode"`
	SkipTLSVerify       bool   `json:"skip_tls_verify"`
	BindDN              string `json:"bind_dn"`
	BindPassword        string `json:"bind_password,omitempty"`
	BindPasswordSet     bool   `json:"bind_password_set"`
	BaseDN              string `json:"base_dn"`
	UserFilter          string `json:"user_filter"`
	AttrUsername        string `json:"attr_username"`
	AttrEmail           string `json:"attr_email"`
	AttrDisplayName     string `json:"attr_display_name"`
	AttrMobile          string `json:"attr_mobile"`
	AttrGroups          string `json:"attr_groups"`
	AllowPasswordChange bool   `json:"allow_password_change"`
	Writable            bool   `json:"writable"`
}

func ldapServerToDTO(row *models.LdapServer, wb ldapauth.WritebackConfig) LdapServerDTO {
	enabled := serverEnabled(row)
	code, _ := directoryWriteBlock(row.ServerType, enabled, wb)
	port := row.Port
	if port == 0 {
		port = 389
	}
	return LdapServerDTO{
		ID:                  row.ID,
		Position:            row.Position,
		Enabled:             enabled,
		Name:                row.Name,
		ServerType:          ldapServerTypeName(row.ServerType),
		Host:                row.Host,
		Port:                port,
		TLSMode:             ldapServerTypeNameTLS(row.TLSMode),
		SkipTLSVerify:       row.SkipTLSVerify != nil && *row.SkipTLSVerify,
		BindDN:              row.BindDN,
		BindPasswordSet:     row.BindPassword != "",
		BaseDN:              row.BaseDN,
		UserFilter:          row.UserFilter,
		AttrUsername:        row.AttrUsername,
		AttrEmail:           row.AttrEmail,
		AttrDisplayName:     row.AttrDisplayName,
		AttrMobile:          row.AttrMobile,
		AttrGroups:          row.AttrGroups,
		AllowPasswordChange: row.AllowPasswordChange != nil && *row.AllowPasswordChange,
		Writable:            code == 0,
	}
}

func ldapServerTypeNameTLS(mode string) string {
	switch strings.TrimSpace(mode) {
	case "starttls", "ldaps":
		return mode
	default:
		return "none"
	}
}

func applyLdapServerDTO(row *models.LdapServer, req LdapServerDTO, creating bool) error {
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return errLDAPHostRequired
	}
	typ := strings.TrimSpace(req.ServerType)
	if typ == "" {
		typ = "generic"
	}
	if typ != "ad" && typ != "generic" {
		return errLDAPServerType
	}
	mode := ldapServerTypeNameTLS(req.TLSMode)
	row.Enabled = ldapBool(req.Enabled)
	row.Name = strings.TrimSpace(req.Name)
	row.ServerType = typ
	row.Host = host
	row.Port = req.Port
	if row.Port == 0 {
		row.Port = 389
	}
	row.TLSMode = mode
	row.SkipTLSVerify = ldapBool(req.SkipTLSVerify)
	row.BindDN = strings.TrimSpace(req.BindDN)
	if req.BindPassword != "" {
		row.BindPassword = req.BindPassword
	} else if creating || row.BindDN == "" {
		row.BindPassword = ""
	}
	row.BaseDN = strings.TrimSpace(req.BaseDN)
	row.UserFilter = strings.TrimSpace(req.UserFilter)
	row.AttrUsername = strings.TrimSpace(req.AttrUsername)
	row.AttrEmail = strings.TrimSpace(req.AttrEmail)
	row.AttrDisplayName = strings.TrimSpace(req.AttrDisplayName)
	row.AttrMobile = strings.TrimSpace(req.AttrMobile)
	row.AttrGroups = strings.TrimSpace(req.AttrGroups)
	row.AllowPasswordChange = ldapBool(req.AllowPasswordChange)
	if req.Position > 0 {
		row.Position = req.Position
	}
	return nil
}

var (
	errLDAPHostRequired = echoStatusError{http.StatusBadRequest, "host is required"}
	errLDAPServerType   = echoStatusError{http.StatusBadRequest, "server type must be ad or generic"}
)

type echoStatusError struct {
	code int
	msg  string
}

func (e echoStatusError) Error() string { return e.msg }

func (ctrl *Controller) ApiLdapServers(c *echo.Context) error {
	rows, _, err := ldapServerRows(ctrl.DB, false)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	wb := ldapWritebackConfig()
	out := make([]LdapServerDTO, 0, len(rows))
	for i := range rows {
		out = append(out, ldapServerToDTO(&rows[i], wb))
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiLdapServerCreate(c *echo.Context) error {
	var req LdapServerDTO
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row := models.LdapServer{}
	if req.Position <= 0 {
		var maxPos *int
		_ = ctrl.DB.Model(&models.LdapServer{}).Select("MAX(position)").Scan(&maxPos).Error
		if maxPos == nil {
			req.Position = 1
		} else {
			req.Position = *maxPos + 1
		}
	}
	if err := applyLdapServerDTO(&row, req, true); err != nil {
		return writeLDAPServerError(c, err)
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, ldapServerToDTO(&row, ldapWritebackConfig()))
}

func (ctrl *Controller) ApiLdapServerUpdate(c *echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var req LdapServerDTO
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var row models.LdapServer
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "LDAP server not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := applyLdapServerDTO(&row, req, false); err != nil {
		return writeLDAPServerError(c, err)
	}
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, ldapServerToDTO(&row, ldapWritebackConfig()))
}

func (ctrl *Controller) ApiLdapServerDelete(c *echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	res := ctrl.DB.Delete(&models.LdapServer{}, id)
	if res.Error != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "LDAP server not found"})
	}
	return c.JSON(http.StatusOK, map[string]any{"id": id})
}

func ldapBool(v bool) *bool { return &v }

// ApiLdapServersReorder sets login order from the directory tree. ids is
// every external server, first to last. The local directory is not a row
// here, so it stays ahead of this list.
func (ctrl *Controller) ApiLdapServersReorder(c *echo.Context) error {
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var rows []models.LdapServer
	if err := ctrl.DB.Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if len(req.IDs) != len(rows) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "list every LDAP server"})
	}
	known := make(map[uint]bool, len(rows))
	for i := range rows {
		known[rows[i].ID] = true
	}
	seen := make(map[uint]bool, len(req.IDs))
	for _, id := range req.IDs {
		if id == 0 || !known[id] || seen[id] {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "list every LDAP server"})
		}
		seen[id] = true
	}
	err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		for i, id := range req.IDs {
			if err := tx.Model(&models.LdapServer{}).Where("id = ?", id).Update("position", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func writeLDAPServerError(c *echo.Context, err error) error {
	if se, ok := err.(echoStatusError); ok {
		return c.JSON(se.code, map[string]any{"error": se.msg})
	}
	return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
}
