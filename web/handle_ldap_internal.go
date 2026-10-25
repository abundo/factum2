package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/ldapsrv"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

type ldapInternalDTO struct {
	Enabled           bool            `json:"enabled"`
	Listen            string          `json:"listen"`
	TLSListen         string          `json:"tls_listen"`
	TLSCertificateID  *uint           `json:"tls_certificate_id"`
	Certificates      []tlsCertChoice `json:"certificates"`
	BaseDN            string          `json:"base_dn"`
	BindDN            string          `json:"bind_dn"`
	BindPasswordSet   bool            `json:"bind_password_set"`
	RadiusUseInternal bool            `json:"radius_use_internal"`
	UserFilter        string          `json:"user_filter"`
	OUs               []ouDTO         `json:"ous"`
	Placements        []placeDTO      `json:"placements"`
	Nests             []nestDTO       `json:"nests"`
}

type ouDTO struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	ParentID *uint  `json:"parent_id"`
	DN       string `json:"dn"`
}

type placeDTO struct {
	Kind string `json:"kind"`
	ID   uint   `json:"id"`
	OUID *uint  `json:"ou_id"`
}

type nestDTO struct {
	RoleID       uint `json:"role_id"`
	MemberRoleID uint `json:"member_role_id"`
}

type tlsCertChoice struct {
	ID      uint     `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

type ldapInternalSettingsRequest struct {
	Enabled           bool    `json:"enabled"`
	Listen            string  `json:"listen"`
	TLSListen         string  `json:"tls_listen"`
	TLSCertificateID  *uint   `json:"tls_certificate_id"`
	BaseDN            string  `json:"base_dn"`
	BindPassword      *string `json:"bind_password"`
	RadiusUseInternal bool    `json:"radius_use_internal"`
}

func (ctrl *Controller) ApiLdapConfig(c *echo.Context) error {
	cfg, err := ctrl.buildLdapConfig()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, cfg)
}

func (ctrl *Controller) buildLdapConfig() (ldapsrv.Config, error) {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return ldapsrv.Config{}, err
	}
	in, err := ctrl.directoryInput(settings)
	if err != nil {
		return ldapsrv.Config{}, err
	}
	dir := ldapsrv.Build(in)
	enabled := settings.LdapInternalEnabled != nil && *settings.LdapInternalEnabled
	listen := strings.TrimSpace(settings.LdapInternalListen)
	cfg := dir.Snapshot(enabled, listen, strings.TrimSpace(settings.LdapInternalTLSListen), settings.LdapInternalBindPassword)
	name, domain, err := ctrl.internalTLSFiles(settings)
	if err != nil {
		return ldapsrv.Config{}, err
	}
	cfg.TLSCertificate = name
	cfg.TLSDomain = domain
	return cfg, nil
}

func (ctrl *Controller) directoryInput(settings *models.Settings) (ldapsrv.Input, error) {
	base := strings.TrimSpace(settings.LdapInternalBaseDN)
	if base == "" {
		base = ldapsrv.DefaultBaseDN
	}
	var ous []models.LdapOU
	if err := ctrl.DB.Order("id").Find(&ous).Error; err != nil {
		return ldapsrv.Input{}, err
	}
	var users []models.User
	if err := ctrl.DB.Preload("Roles").Order("username").Find(&users).Error; err != nil {
		return ldapsrv.Input{}, err
	}
	var roles []models.Role
	if err := ctrl.DB.Order("name").Find(&roles).Error; err != nil {
		return ldapsrv.Input{}, err
	}
	var nests []models.RoleMember
	if err := ctrl.DB.Order("role_id, member_role_id").Find(&nests).Error; err != nil {
		return ldapsrv.Input{}, err
	}
	members := map[uint][]uint{}
	for _, n := range nests {
		members[n.RoleID] = append(members[n.RoleID], n.MemberRoleID)
	}
	in := ldapsrv.Input{BaseDN: base}
	for _, ou := range ous {
		in.OUs = append(in.OUs, ldapsrv.OU{ID: ou.ID, ParentID: ou.ParentID, Name: ou.Name})
	}
	for _, u := range users {
		var ids []uint
		for _, r := range u.Roles {
			ids = append(ids, r.ID)
		}
		in.Users = append(in.Users, ldapsrv.User{
			ID:       u.ID,
			Username: u.Username,
			Name:     u.Name,
			Email:    u.Email,
			Mobile:   u.Mobile,
			Password: u.PasswordHash,
			OUID:     u.LdapOUID,
			RoleIDs:  ids,
		})
	}
	for _, r := range roles {
		in.Roles = append(in.Roles, ldapsrv.Role{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			OUID:        r.LdapOUID,
			MemberIDs:   members[r.ID],
		})
	}
	return in, nil
}

func internalLDAPAuth(settings *models.Settings) ldapauth.Config {
	base := strings.TrimSpace(settings.LdapInternalBaseDN)
	if base == "" {
		base = ldapsrv.DefaultBaseDN
	}
	listen := strings.TrimSpace(settings.LdapInternalListen)
	if listen == "" {
		listen = ldapsrv.DefaultListen
	}
	return ldapauth.Config{
		Host:            "127.0.0.1",
		Port:            ldapsrv.Port(listen),
		TLSMode:         "none",
		ServerType:      "generic",
		BindDN:          ldapsrv.ServiceBindDN(base),
		BindPassword:    settings.LdapInternalBindPassword,
		BaseDN:          base,
		UserFilter:      "(uid=%s)",
		AttrUsername:    "uid",
		AttrEmail:       "mail",
		AttrDisplayName: "displayName",
		AttrMobile:      "mobile",
		AttrGroups:      "memberOf",
	}
}

func (ctrl *Controller) ApiLdapInternalGet(c *echo.Context) error {
	dto, err := ctrl.ldapInternalView()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, dto)
}

func (ctrl *Controller) ldapInternalView() (ldapInternalDTO, error) {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return ldapInternalDTO{}, err
	}
	in, err := ctrl.directoryInput(settings)
	if err != nil {
		return ldapInternalDTO{}, err
	}
	dir := ldapsrv.Build(in)
	certs, err := ctrl.tlsCertificateChoices()
	if err != nil {
		return ldapInternalDTO{}, err
	}
	dto := ldapInternalDTO{
		Enabled:           settings.LdapInternalEnabled != nil && *settings.LdapInternalEnabled,
		Listen:            settings.LdapInternalListen,
		TLSListen:         settings.LdapInternalTLSListen,
		TLSCertificateID:  settings.LdapInternalTLSCertificateID,
		Certificates:      certs,
		BaseDN:            in.BaseDN,
		BindDN:            dir.BindDN,
		BindPasswordSet:   settings.LdapInternalBindPassword != "",
		RadiusUseInternal: settings.RadiusUseInternalLDAP != nil && *settings.RadiusUseInternalLDAP,
		UserFilter:        "(uid=%s)",
		OUs:               []ouDTO{},
		Placements:        []placeDTO{},
		Nests:             []nestDTO{},
	}
	if strings.TrimSpace(settings.LdapInternalBaseDN) == "" {
		dto.BaseDN = in.BaseDN
	}
	for _, ou := range in.OUs {
		dto.OUs = append(dto.OUs, ouDTO{ID: ou.ID, Name: ou.Name, ParentID: ou.ParentID, DN: dir.OUDN[ou.ID]})
	}
	for _, u := range in.Users {
		if u.OUID == nil {
			continue
		}
		dto.Placements = append(dto.Placements, placeDTO{Kind: "user", ID: u.ID, OUID: u.OUID})
	}
	for _, r := range in.Roles {
		if r.OUID != nil {
			dto.Placements = append(dto.Placements, placeDTO{Kind: "role", ID: r.ID, OUID: r.OUID})
		}
		for _, m := range r.MemberIDs {
			dto.Nests = append(dto.Nests, nestDTO{RoleID: r.ID, MemberRoleID: m})
		}
	}
	return dto, nil
}

func (ctrl *Controller) ApiLdapInternalUpdate(c *echo.Context) error {
	var req ldapInternalSettingsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := applyInternalSettings(settings, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.assignInternalTLSCertificate(settings, req.TLSCertificateID); err != nil {
		return ldapErrStatus(c, err)
	}
	if err := saveSettings(ctrl.DB, settings); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	dto, err := ctrl.ldapInternalView()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, dto)
}

func applyInternalSettings(settings *models.Settings, req ldapInternalSettingsRequest) error {
	if err := ldapsrv.ValidateListen(req.Listen); err != nil {
		return err
	}
	if err := ldapsrv.ValidateListen(req.TLSListen); err != nil {
		return err
	}
	listen := strings.TrimSpace(req.Listen)
	tlsListen := strings.TrimSpace(req.TLSListen)
	if listen != "" && listen == tlsListen {
		return errLDAP("LDAP and LDAPS must use different addresses")
	}
	base := strings.TrimSpace(req.BaseDN)
	if req.Enabled && base == "" {
		base = ldapsrv.DefaultBaseDN
	}
	if base != "" && !ldapsrv.ValidBaseDN(base) {
		return errLDAP("base DN is invalid")
	}
	on := req.Enabled
	use := req.RadiusUseInternal
	settings.LdapInternalEnabled = &on
	settings.RadiusUseInternalLDAP = &use
	settings.LdapInternalListen = listen
	settings.LdapInternalTLSListen = tlsListen
	settings.LdapInternalBaseDN = base
	if req.BindPassword != nil && *req.BindPassword != "" {
		settings.LdapInternalBindPassword = *req.BindPassword
	}
	return nil
}

func (ctrl *Controller) assignInternalTLSCertificate(settings *models.Settings, id *uint) error {
	if id == nil || *id == 0 {
		settings.LdapInternalTLSCertificateID = nil
		return nil
	}
	cert, err := ctrl.tlsCertificate(*id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errLDAP("certificate not found")
	}
	if err != nil {
		return err
	}
	if firstCertDomain(cert) == "" {
		return errLDAP("certificate has no domain")
	}
	chosen := cert.ID
	settings.LdapInternalTLSCertificateID = &chosen
	return nil
}

func (ctrl *Controller) internalTLSFiles(settings *models.Settings) (string, string, error) {
	if settings.LdapInternalTLSCertificateID == nil || *settings.LdapInternalTLSCertificateID == 0 {
		return "", "", nil
	}
	cert, err := ctrl.tlsCertificate(*settings.LdapInternalTLSCertificateID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return cert.Name, firstCertDomain(cert), nil
}

func (ctrl *Controller) tlsCertificate(id uint) (models.Certificate, error) {
	var cert models.Certificate
	err := ctrl.DB.Preload("Domains", func(db *gorm.DB) *gorm.DB {
		return db.Order("rank")
	}).First(&cert, id).Error
	return cert, err
}

func (ctrl *Controller) tlsCertificateChoices() ([]tlsCertChoice, error) {
	var items []models.Certificate
	if err := ctrl.DB.Preload("Domains", func(db *gorm.DB) *gorm.DB {
		return db.Order("rank")
	}).Order("name").Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]tlsCertChoice, 0, len(items))
	for _, c := range items {
		ch := tlsCertChoice{ID: c.ID, Name: c.Name, Domains: []string{}}
		for _, d := range c.Domains {
			if name := strings.TrimSpace(d.Name); name != "" {
				ch.Domains = append(ch.Domains, name)
			}
		}
		out = append(out, ch)
	}
	return out, nil
}

func firstCertDomain(c models.Certificate) string {
	for _, d := range c.Domains {
		if name := strings.TrimSpace(d.Name); name != "" {
			return name
		}
	}
	return ""
}

// saveSettings writes the row. Gorm's Save leaves a nil pointer column
// unchanged, so a cleared LDAPS certificate is stored as NULL on purpose.
func saveSettings(db *gorm.DB, settings *models.Settings) error {
	if err := db.Save(settings).Error; err != nil {
		return err
	}
	if settings.LdapInternalTLSCertificateID != nil {
		return nil
	}
	return db.Model(settings).Update("ldap_internal_tls_certificate_id", gorm.Expr("NULL")).Error
}

func ldapErrStatus(c *echo.Context, err error) error {
	var le *ldapError
	if errors.As(err, &le) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func validateInternalLDAP(settings *models.Settings) error {
	if err := ldapsrv.ValidateListen(settings.LdapInternalListen); err != nil {
		return err
	}
	if err := ldapsrv.ValidateListen(settings.LdapInternalTLSListen); err != nil {
		return err
	}
	listen := strings.TrimSpace(settings.LdapInternalListen)
	tlsListen := strings.TrimSpace(settings.LdapInternalTLSListen)
	if listen != "" && listen == tlsListen {
		return errLDAP("LDAP and LDAPS must use different addresses")
	}
	base := strings.TrimSpace(settings.LdapInternalBaseDN)
	if settings.LdapInternalEnabled != nil && *settings.LdapInternalEnabled && base == "" {
		settings.LdapInternalBaseDN = ldapsrv.DefaultBaseDN
		base = settings.LdapInternalBaseDN
	}
	if base != "" && !ldapsrv.ValidBaseDN(base) {
		return errLDAP("base DN is invalid")
	}
	return nil
}

func errLDAP(msg string) error { return &ldapError{msg} }

type ldapError struct{ msg string }

func (e *ldapError) Error() string { return e.msg }

func (ctrl *Controller) ApiLdapOUCreate(c *echo.Context) error {
	var req struct {
		Name     string `json:"name"`
		ParentID *uint  `json:"parent_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(req.Name)
	if !ldapsrv.ValidOUName(name) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "folder name must be letters, digits, spaces, or - _ ."})
	}
	var ous []models.LdapOU
	if err := ctrl.DB.Find(&ous).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if req.ParentID != nil && !ouExists(ous, *req.ParentID) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "parent folder not found"})
	}
	if ouNameTaken(ous, req.ParentID, name, 0) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "a folder with that name is already here"})
	}
	row := models.LdapOU{Name: name, ParentID: req.ParentID}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, ouDTO{ID: row.ID, Name: row.Name, ParentID: row.ParentID})
}

func (ctrl *Controller) ApiLdapOUUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "bad id"})
	}
	var req struct {
		Name     string `json:"name"`
		ParentID *uint  `json:"parent_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(req.Name)
	if !ldapsrv.ValidOUName(name) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "folder name must be letters, digits, spaces, or - _ ."})
	}
	var ous []models.LdapOU
	if err := ctrl.DB.Find(&ous).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if !ouExists(ous, id) {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "folder not found"})
	}
	if req.ParentID != nil && !ouExists(ous, *req.ParentID) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "parent folder not found"})
	}
	if ouCycle(ous, id, req.ParentID) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "a folder cannot sit inside itself"})
	}
	if ouNameTaken(ous, req.ParentID, name, id) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "a folder with that name is already here"})
	}
	if err := ctrl.rewriteRoleDNs(func() error {
		// A nil *uint in a map is not SQL NULL. Use an untyped nil so a
		// folder can move back to the base.
		var parent any
		if req.ParentID != nil {
			parent = *req.ParentID
		}
		return ctrl.DB.Model(&models.LdapOU{}).Where("id = ?", id).Updates(map[string]any{
			"name":      name,
			"parent_id": parent,
		}).Error
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, ouDTO{ID: id, Name: name, ParentID: req.ParentID})
}

func (ctrl *Controller) ApiLdapOUDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "bad id"})
	}
	var n int64
	if err := ctrl.DB.Model(&models.LdapOU{}).Where("parent_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "move the folders inside this one first"})
	}
	if err := ctrl.DB.Model(&models.User{}).Where("ldap_ou_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "move the people in this folder first"})
	}
	if err := ctrl.DB.Model(&models.Role{}).Where("ldap_ou_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "move the roles in this folder first"})
	}
	res := ctrl.DB.Delete(&models.LdapOU{}, id)
	if res.Error != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "folder not found"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiLdapPlace(c *echo.Context) error {
	var req placeDTO
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if req.OUID != nil {
		var n int64
		if err := ctrl.DB.Model(&models.LdapOU{}).Where("id = ?", *req.OUID).Count(&n).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if n == 0 {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "folder not found"})
		}
	}
	var ou any
	if req.OUID != nil {
		ou = *req.OUID
	}
	switch req.Kind {
	case "user":
		res := ctrl.DB.Model(&models.User{}).Where("id = ?", req.ID).Update("ldap_ou_id", ou)
		if res.Error != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
		}
		if res.RowsAffected == 0 {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "user not found"})
		}
	case "role":
		err := ctrl.rewriteRoleDNs(func() error {
			res := ctrl.DB.Model(&models.Role{}).Where("id = ?", req.ID).Update("ldap_ou_id", ou)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return errLDAP("role not found")
			}
			return nil
		})
		if err != nil {
			if err.Error() == "role not found" {
				return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			}
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	default:
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "kind must be user or role"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiRoleMembersUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "bad id"})
	}
	var req struct {
		MemberRoleIDs []uint `json:"member_role_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var role models.Role
	if err := ctrl.DB.First(&role, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "role not found"})
	}
	seen := map[uint]bool{}
	var members []uint
	for _, m := range req.MemberRoleIDs {
		if m == 0 || m == id || seen[m] {
			continue
		}
		seen[m] = true
		var n int64
		if err := ctrl.DB.Model(&models.Role{}).Where("id = ?", m).Count(&n).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if n == 0 {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "member role not found"})
		}
		members = append(members, m)
	}
	var existing []models.RoleMember
	if err := ctrl.DB.Find(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if memberCycle(id, members, existing) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "that membership would loop"})
	}
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", id).Delete(&models.RoleMember{}).Error; err != nil {
			return err
		}
		for _, m := range members {
			if err := tx.Create(&models.RoleMember{RoleID: id, MemberRoleID: m}).Error; err != nil {
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

func memberCycle(role uint, members []uint, existing []models.RoleMember) bool {
	children := map[uint][]uint{}
	for _, row := range existing {
		if row.RoleID == role {
			continue
		}
		children[row.RoleID] = append(children[row.RoleID], row.MemberRoleID)
	}
	children[role] = members
	stack := map[uint]bool{}
	var walk func(uint) bool
	walk = func(id uint) bool {
		if stack[id] {
			return true
		}
		stack[id] = true
		for _, m := range children[id] {
			if walk(m) {
				return true
			}
		}
		stack[id] = false
		return false
	}
	return walk(role)
}

func ouExists(ous []models.LdapOU, id uint) bool {
	for _, ou := range ous {
		if ou.ID == id {
			return true
		}
	}
	return false
}

func ouNameTaken(ous []models.LdapOU, parent *uint, name string, self uint) bool {
	for _, ou := range ous {
		if ou.ID == self || !sameParent(ou.ParentID, parent) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ou.Name), name) {
			return true
		}
	}
	return false
}

func sameParent(a, b *uint) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func ouCycle(ous []models.LdapOU, id uint, parent *uint) bool {
	byID := map[uint]models.LdapOU{}
	for _, ou := range ous {
		byID[ou.ID] = ou
	}
	seen := map[uint]bool{}
	for parent != nil {
		if *parent == id || seen[*parent] {
			return true
		}
		seen[*parent] = true
		ou, ok := byID[*parent]
		if !ok {
			return false
		}
		parent = ou.ParentID
	}
	return false
}

func (ctrl *Controller) ApiRoleUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.RoleDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	var role models.Role
	if err := ctrl.DB.First(&role, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if err := ctrl.rewriteRoleDNs(func() error {
		role.Name = name
		role.Description = dto.Description
		return ctrl.DB.Save(&role).Error
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, models.RoleDTO{ID: role.ID, Name: role.Name, Description: role.Description})
}

func (ctrl *Controller) rewriteRoleDNs(change func() error) error {
	before, err := ctrl.roleDNs()
	if err != nil {
		return err
	}
	if err := change(); err != nil {
		return err
	}
	after, err := ctrl.roleDNs()
	if err != nil {
		return err
	}
	for id, old := range before {
		neu := after[id]
		if neu == "" || strings.EqualFold(old, neu) {
			continue
		}
		if err := ctrl.DB.Model(&models.RadiusPolicy{}).Where("lower(group_dn) = ?", strings.ToLower(old)).Update("group_dn", neu).Error; err != nil {
			return err
		}
		if err := ctrl.DB.Model(&models.LdapRoleMapping{}).Where("lower(group_dn) = ?", strings.ToLower(old)).Update("group_dn", neu).Error; err != nil {
			return err
		}
	}
	return nil
}

func (ctrl *Controller) roleDNs() (map[uint]string, error) {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return nil, err
	}
	in, err := ctrl.directoryInput(settings)
	if err != nil {
		return nil, err
	}
	return ldapsrv.Build(in).RoleDN, nil
}
