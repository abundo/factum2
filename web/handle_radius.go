package web

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/radius"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/go-ldap/ldap/v3"
	"github.com/labstack/echo/v5"
)

const radiusEventKeep = 10000

type radiusClientDTO struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Secret    string `json:"secret,omitempty"`
	SecretSet bool   `json:"secret_set"`
	Enabled   bool   `json:"enabled"`
}

type radiusPolicyDTO struct {
	ID         uint     `json:"id"`
	GroupDN    string   `json:"group_dn"`
	AllDevices bool     `json:"all_devices"`
	Roles      []string `json:"roles"`
}

type radiusEventBatch struct {
	Events []models.RadiusEvent `json:"events"`
}

func (ctrl *Controller) ApiRadiusConfig(c *echo.Context) error {
	cfg, err := ctrl.buildRadiusConfig()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, cfg)
}

func (ctrl *Controller) buildRadiusConfig() (radius.Config, error) {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return radius.Config{}, err
	}
	cfg := radius.Config{
		Enabled: settings.RadiusEnabled != nil && *settings.RadiusEnabled,
		Listen:  strings.TrimSpace(settings.RadiusListen),
		LDAP:    ldapauth.ConfigFromSettings(settings),
		Reply:   radius.ReplyText(settings.RadiusReply),
		Machine: radius.Machine{
			Account:  strings.TrimSpace(settings.RadiusMachineAccount),
			Password: settings.RadiusMachinePassword,
			Domain:   strings.TrimSpace(settings.RadiusMachineDomain),
		},
	}
	if cfg.Listen == "" {
		cfg.Listen = radius.DefaultListen
	}
	var clients []models.RadiusClient
	if err := ctrl.DB.Where("enabled", true).Order("name").Find(&clients).Error; err != nil {
		return radius.Config{}, err
	}
	for _, c := range clients {
		cfg.Clients = append(cfg.Clients, radius.Client{Name: c.Name, Address: c.Address, Secret: c.Secret})
	}
	var policies []models.RadiusPolicy
	if err := ctrl.DB.Order("group_dn").Find(&policies).Error; err != nil {
		return radius.Config{}, err
	}
	for _, p := range policies {
		cfg.Policies = append(cfg.Policies, radius.Policy{
			GroupDN:    p.GroupDN,
			AllDevices: p.AllDevices,
			Roles:      splitRadiusRoles(p.Roles),
		})
	}
	devices, err := ctrl.radiusDevices()
	if err != nil {
		return radius.Config{}, err
	}
	cfg.Devices = devices
	return cfg, nil
}

func (ctrl *Controller) radiusDevices() ([]radius.Device, error) {
	var rows []models.Device
	if err := ctrl.DB.Select("id", "name", "role", "enabled", "primary_ipv4", "primary_ipv6").Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]*radius.Device, len(rows))
	var ordered []uint
	for _, d := range rows {
		dev := radius.Device{Name: d.Name, Role: d.Role, Enabled: d.Enabled}
		addRadiusAddr(&dev, d.PrimaryIPv4)
		addRadiusAddr(&dev, d.PrimaryIPv6)
		byID[d.ID] = &dev
		ordered = append(ordered, d.ID)
	}
	var addrs []struct {
		DeviceID uint
		Address  string
	}
	err := ctrl.DB.Table("addresses").
		Select("interfaces.device_id as device_id, addresses.address as address").
		Joins("join interfaces on interfaces.id = addresses.interface_id").
		Scan(&addrs).Error
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if dev := byID[a.DeviceID]; dev != nil {
			addRadiusAddr(dev, a.Address)
		}
	}
	out := make([]radius.Device, 0, len(ordered))
	for _, id := range ordered {
		dev := byID[id]
		if len(dev.Addresses) == 0 {
			continue
		}
		out = append(out, *dev)
	}
	return out, nil
}

func addRadiusAddr(dev *radius.Device, addr string) {
	addr = strings.TrimSpace(addr)
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		addr = addr[:i]
	}
	if addr == "" || strings.IndexFunc(addr, unicode.IsSpace) >= 0 {
		return
	}
	for _, have := range dev.Addresses {
		if have == addr {
			return
		}
	}
	dev.Addresses = append(dev.Addresses, addr)
}

func splitRadiusRoles(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func joinRadiusRoles(roles []string) string {
	var out []string
	seen := map[string]bool{}
	for _, r := range roles {
		r = strings.TrimSpace(r)
		if r == "" || seen[strings.ToLower(r)] {
			continue
		}
		seen[strings.ToLower(r)] = true
		out = append(out, r)
	}
	return strings.Join(out, "\n")
}

func (ctrl *Controller) ApiRadiusClients(c *echo.Context) error {
	var rows []models.RadiusClient
	if err := ctrl.DB.Order("name").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := make([]radiusClientDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, radiusClientDTO{
			ID: row.ID, Name: row.Name, Address: row.Address, SecretSet: row.Secret != "", Enabled: row.Enabled,
		})
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiRadiusClientSecret(c *echo.Context) error {
	row, status, err := ctrl.radiusClient(c)
	if err != nil {
		return c.JSON(status, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"secret": row.Secret})
}

func (ctrl *Controller) ApiRadiusClientCreate(c *echo.Context) error {
	var dto radiusClientDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := validateRadiusClient(dto, true)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "could not save client"})
	}
	dto.ID = row.ID
	dto.Address = row.Address
	dto.Secret = ""
	dto.SecretSet = true
	return c.JSON(http.StatusOK, dto)
}

func (ctrl *Controller) ApiRadiusClientUpdate(c *echo.Context) error {
	row, status, err := ctrl.radiusClient(c)
	if err != nil {
		return c.JSON(status, map[string]any{"error": err.Error()})
	}
	var dto radiusClientDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.Secret == "" {
		dto.Secret = row.Secret
	}
	next, err := validateRadiusClient(dto, false)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row.Name = next.Name
	row.Address = next.Address
	row.Secret = next.Secret
	row.Enabled = next.Enabled
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "could not save client"})
	}
	return c.JSON(http.StatusOK, radiusClientDTO{
		ID: row.ID, Name: row.Name, Address: row.Address, SecretSet: row.Secret != "", Enabled: row.Enabled,
	})
}

func (ctrl *Controller) ApiRadiusClientDelete(c *echo.Context) error {
	row, status, err := ctrl.radiusClient(c)
	if err != nil {
		return c.JSON(status, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Delete(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

func (ctrl *Controller) radiusClient(c *echo.Context) (models.RadiusClient, int, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return models.RadiusClient{}, http.StatusBadRequest, errRadius("bad id")
	}
	var row models.RadiusClient
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return models.RadiusClient{}, http.StatusNotFound, errRadius("client not found")
	}
	return row, 0, nil
}

func validateRadiusClient(dto radiusClientDTO, secretRequired bool) (models.RadiusClient, error) {
	name := strings.TrimSpace(dto.Name)
	if name == "" || len(name) > 255 {
		return models.RadiusClient{}, errRadius("name is required")
	}
	addr := strings.TrimSpace(dto.Address)
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		addr = addr[:i]
	}
	ip := canonClientIP(addr)
	if ip == "" {
		return models.RadiusClient{}, errRadius("address must be an IP address")
	}
	secret := dto.Secret
	if secretRequired && secret == "" {
		return models.RadiusClient{}, errRadius("secret is required")
	}
	if len(secret) > 128 || strings.IndexFunc(secret, unicode.IsControl) >= 0 {
		return models.RadiusClient{}, errRadius("secret is invalid")
	}
	return models.RadiusClient{Name: name, Address: ip, Secret: secret, Enabled: dto.Enabled}, nil
}

func canonClientIP(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "[]")
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func (ctrl *Controller) ApiRadiusPolicies(c *echo.Context) error {
	var rows []models.RadiusPolicy
	if err := ctrl.DB.Order("group_dn").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := make([]radiusPolicyDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, policyDTO(row))
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiRadiusPolicyCreate(c *echo.Context) error {
	var dto radiusPolicyDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := validateRadiusPolicy(dto)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "could not save group"})
	}
	return c.JSON(http.StatusOK, policyDTO(row))
}

func (ctrl *Controller) ApiRadiusPolicyUpdate(c *echo.Context) error {
	row, status, err := ctrl.radiusPolicy(c)
	if err != nil {
		return c.JSON(status, map[string]any{"error": err.Error()})
	}
	var dto radiusPolicyDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	next, err := validateRadiusPolicy(dto)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row.GroupDN = next.GroupDN
	row.AllDevices = next.AllDevices
	row.Roles = next.Roles
	if err := ctrl.DB.Save(&row).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "could not save group"})
	}
	return c.JSON(http.StatusOK, policyDTO(row))
}

func (ctrl *Controller) ApiRadiusPolicyDelete(c *echo.Context) error {
	row, status, err := ctrl.radiusPolicy(c)
	if err != nil {
		return c.JSON(status, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Delete(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

func (ctrl *Controller) radiusPolicy(c *echo.Context) (models.RadiusPolicy, int, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return models.RadiusPolicy{}, http.StatusBadRequest, errRadius("bad id")
	}
	var row models.RadiusPolicy
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return models.RadiusPolicy{}, http.StatusNotFound, errRadius("group not found")
	}
	return row, 0, nil
}

func validateRadiusPolicy(dto radiusPolicyDTO) (models.RadiusPolicy, error) {
	dn := ldapauth.NormalizeDN(dto.GroupDN)
	if dn == "" {
		return models.RadiusPolicy{}, errRadius("group DN is required")
	}
	if _, err := ldap.ParseDN(dn); err != nil {
		return models.RadiusPolicy{}, errRadius("group DN is invalid")
	}
	roles := joinRadiusRoles(dto.Roles)
	if !dto.AllDevices && roles == "" {
		return models.RadiusPolicy{}, errRadius("choose all devices or at least one role")
	}
	if dto.AllDevices {
		roles = ""
	}
	return models.RadiusPolicy{GroupDN: dn, AllDevices: dto.AllDevices, Roles: roles}, nil
}

func policyDTO(row models.RadiusPolicy) radiusPolicyDTO {
	return radiusPolicyDTO{ID: row.ID, GroupDN: row.GroupDN, AllDevices: row.AllDevices, Roles: splitRadiusRoles(row.Roles)}
}

func (ctrl *Controller) ApiRadiusDeviceRoles(c *echo.Context) error {
	var roles []string
	if err := ctrl.DB.Model(&models.Device{}).Where("role <> ''").Distinct().Pluck("role", &roles).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if roles == nil {
		roles = []string{}
	}
	return c.JSON(http.StatusOK, roles)
}

func (ctrl *Controller) ApiRadiusEvents(c *echo.Context) error {
	if c.Request().Method == http.MethodGet {
		var rows []models.RadiusEvent
		if err := ctrl.DB.Order("id desc").Limit(200).Find(&rows).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if rows == nil {
			rows = []models.RadiusEvent{}
		}
		return c.JSON(http.StatusOK, rows)
	}
	var batch radiusEventBatch
	if err := c.Bind(&batch); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if len(batch.Events) == 0 || len(batch.Events) > 100 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "expected 1 to 100 events"})
	}
	now := time.Now().UTC()
	rows := make([]models.RadiusEvent, 0, len(batch.Events))
	for _, ev := range batch.Events {
		clean, err := cleanRadiusEvent(ev, now)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
		rows = append(rows, clean)
	}
	if err := ctrl.DB.Create(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	ctrl.trimRadiusEvents()
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "stored": len(rows)})
}

func cleanRadiusEvent(ev models.RadiusEvent, now time.Time) (models.RadiusEvent, error) {
	switch ev.Result {
	case "accept", "reject", "drop":
	default:
		return models.RadiusEvent{}, errRadius("bad result")
	}
	ev.ID = 0
	ev.Username = clip(ev.Username, 255)
	ev.NASIP = clip(ev.NASIP, 64)
	ev.DeviceName = clip(ev.DeviceName, 255)
	ev.DeviceRole = clip(ev.DeviceRole, 255)
	ev.Reason = clip(ev.Reason, 255)
	ev.Worker = clip(ev.Worker, 255)
	if ev.Result == "drop" {
		ev.Username = ""
	}
	if strings.IndexFunc(ev.Username, unicode.IsControl) >= 0 || strings.IndexFunc(ev.Reason, unicode.IsControl) >= 0 {
		return models.RadiusEvent{}, errRadius("invalid event text")
	}
	if ev.ReportedAt.IsZero() || ev.ReportedAt.Before(now.Add(-24*time.Hour)) || ev.ReportedAt.After(now.Add(5*time.Minute)) {
		ev.ReportedAt = now
	}
	return ev, nil
}

func (ctrl *Controller) trimRadiusEvents() {
	var n int64
	if err := ctrl.DB.Model(&models.RadiusEvent{}).Count(&n).Error; err != nil || n <= radiusEventKeep {
		return
	}
	var cutoff models.RadiusEvent
	if err := ctrl.DB.Order("id desc").Offset(radiusEventKeep).Limit(1).Find(&cutoff).Error; err != nil || cutoff.ID == 0 {
		return
	}
	_ = ctrl.DB.Where("id <= ?", cutoff.ID).Delete(&models.RadiusEvent{}).Error
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

type radiusInputError struct{ msg string }

func (e radiusInputError) Error() string { return e.msg }

func errRadius(msg string) error { return radiusInputError{msg} }
