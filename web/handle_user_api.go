package web

import (
	"fmt"
	"net/http"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// --------------------------------------------------------------------------
//
//	API: admin user management
//
//	The generic SecureCRUDHandler can't be used for users: Create/Update
//	round-trip the request DTO through the DB model via JSON, which never
//	touches PasswordHash (it isn't part of UserDTO's JSON) - without custom
//	handling, users created through the admin API would have no usable
//	password. List/GetOne need custom handling too: Roles is a many2many
//	association (json:"-" on the model, so it never round-trips through the
//	generic handler either) that has to be preloaded and flattened into
//	UserDTO.RoleIDs by hand.
//
// --------------------------------------------------------------------------

// rolesByIDs resolves the Role rows to associate with a user from the IDs
// submitted in a UserDTO. An empty/nil slice is treated as "no roles" rather
// than skipped: gorm's Find with an empty ID slice matches every row, not
// zero, so that case is special-cased instead of just calling DB.Find.
func (ctrl *Controller) rolesByIDs(ids []uint) ([]*models.Role, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var roles []*models.Role
	if err := ctrl.DB.Find(&roles, ids).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

func (ctrl *Controller) ApiUserList(c *echo.Context) error {
	var users []models.User
	if err := ctrl.DB.Preload("Roles").Find(&users).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	dtos := make([]models.UserDTO, len(users))
	for i, u := range users {
		dtos[i] = toUserDTO(u)
	}
	return c.JSON(http.StatusOK, dtos)
}

func (ctrl *Controller) ApiUserGetOne(c *echo.Context) error {
	id := c.Param("id")
	var user models.User
	if err := ctrl.DB.Preload("Roles").First(&user, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	return c.JSON(http.StatusOK, toUserDTO(user))
}

func (ctrl *Controller) ApiUserCreate(c *echo.Context) error {
	var dto models.UserDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.Username == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "username is required"})
	}
	if dto.Password == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "password is required"})
	}

	hash, err := HashPassword(dto.Password)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to hash password"})
	}

	roles, err := ctrl.rolesByIDs(dto.RoleIDs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	user := models.User{
		Username:     dto.Username,
		PasswordHash: hash,
		Name:         dto.Name,
		Email:        dto.Email,
		Mobile:       dto.Mobile,
		Roles:        roles,
	}
	if err := ctrl.DB.Create(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toUserDTO(user))
}

func (ctrl *Controller) ApiUserUpdate(c *echo.Context) error {
	id := c.Param("id")
	var user models.User
	if err := ctrl.DB.First(&user, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	var dto models.UserDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	roles, err := ctrl.rolesByIDs(dto.RoleIDs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	user.Username = dto.Username
	user.Name = dto.Name
	user.Email = dto.Email
	user.Mobile = dto.Mobile

	if dto.Password != "" {
		hash, err := HashPassword(dto.Password)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to hash password"})
		}
		user.PasswordHash = hash
	}

	if err := ctrl.DB.Save(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Model(&user).Association("Roles").Replace(roles); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	user.Roles = roles

	return c.JSON(http.StatusOK, toUserDTO(user))
}

// ApiUserDelete refuses to delete a user holding the "admin" role, so the
// last admin account can never be removed through this endpoint (there's no
// other way to grant that role back once nobody holds it).
func (ctrl *Controller) ApiUserDelete(c *echo.Context) error {
	id := c.Param("id")
	var user models.User
	if err := ctrl.DB.Preload("Roles").First(&user, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if user.HasRole("admin") {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "the admin user cannot be deleted"})
	}
	if err := ctrl.DB.Delete(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// --------------------------------------------------------------------------
//
//	API: current user's own profile ("/api/me")
//
// --------------------------------------------------------------------------

func mePayload(db *gorm.DB, u models.User) map[string]any {
	dto := toUserDTO(u)
	return map[string]any{
		"id":              dto.ID,
		"username":        dto.Username,
		"name":            dto.Name,
		"email":           dto.Email,
		"mobile":          dto.Mobile,
		"roles":           dto.Roles,
		"role_ids":        dto.RoleIDs,
		"optical_enabled": optical.OpticalEnabled(db),
		"ipam_enabled":    ipam.Enabled(db),
	}
}

func toUserDTO(u models.User) models.UserDTO {
	roleIDs := make([]uint, len(u.Roles))
	roleNames := make([]string, len(u.Roles))
	for i, r := range u.Roles {
		roleIDs[i] = r.ID
		roleNames[i] = r.Name
	}
	return models.UserDTO{
		ID:       u.ID,
		Username: u.Username,
		Name:     u.Name,
		Email:    u.Email,
		Mobile:   u.Mobile,
		Roles:    roleNames,
		RoleIDs:  roleIDs,
	}
}

func (ctrl *Controller) ApiMeGet(c *echo.Context) error {
	user, ok := c.Get("user").(models.User)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
	}
	return c.JSON(http.StatusOK, mePayload(ctrl.DB, user))
}

type MeUpdateRequest struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Mobile          string `json:"mobile"`
	CurrentPassword string `json:"current_password,omitempty"`
	NewPassword     string `json:"new_password,omitempty"`
}

func (ctrl *Controller) ApiMeUpdate(c *echo.Context) error {
	current, ok := c.Get("user").(models.User)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "not authenticated"})
	}

	var req MeUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	var user models.User
	if err := ctrl.DB.First(&user, current.ID).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	user.Name = req.Name
	user.Email = req.Email
	user.Mobile = req.Mobile

	if req.NewPassword != "" {
		if user.PasswordHash == ldapSentinelPassword {
			if err := ctrl.changeLDAPUserPassword(&user, req.CurrentPassword, req.NewPassword); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			}
			// PasswordHash stays the sentinel - the real password lives in
			// the directory, not here.
		} else {
			if !CheckPasswordHash(req.CurrentPassword, user.PasswordHash) {
				return c.JSON(http.StatusBadRequest, map[string]any{"error": "current password is incorrect"})
			}
			hash, err := HashPassword(req.NewPassword)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to hash password"})
			}
			user.PasswordHash = hash
		}
	}

	if err := ctrl.DB.Save(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	// user was re-fetched without preloading Roles; current (from the auth
	// middleware) already has it and this endpoint never changes roles.
	user.Roles = current.Roles
	return c.JSON(http.StatusOK, toUserDTO(user))
}

// changeLDAPUserPassword handles ApiMeUpdate's self-service "change
// password" for an LDAP-backed user (PasswordHash == ldapSentinelPassword):
// verifies currentPassword against the directory itself (bcrypt can't, the
// sentinel isn't a real hash), then writes newPassword back via
// ldapauth.ChangePassword - only if an admin has opted into write-back
// (Settings.LdapAllowPasswordChange) and the elevated write-back identity
// is actually configured (util.Config.LdapWriteback, config-file only).
// Otherwise this is where the change is still cleanly refused, same as
// before this feature existed, just with a clearer reason.
func (ctrl *Controller) changeLDAPUserPassword(user *models.User, currentPassword, newPassword string) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return err
	}
	if settings.LdapEnabled == nil || !*settings.LdapEnabled {
		return fmt.Errorf("LDAP authentication is disabled; contact your administrator")
	}

	cfg := ldapauth.ConfigFromSettings(settings)
	ok, _, err := ldapAuthenticate(cfg, user.Username, currentPassword)
	if err != nil {
		return fmt.Errorf("failed to verify current password: %w", err)
	}
	if !ok {
		return fmt.Errorf("current password is incorrect")
	}

	if !ldapWritebackAvailable(settings) {
		return fmt.Errorf("your password is managed by your organization's directory service; contact your administrator to change it")
	}

	wb := ldapauth.WritebackConfig{
		BindDN:   util.Config.LdapWriteback.BindDN,
		Password: util.Config.LdapWriteback.Password,
	}
	if err := ldapauth.ChangePassword(cfg, wb, user.Username, newPassword); err != nil {
		return fmt.Errorf("failed to update password in the directory: %w", err)
	}
	return nil
}
