package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// LdapDirectoryStatusDTO tells the directory page whether to show the
// server tree and whether OpenLDAP writes are available. The write-back
// password is never included.
type LdapDirectoryStatusDTO struct {
	Enabled    bool   `json:"enabled"`
	ServerType string `json:"server_type"`
	BaseDN     string `json:"base_dn"`
	Writable   bool   `json:"writable"`
}

type ldapPersonRequest struct {
	DN          string `json:"dn"`
	ParentDN    string `json:"parent_dn"`
	UID         string `json:"uid"`
	CN          string `json:"cn"`
	SN          string `json:"sn"`
	Mail        string `json:"mail"`
	DisplayName string `json:"display_name"`
	Mobile      string `json:"mobile"`
	Password    string `json:"password"`
}

type ldapGroupRequest struct {
	DN          string   `json:"dn"`
	ParentDN    string   `json:"parent_dn"`
	CN          string   `json:"cn"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
}

type ldapEntryDTO struct {
	DN          string   `json:"dn"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	UID         string   `json:"uid,omitempty"`
	CN          string   `json:"cn,omitempty"`
	SN          string   `json:"sn,omitempty"`
	Mail        string   `json:"mail,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Mobile      string   `json:"mobile,omitempty"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
	MemberAttr  string   `json:"member_attr,omitempty"`
	Editable    bool     `json:"editable"`
}

func ldapWritebackConfig() ldapauth.WritebackConfig {
	if util.Config == nil {
		return ldapauth.WritebackConfig{}
	}
	return ldapauth.WritebackConfig{
		BindDN:   util.Config.LdapWriteback.BindDN,
		Password: util.Config.LdapWriteback.Password,
	}
}

func ldapServerType(settings *models.Settings) string {
	if settings == nil || strings.TrimSpace(settings.LdapServerType) == "" {
		return "generic"
	}
	return settings.LdapServerType
}

func ldapEnabled(settings *models.Settings) bool {
	return settings != nil && settings.LdapEnabled != nil && *settings.LdapEnabled
}

// directoryWriteBlock is the reason a directory write must be refused,
// or 0 when the OpenLDAP write-back account may be used. Reads are allowed
// for both server types once LDAP is enabled.
func directoryWriteBlock(settings *models.Settings, wb ldapauth.WritebackConfig) (int, string) {
	if !ldapEnabled(settings) {
		return http.StatusBadRequest, "LDAP is not enabled"
	}
	if strings.EqualFold(ldapServerType(settings), "ad") {
		return http.StatusForbidden, ldapauth.ErrADReadOnly.Error()
	}
	if strings.TrimSpace(wb.BindDN) == "" || wb.Password == "" {
		return http.StatusBadRequest, ldapauth.ErrWritebackMissing.Error()
	}
	return 0, ""
}

func directoryStatus(settings *models.Settings, wb ldapauth.WritebackConfig) LdapDirectoryStatusDTO {
	status := LdapDirectoryStatusDTO{
		Enabled:    ldapEnabled(settings),
		ServerType: ldapServerType(settings),
		BaseDN:     settings.LdapBaseDN,
	}
	code, _ := directoryWriteBlock(settings, wb)
	status.Writable = code == 0
	return status
}

func (ctrl *Controller) ApiLdapDirectoryStatus(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, directoryStatus(settings, ldapWritebackConfig()))
}

func (ctrl *Controller) loadDirectoryRead(c *echo.Context) (*models.Settings, ldapauth.Config, bool) {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		_ = c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return nil, ldapauth.Config{}, false
	}
	if !ldapEnabled(settings) {
		_ = c.JSON(http.StatusBadRequest, map[string]any{"error": "LDAP is not enabled"})
		return nil, ldapauth.Config{}, false
	}
	return settings, ldapauth.ConfigFromSettings(settings), true
}

func (ctrl *Controller) loadDirectoryWrite(c *echo.Context) (ldapauth.Config, ldapauth.WritebackConfig, bool) {
	settings, cfg, ok := ctrl.loadDirectoryRead(c)
	if !ok {
		return ldapauth.Config{}, ldapauth.WritebackConfig{}, false
	}
	wb := ldapWritebackConfig()
	if code, msg := directoryWriteBlock(settings, wb); code != 0 {
		_ = c.JSON(code, map[string]any{"error": msg})
		return ldapauth.Config{}, ldapauth.WritebackConfig{}, false
	}
	return cfg, wb, true
}

func ldapDirectoryError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, ldapauth.ErrADReadOnly):
		return c.JSON(http.StatusForbidden, map[string]any{"error": err.Error()})
	case errors.Is(err, ldapauth.ErrWritebackMissing):
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	default:
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
}

func toLDAPEntryDTO(rec *ldapauth.Record, writable bool) ldapEntryDTO {
	editable := writable && (rec.Kind == "person" || (rec.Kind == "group" && rec.MemberAttr != ""))
	members := rec.Members
	if members == nil {
		members = []string{}
	}
	return ldapEntryDTO{
		DN:          rec.DN,
		Name:        rec.RDN,
		Kind:        rec.Kind,
		UID:         rec.UID,
		CN:          rec.CN,
		SN:          rec.SN,
		Mail:        rec.Mail,
		DisplayName: rec.DisplayName,
		Mobile:      rec.Mobile,
		Description: rec.Description,
		Members:     members,
		MemberAttr:  rec.MemberAttr,
		Editable:    editable,
	}
}

func (ctrl *Controller) ApiLdapEntry(c *echo.Context) error {
	settings, cfg, ok := ctrl.loadDirectoryRead(c)
	if !ok {
		return nil
	}
	rec, err := ldapauth.ReadEntry(cfg, c.QueryParam("dn"))
	if err != nil {
		return ldapDirectoryError(c, err)
	}
	_, msg := directoryWriteBlock(settings, ldapWritebackConfig())
	return c.JSON(http.StatusOK, toLDAPEntryDTO(rec, msg == ""))
}

func personInput(req ldapPersonRequest) ldapauth.PersonInput {
	return ldapauth.PersonInput{
		DN:          req.DN,
		ParentDN:    req.ParentDN,
		UID:         req.UID,
		CN:          req.CN,
		SN:          req.SN,
		Mail:        req.Mail,
		DisplayName: req.DisplayName,
		Mobile:      req.Mobile,
		Password:    req.Password,
	}
}

func (ctrl *Controller) ApiLdapPersonCreate(c *echo.Context) error {
	var req ldapPersonRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if strings.TrimSpace(req.Password) == "" || len(req.Password) < minPasswordLength {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("password must be at least %d characters", minPasswordLength)})
	}
	cfg, wb, ok := ctrl.loadDirectoryWrite(c)
	if !ok {
		return nil
	}
	dn, err := ldapauth.CreatePerson(cfg, wb, personInput(req))
	if err != nil {
		return ldapDirectoryError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"dn": dn})
}

func (ctrl *Controller) ApiLdapPersonUpdate(c *echo.Context) error {
	var req ldapPersonRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if req.Password != "" && len(req.Password) < minPasswordLength {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("password must be at least %d characters", minPasswordLength)})
	}
	cfg, wb, ok := ctrl.loadDirectoryWrite(c)
	if !ok {
		return nil
	}
	dn, err := ldapauth.UpdatePerson(cfg, wb, personInput(req))
	if err != nil {
		return ldapDirectoryError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"dn": dn})
}

func (ctrl *Controller) ApiLdapPersonDelete(c *echo.Context) error {
	dn := c.QueryParam("dn")
	cfg, wb, ok := ctrl.loadDirectoryWrite(c)
	if !ok {
		return nil
	}
	rec, err := ldapauth.ReadEntry(cfg, dn)
	if err != nil {
		return ldapDirectoryError(c, err)
	}
	if err := ldapauth.DeletePerson(cfg, wb, dn); err != nil {
		return ldapDirectoryError(c, err)
	}
	body := map[string]any{"dn": dn}
	if rec.UID != "" {
		var user models.User
		err := ctrl.DB.Where("username = ?", rec.UID).First(&user).Error
		if err == nil && user.PasswordHash == ldapSentinelPassword {
			body["local_user_id"] = user.ID
			body["local_username"] = user.Username
		}
	}
	return c.JSON(http.StatusOK, body)
}

func (ctrl *Controller) ApiLdapGroupCreate(c *echo.Context) error {
	var req ldapGroupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	cfg, wb, ok := ctrl.loadDirectoryWrite(c)
	if !ok {
		return nil
	}
	dn, err := ldapauth.CreateGroup(cfg, wb, ldapauth.GroupInput{
		ParentDN: req.ParentDN, CN: req.CN, Description: req.Description, Members: req.Members,
	})
	if err != nil {
		return ldapDirectoryError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"dn": dn})
}

func (ctrl *Controller) ApiLdapGroupUpdate(c *echo.Context) error {
	var req ldapGroupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	cfg, wb, ok := ctrl.loadDirectoryWrite(c)
	if !ok {
		return nil
	}
	oldDN := strings.TrimSpace(req.DN)
	dn, err := ldapauth.UpdateGroup(cfg, wb, ldapauth.GroupInput{
		DN: req.DN, CN: req.CN, Description: req.Description, Members: req.Members,
	})
	if err != nil {
		return ldapDirectoryError(c, err)
	}
	if !strings.EqualFold(oldDN, dn) {
		if err := rewriteGroupDNReferences(ctrl.DB, oldDN, dn); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"dn": dn})
}

func (ctrl *Controller) ApiLdapGroupDelete(c *echo.Context) error {
	dn := strings.TrimSpace(c.QueryParam("dn"))
	cfg, wb, ok := ctrl.loadDirectoryWrite(c)
	if !ok {
		return nil
	}
	if err := ldapauth.DeleteGroup(cfg, wb, dn); err != nil {
		return ldapDirectoryError(c, err)
	}
	if err := rewriteGroupDNReferences(ctrl.DB, dn, ""); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"dn": dn})
}

// rewriteGroupDNReferences points Factum rows that stored oldDN at newDN.
// An empty newDN deletes those rows. Role rows and RADIUS policies both
// keep a group DN; the directory group itself is the thing being renamed
// or removed. Factum roles are not created or deleted here.
func rewriteGroupDNReferences(db *gorm.DB, oldDN, newDN string) error {
	old := ldapauth.NormalizeDN(oldDN)
	if old == "" {
		return nil
	}
	var mappings []models.LdapRoleMapping
	if err := db.Find(&mappings).Error; err != nil {
		return err
	}
	for i := range mappings {
		if ldapauth.NormalizeDN(mappings[i].GroupDN) != old {
			continue
		}
		if newDN == "" {
			if err := db.Delete(&mappings[i]).Error; err != nil {
				return err
			}
			continue
		}
		if err := db.Model(&mappings[i]).Update("group_dn", newDN).Error; err != nil {
			return err
		}
	}
	var policies []models.RadiusPolicy
	if err := db.Find(&policies).Error; err != nil {
		return err
	}
	for i := range policies {
		if ldapauth.NormalizeDN(policies[i].GroupDN) != old {
			continue
		}
		if newDN == "" {
			if err := db.Delete(&policies[i]).Error; err != nil {
				return err
			}
			continue
		}
		if err := db.Model(&policies[i]).Update("group_dn", newDN).Error; err != nil {
			return err
		}
	}
	return nil
}
