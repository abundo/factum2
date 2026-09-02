package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func configWriteError(c *echo.Context, err error) error {
	if se := cfgmgmt.AsStatusError(err); se != nil {
		return c.JSON(se.Status, map[string]any{"error": se.Message})
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func (ctrl *Controller) ApiConfigScopeList(c *echo.Context) error {
	rows, err := cfgmgmt.ListScopes(ctrl.DB)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiConfigScopeTree(c *echo.Context) error {
	tree, err := cfgmgmt.ScopeTree(ctrl.DB)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, tree)
}

func (ctrl *Controller) ApiConfigScopeCreate(c *echo.Context) error {
	var dto models.ConfigScopeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.DeviceID != nil && dto.Kind == models.ConfigScopeKindDevice {
		parent := uint(0)
		if dto.ParentID != nil {
			parent = *dto.ParentID
		} else {
			root, err := cfgmgmt.RootScope(ctrl.DB)
			if err != nil {
				return configWriteError(c, err)
			}
			parent = root.ID
		}
		node, err := cfgmgmt.AttachDevice(ctrl.DB, parent, *dto.DeviceID)
		if err != nil {
			return configWriteError(c, err)
		}
		return c.JSON(http.StatusCreated, node)
	}
	row := models.ConfigScope{
		ParentID: dto.ParentID, Name: dto.Name, Kind: dto.Kind,
		SiteID: dto.SiteID, DeviceID: dto.DeviceID, InterfaceID: dto.InterfaceID,
		SortOrder: dto.SortOrder,
	}
	created, err := cfgmgmt.CreateScope(ctrl.DB, &row)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusCreated, created)
}

func (ctrl *Controller) ApiConfigScopeUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var dto models.ConfigScopeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row := models.ConfigScope{
		ParentID: dto.ParentID, Name: dto.Name, Kind: dto.Kind,
		SiteID: dto.SiteID, DeviceID: dto.DeviceID, InterfaceID: dto.InterfaceID,
		SortOrder: dto.SortOrder,
	}
	updated, err := cfgmgmt.UpdateScope(ctrl.DB, id, &row)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, updated)
}

func (ctrl *Controller) ApiConfigScopeDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	if err := cfgmgmt.DeleteScope(ctrl.DB, id); err != nil {
		return configWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiConfigVariableList(c *echo.Context) error {
	var rows []models.ConfigVariableDef
	if err := ctrl.DB.Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	for i := range rows {
		cfgmgmt.RedactVariableSecrets(&rows[i])
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiConfigVariableGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var row models.ConfigVariableDef
	if err := ctrl.DB.First(&row, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	cfgmgmt.RedactVariableSecrets(&row)
	return c.JSON(http.StatusOK, row)
}

func emptyJSONNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func validateVariableDTO(dto *models.ConfigVariableDefDTO) error {
	dto.Name = strings.TrimSpace(dto.Name)
	dto.Type = strings.TrimSpace(dto.Type)
	if dto.Name == "" {
		return errors.New("name is required")
	}
	if !cfgmgmt.ValidVarType(dto.Type) {
		return errors.New("invalid variable type")
	}
	if dto.Type == models.VarTypeSecret {
		dto.Secret = true
	}
	def := models.ConfigVariableDef{
		Name: dto.Name, Type: dto.Type,
		DefaultValue: emptyJSONNull(dto.DefaultValue),
		Constraints:  emptyJSONNull(dto.Constraints),
		Secret:       dto.Secret,
	}
	return cfgmgmt.ValidateVariableDef(&def)
}

func (ctrl *Controller) ApiConfigVariableCreate(c *echo.Context) error {
	var dto models.ConfigVariableDefDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := validateVariableDTO(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row := models.ConfigVariableDef{
		Name: dto.Name, Type: dto.Type, Description: dto.Description,
		DefaultValue: emptyJSONNull(dto.DefaultValue), Constraints: emptyJSONNull(dto.Constraints),
		Secret: dto.Secret, Required: dto.Required, Platforms: emptyJSONNull(dto.Platforms),
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiConfigVariableUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var existing models.ConfigVariableDef
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.ConfigVariableDefDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.Type != "" && !cfgmgmt.ValidVarType(dto.Type) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid variable type"})
	}
	if dto.Name != "" {
		existing.Name = dto.Name
	}
	if dto.Type != "" {
		existing.Type = dto.Type
	}
	existing.Description = dto.Description
	secret := existing.Secret || existing.Type == models.VarTypeSecret || dto.Secret || dto.Type == models.VarTypeSecret
	if secret && cfgmgmt.SecretDefaultUnchanged(dto.DefaultValue) {
		// keep stored default
	} else {
		existing.DefaultValue = dto.DefaultValue
	}
	existing.Constraints = dto.Constraints
	existing.Secret = dto.Secret || dto.Type == models.VarTypeSecret
	existing.Required = dto.Required
	existing.Platforms = dto.Platforms
	if err := cfgmgmt.ValidateVariableDef(&existing); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Save(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, existing)
}

func (ctrl *Controller) ApiConfigVariableDelete(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigVariableDef, models.ConfigVariableDefDTO](ctrl.DB)
	return h.Delete(c)
}

func (ctrl *Controller) ApiConfigAssignmentList(c *echo.Context) error {
	var scopeID uint
	_ = echo.QueryParamsBinder(c).Uint("scope_id", &scopeID).BindError()
	rows, err := cfgmgmt.ListAssignments(ctrl.DB, scopeID)
	if err != nil {
		return configWriteError(c, err)
	}
	if err := cfgmgmt.RedactAssignmentSecrets(ctrl.DB, rows); err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiConfigAssignmentUpsert(c *echo.Context) error {
	var dto models.ConfigAssignmentDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.VariableDefID == 0 || dto.ScopeID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "variable_def_id and scope_id are required"})
	}
	row, err := cfgmgmt.UpsertAssignment(ctrl.DB, dto.VariableDefID, dto.ScopeID, dto.Value)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiConfigAssignmentDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	if err := cfgmgmt.DeleteAssignment(ctrl.DB, id); err != nil {
		return configWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

type resolvedVarJSON struct {
	Name        string `json:"name"`
	Value       any    `json:"value"`
	SourceID    *uint  `json:"source_id,omitempty"`
	SourceName  string `json:"source_name,omitempty"`
	FromDefault bool   `json:"from_default"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`
	Error       string `json:"error,omitempty"`
}

func (ctrl *Controller) ApiConfigResolve(c *echo.Context) error {
	var interfaceID uint
	_ = echo.QueryParamsBinder(c).Uint("interface_id", &interfaceID).BindError()
	if interfaceID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "interface_id is required"})
	}
	all, err := cfgmgmt.ResolveAll(ctrl.DB, interfaceID)
	if err != nil {
		return configWriteError(c, err)
	}
	all = cfgmgmt.RedactSecrets(all)
	out := make([]resolvedVarJSON, 0, len(all))
	for _, rv := range all {
		row := resolvedVarJSON{
			Name: rv.Name, Value: rv.Value, FromDefault: rv.FromDefault,
			Secret: rv.Secret, Required: rv.Required, Type: rv.Type,
		}
		if rv.Source != nil {
			id := rv.Source.ID
			row.SourceID = &id
			row.SourceName = rv.Source.Name
		}
		if rv.Err != nil {
			row.Error = rv.Err.Error()
		}
		out = append(out, row)
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiConfigMatrix(c *echo.Context) error {
	var scopeID uint
	varName := ""
	_ = echo.QueryParamsBinder(c).Uint("scope_id", &scopeID).String("variable", &varName).BindError()
	if scopeID == 0 || varName == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "scope_id and variable are required"})
	}
	rows, err := cfgmgmt.Matrix(ctrl.DB, scopeID, varName)
	if err != nil {
		return configWriteError(c, err)
	}
	def, err := func() (*models.ConfigVariableDef, error) {
		var d models.ConfigVariableDef
		if err := ctrl.DB.Where("name = ?", varName).First(&d).Error; err != nil {
			return nil, err
		}
		return &d, nil
	}()
	if err == nil && (def.Secret || def.Type == models.VarTypeSecret) {
		for i := range rows {
			if rows[i].Value != nil && rows[i].Error == "" {
				rows[i].Value = "***"
			}
		}
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiConfigServiceTypeList(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ServiceType, models.ServiceTypeDTO](ctrl.DB)
	return h.GetAll(c)
}

func (ctrl *Controller) ApiConfigServiceTypeGet(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ServiceType, models.ServiceTypeDTO](ctrl.DB)
	return h.GetOne(c)
}

func (ctrl *Controller) ApiConfigServiceTypeCreate(c *echo.Context) error {
	var dto models.ServiceTypeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	row := models.ServiceType{
		Name: dto.Name, Description: dto.Description,
		Schema: dto.Schema, EndpointRoles: dto.EndpointRoles,
		SyncSource: dto.SyncSource, NetboxType: dto.NetboxType,
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiConfigServiceTypeUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var existing models.ServiceType
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.ServiceTypeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if existing.Builtin && dto.Name != "" && dto.Name != existing.Name {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "cannot rename a built-in service type"})
	}
	if dto.Name != "" && !existing.Builtin {
		existing.Name = dto.Name
	}
	existing.Description = dto.Description
	existing.Schema = dto.Schema
	existing.EndpointRoles = dto.EndpointRoles
	existing.SyncSource = dto.SyncSource
	existing.NetboxType = dto.NetboxType
	if err := ctrl.DB.Save(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, existing)
}

func (ctrl *Controller) ApiConfigServiceTypeDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var existing models.ServiceType
	if err := ctrl.DB.First(&existing, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if existing.Builtin {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "cannot delete a built-in service type"})
	}
	if err := ctrl.DB.Delete(&existing).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiConfigPlatformPackList(c *echo.Context) error {
	var typeID uint
	_ = echo.QueryParamsBinder(c).Uint("service_type_id", &typeID).BindError()
	q := ctrl.DB.Model(&models.PlatformPack{})
	if typeID != 0 {
		q = q.Where("service_type_id = ?", typeID)
	}
	var rows []models.PlatformPack
	if err := q.Order("platform").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiConfigPlatformPackGet(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.PlatformPack, models.PlatformPackDTO](ctrl.DB)
	return h.GetOne(c)
}

func (ctrl *Controller) ApiConfigPlatformPackCreate(c *echo.Context) error {
	var dto models.PlatformPackDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.ServiceTypeID == 0 || dto.Platform == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service_type_id and platform are required"})
	}
	kind := dto.PayloadKind
	if kind == "" {
		kind = models.PayloadKindCLI
	}
	row := models.PlatformPack{
		ServiceTypeID: dto.ServiceTypeID, Platform: cfgmgmt.NormalizePlatform(dto.Platform),
		PayloadKind: kind, ApplyTemplate: dto.ApplyTemplate, CleanupTemplate: dto.CleanupTemplate,
	}
	if err := ctrl.DB.Create(&row).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiConfigPlatformPackUpdate(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.PlatformPack, models.PlatformPackDTO](ctrl.DB)
	return h.Update(c)
}

func (ctrl *Controller) ApiConfigPlatformPackDelete(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.PlatformPack, models.PlatformPackDTO](ctrl.DB)
	return h.Delete(c)
}

func (ctrl *Controller) ApiConfigMacroList(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigMacro, models.ConfigMacroDTO](ctrl.DB)
	return h.GetAll(c)
}

func (ctrl *Controller) ApiConfigMacroGet(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigMacro, models.ConfigMacroDTO](ctrl.DB)
	return h.GetOne(c)
}

func (ctrl *Controller) ApiConfigMacroCreate(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigMacro, models.ConfigMacroDTO](ctrl.DB)
	return h.Create(c)
}

func (ctrl *Controller) ApiConfigMacroUpdate(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigMacro, models.ConfigMacroDTO](ctrl.DB)
	return h.Update(c)
}

func (ctrl *Controller) ApiConfigMacroDelete(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigMacro, models.ConfigMacroDTO](ctrl.DB)
	return h.Delete(c)
}

func (ctrl *Controller) ApiConfigTemplateList(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigTemplate, models.ConfigTemplateDTO](ctrl.DB)
	return h.GetAll(c)
}

func (ctrl *Controller) ApiConfigTemplateGet(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigTemplate, models.ConfigTemplateDTO](ctrl.DB)
	return h.GetOne(c)
}

func (ctrl *Controller) ApiConfigTemplateCreate(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigTemplate, models.ConfigTemplateDTO](ctrl.DB)
	return h.Create(c)
}

func (ctrl *Controller) ApiConfigTemplateUpdate(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigTemplate, models.ConfigTemplateDTO](ctrl.DB)
	return h.Update(c)
}

func (ctrl *Controller) ApiConfigTemplateDelete(c *echo.Context) error {
	h := NewSecureCRUDHandler[models.ConfigTemplate, models.ConfigTemplateDTO](ctrl.DB)
	return h.Delete(c)
}

type configRenderRequest struct {
	DeviceID  uint `json:"device_id"`
	ServiceID uint `json:"service_id"`
}

func (ctrl *Controller) ApiConfigRender(c *echo.Context) error {
	var req configRenderRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if req.DeviceID == 0 && req.ServiceID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "device_id or service_id is required"})
	}
	if req.DeviceID != 0 {
		out, err := cfgmgmt.RenderDevice(ctrl.DB, req.DeviceID)
		if err != nil {
			return configWriteError(c, err)
		}
		return c.JSON(http.StatusOK, out)
	}
	sources, err := cfgmgmt.RenderService(ctrl.DB, req.ServiceID)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"sources": sources})
}

func (ctrl *Controller) ApiServiceEndpointsGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	rows, err := cfgmgmt.ListEndpoints(ctrl.DB, id)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

type serviceEndpointsBody struct {
	Fields    json.RawMessage             `json:"fields"`
	Endpoints []models.ServiceEndpointDTO `json:"endpoints"`
}

func (ctrl *Controller) ApiServiceEndpointsPut(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var svc models.Service
	if err := ctrl.DB.First(&svc, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	st, err := cfgmgmt.LookupServiceType(ctrl.DB, svc.ServiceType)
	if err != nil {
		return configWriteError(c, err)
	}
	var body serviceEndpointsBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	eps := make([]models.ServiceEndpoint, 0, len(body.Endpoints))
	for _, d := range body.Endpoints {
		eps = append(eps, models.ServiceEndpoint{
			Role: d.Role, DeviceID: d.DeviceID, InterfaceID: d.InterfaceID, Fields: d.Fields,
		})
	}
	if err := cfgmgmt.ValidateEndpoints(ctrl.DB, st, eps); err != nil {
		return configWriteError(c, err)
	}
	if svc.ServiceType == "ELINE" {
		if err := cfgmgmt.ValidateELINEShape(ctrl.DB, eps); err != nil {
			return configWriteError(c, err)
		}
		if err := ctrl.persistELINEEndpoints(c.Request().Context(), &svc, eps); err != nil {
			return elinePersistError(c, err)
		}
	} else {
		if err := cfgmgmt.ReplaceEndpoints(ctrl.DB, id, eps); err != nil {
			return configWriteError(c, err)
		}
	}
	if len(body.Fields) > 0 && string(body.Fields) != "null" {
		if err := ctrl.DB.Model(&models.Service{}).Where("id = ?", id).Update("fields", body.Fields).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}
	rows, err := cfgmgmt.ListEndpoints(ctrl.DB, id)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

// ApiServicePush renders the platform pack and applies CLI sessions.
func (ctrl *Controller) ApiServicePush(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var svc models.Service
	if err := ctrl.DB.First(&svc, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	return ctrl.apiServiceGenericPush(c, &svc)
}

func (ctrl *Controller) apiServiceGenericPush(c *echo.Context, svc *models.Service) error {
	var creds deviceCredentialsRequest
	if err := c.Bind(&creds); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	eps, err := cfgmgmt.ListEndpoints(ctrl.DB, svc.ID)
	if err != nil {
		return configWriteError(c, err)
	}
	if len(eps) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service has no endpoints"})
	}
	if svc.ServiceType == "ELINE" && svc.PseudowireID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service has not been provisioned yet"})
	}
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	order := []uint{}
	byDev := map[uint][]models.ServiceEndpoint{}
	currentDeviceIDs := map[uint]bool{}
	for _, ep := range eps {
		if _, ok := byDev[ep.DeviceID]; !ok {
			order = append(order, ep.DeviceID)
		}
		byDev[ep.DeviceID] = append(byDev[ep.DeviceID], ep)
		currentDeviceIDs[ep.DeviceID] = true
	}

	if svc.ServiceType == "ELINE" {
		seenAbandoned := map[uint]bool{}
		for _, id := range []uint{svc.AppliedEndpointADeviceID, svc.AppliedEndpointBDeviceID} {
			if id != 0 && !currentDeviceIDs[id] && !seenAbandoned[id] {
				seenAbandoned[id] = true
				order = append(order, id)
			}
		}
	}

	fetchIDs := append([]uint{}, order...)
	fetched, err := fetchDevices(c.Request().Context(), ctrl.DB, fetchIDs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	devicesByID := map[uint]models.Device{}
	for _, d := range fetched {
		devicesByID[d.ID] = d
	}

	results := []ApiServiceElinePushResult{}
	deviceOK := map[uint]bool{}
	abandonedErr := map[uint]string{}

	for _, deviceID := range order {
		device, ok := devicesByID[deviceID]
		if !ok {
			res := ApiServiceElinePushResult{
				Device: "device #" + strconv.FormatUint(uint64(deviceID), 10),
				Error:  "device not found",
			}
			if !currentDeviceIDs[deviceID] {
				res.Error = "abandoned ELINE endpoint device no longer exists in factum - stale config was not removed"
				abandonedErr[deviceID] = res.Error
			}
			results = append(results, res)
			continue
		}
		if !currentDeviceIDs[deviceID] {
			res := ctrl.removeELINEFromDevice(&device, creds, settings, &drivers.ELINERemoval{
				Name:               svc.ServiceID,
				StaleSubinterfaces: abandonedSubsForDevice(svc, deviceID),
			})
			abandonedErr[deviceID] = res.Error
			results = append(results, res)
			continue
		}

		pack, err := cfgmgmt.LookupPlatformPack(ctrl.DB, svc.ServiceType, device.Platform)
		if err != nil {
			results = append(results, ApiServiceElinePushResult{Device: device.Name, Error: err.Error()})
			continue
		}
		if err := cfgmgmt.RequireCLIPack(pack); err != nil {
			results = append(results, ApiServiceElinePushResult{Device: device.Name, Error: err.Error()})
			continue
		}
		if !isSupportedDriverPlatform(&device) {
			results = append(results, ApiServiceElinePushResult{
				Device: device.Name,
				Error:  "platform pack exists but this platform cannot apply CLI sessions yet",
			})
			continue
		}
		drv, err := ctrl.newDriverForDevice(&device, creds, settings)
		if err != nil {
			results = append(results, ApiServiceElinePushResult{Device: device.Name, Error: err.Error()})
			continue
		}
		applier, ok := drv.(drivers.CLISessionApplier)
		if !ok {
			results = append(results, ApiServiceElinePushResult{
				Device: device.Name,
				Error:  "platform pack exists but this platform cannot apply CLI sessions yet",
			})
			continue
		}

		var cmds []string
		var firstData *cfgmgmt.GenericRenderData
		label := device.Name
		pushErr := ""
		for i := range byDev[deviceID] {
			ep := &byDev[deviceID][i]
			var iface *models.Interface
			for j := range device.Interfaces {
				if device.Interfaces[j].ID == ep.InterfaceID {
					cp := device.Interfaces[j]
					iface = &cp
					break
				}
			}
			if iface != nil {
				label = device.Name + " " + iface.Name
			}
			data, err := cfgmgmt.GenericData(ctrl.DB, svc, ep, &device, iface)
			if err != nil {
				pushErr = err.Error()
				break
			}
			if firstData == nil {
				firstData = data
				cl, err := cfgmgmt.RenderPackCleanupIfPresent(ctrl.DB, pack, data)
				if err != nil {
					pushErr = err.Error()
					break
				}
				cmds = append(cmds, cl...)
			}
			body, err := cfgmgmt.RenderPackApplyBody(ctrl.DB, pack, data)
			if err != nil {
				pushErr = err.Error()
				break
			}
			cmds = append(cmds, body...)
		}
		if pushErr != "" {
			results = append(results, ApiServiceElinePushResult{Device: label, Error: pushErr})
			continue
		}
		if firstData != nil {
			if prep, ok := drv.(drivers.ELINEPrepareChecker); ok {
				if err := prep.PrepareELINEApply(elineIntentFromData(firstData)); err != nil {
					results = append(results, ApiServiceElinePushResult{Device: label, Error: err.Error()})
					continue
				}
			}
		}
		if err := applier.ApplyCLISession(svc.ServiceID, cmds); err != nil {
			results = append(results, ApiServiceElinePushResult{Device: label, Error: err.Error()})
			continue
		}
		deviceOK[deviceID] = true
		results = append(results, ApiServiceElinePushResult{Device: label})
	}

	if svc.ServiceType == "ELINE" {
		ctrl.stampELINEApplied(svc, eps, deviceOK, abandonedErr)
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}

func (ctrl *Controller) stampELINEApplied(svc *models.Service, eps []models.ServiceEndpoint, deviceOK map[uint]bool, abandonedErr map[uint]string) {
	updates := map[string]any{}
	for _, ep := range eps {
		if !deviceOK[ep.DeviceID] {
			continue
		}
		prev := uint(0)
		if ep.Role == "a" {
			prev = svc.AppliedEndpointADeviceID
		} else if ep.Role == "b" {
			prev = svc.AppliedEndpointBDeviceID
		}
		if prev != 0 && prev != ep.DeviceID && abandonedErr[prev] != "" {
			continue
		}
		var iface models.Interface
		if err := ctrl.DB.First(&iface, ep.InterfaceID).Error; err != nil {
			continue
		}
		vlan := cfgmgmt.VLANFromFields(ep.Fields)
		switch ep.Role {
		case "a":
			updates["applied_endpoint_a_device_id"] = ep.DeviceID
			updates["applied_endpoint_a_iface"] = iface.Name
			updates["applied_endpoint_a_vlan"] = vlan
		case "b":
			updates["applied_endpoint_b_device_id"] = ep.DeviceID
			updates["applied_endpoint_b_iface"] = iface.Name
			updates["applied_endpoint_b_vlan"] = vlan
		}
	}
	if len(updates) == 0 {
		return
	}
	_ = ctrl.DB.Model(&models.Service{}).Where("id = ?", svc.ID).Updates(updates).Error
}

func abandonedSubsForDevice(svc *models.Service, deviceID uint) []drivers.ELINEStaleSubinterface {
	var out []drivers.ELINEStaleSubinterface
	if svc.AppliedEndpointADeviceID == deviceID && svc.AppliedEndpointAIface != "" {
		out = append(out, drivers.ELINEStaleSubinterface{Iface: svc.AppliedEndpointAIface, VLAN: svc.AppliedEndpointAVlan})
	}
	if svc.AppliedEndpointBDeviceID == deviceID && svc.AppliedEndpointBIface != "" {
		out = append(out, drivers.ELINEStaleSubinterface{Iface: svc.AppliedEndpointBIface, VLAN: svc.AppliedEndpointBVlan})
	}
	return out
}

func elineIntentFromData(data *cfgmgmt.GenericRenderData) *drivers.ELINEIntent {
	if data == nil {
		return &drivers.ELINEIntent{}
	}
	intent := &drivers.ELINEIntent{
		Name:             data.Name,
		Description:      data.Description,
		LocalIface:       data.LocalIface,
		LocalVLAN:        data.LocalVLAN,
		PeerLocalIface:   data.PeerLocalIface,
		PeerLocalVLAN:    data.PeerLocalVLAN,
		ServiceNumericID: data.ServiceNumericID,
	}
	for _, s := range data.StaleSubinterfaces {
		intent.StaleSubinterfaces = append(intent.StaleSubinterfaces, drivers.ELINEStaleSubinterface{Iface: s.Iface, VLAN: s.VLAN})
	}
	if data.Remote != nil {
		intent.Remote = &drivers.ELINERemotePeer{
			NeighborIP:   data.Remote.NeighborIP,
			PseudowireID: data.Remote.PseudowireID,
			MTU:          data.Remote.MTU,
			ControlWord:  data.Remote.ControlWord,
			DeviceName:   data.Remote.DeviceName,
			RemoteIface:  data.Remote.RemoteIface,
			RemoteVLAN:   data.Remote.RemoteVLAN,
		}
	}
	return intent
}
