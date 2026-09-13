package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

const maxConnectionTypeImage = 512 * 1024

func configWriteError(c *echo.Context, err error) error {
	if se := cfgmgmt.AsStatusError(err); se != nil {
		return c.JSON(se.Status, map[string]any{"error": se.Message})
	}
	if cfgmgmt.IsUniqueViolation(err) {
		return c.JSON(http.StatusConflict, map[string]any{"error": "already exists"})
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
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

func scopeFromDTO(dto *models.ConfigScopeDTO) models.ConfigScope {
	row := models.ConfigScope{
		ParentID: dto.ParentID, SiteID: dto.SiteID,
		DeviceID: dto.DeviceID, InterfaceID: dto.InterfaceID,
		ServiceID: dto.ServiceID, ServiceTypeID: dto.ServiceTypeID,
	}
	if dto.Name != nil {
		row.Name = *dto.Name
	}
	if dto.Kind != nil {
		row.Kind = *dto.Kind
	}
	if dto.Platform != nil {
		row.Platform = *dto.Platform
	}
	if dto.PayloadKind != nil {
		row.PayloadKind = *dto.PayloadKind
	}
	if dto.SortOrder != nil {
		row.SortOrder = *dto.SortOrder
	}
	if dto.Payload != nil {
		row.Payload = *dto.Payload
	}
	return row
}

func (ctrl *Controller) ApiConfigScopeCreate(c *echo.Context) error {
	var dto models.ConfigScopeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	kind := ""
	if dto.Kind != nil {
		kind = *dto.Kind
	}
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
	if dto.DeviceID != nil && kind == models.ConfigScopeKindDevice {
		node, err := cfgmgmt.AttachDevice(ctrl.DB, parent, *dto.DeviceID)
		if err != nil {
			return configWriteError(c, err)
		}
		return c.JSON(http.StatusCreated, node)
	}
	if kind == models.ConfigScopeKindService {
		if dto.Attach != nil && dto.ServiceID != nil && *dto.ServiceID != 0 {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "attach and service_id are mutually exclusive"})
		}
		if dto.Attach != nil {
			node, err := cfgmgmt.CreateServiceFromTree(ctrl.DB, parent, dto.Attach)
			if err != nil {
				return configWriteError(c, err)
			}
			return c.JSON(http.StatusCreated, node)
		}
		if dto.ServiceID != nil && *dto.ServiceID != 0 {
			node, err := cfgmgmt.AttachService(ctrl.DB, parent, *dto.ServiceID)
			if err != nil {
				return configWriteError(c, err)
			}
			return c.JSON(http.StatusCreated, node)
		}
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service_id or attach is required"})
	}
	row := scopeFromDTO(&dto)
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
	updated, err := cfgmgmt.UpdateScope(ctrl.DB, id, &dto)
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

func (ctrl *Controller) ApiConfigScopeMove(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var req models.MoveScopeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	updated, err := cfgmgmt.MoveScope(ctrl.DB, id, req.ParentID, req.SortOrder)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, updated)
}

func (ctrl *Controller) ApiConfigScopeDetach(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	if err := cfgmgmt.DetachDevice(ctrl.DB, id); err != nil {
		return configWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiConfigFeatureList(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	if _, err := cfgmgmt.GetScope(ctrl.DB, id); err != nil {
		return configWriteError(c, err)
	}
	rows, err := cfgmgmt.ListCLIFeatures(ctrl.DB, id)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiConfigFeatureCreate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var dto models.ConfigCLIFeatureDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row := models.ConfigCLIFeature{
		Name:           dto.Name,
		SortOrder:      dto.SortOrder,
		AddCommands:    dto.AddCommands,
		UpdateCommands: dto.UpdateCommands,
		RemoveCommands: dto.RemoveCommands,
		RemoveAtRoot:   dto.RemoveAtRoot,
	}
	created, err := cfgmgmt.CreateCLIFeature(ctrl.DB, id, &row)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusCreated, created)
}

func (ctrl *Controller) ApiConfigFeatureUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var dto models.ConfigCLIFeatureDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	updated, err := cfgmgmt.UpdateCLIFeature(ctrl.DB, id, &dto)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, updated)
}

func (ctrl *Controller) ApiConfigFeatureDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	if err := cfgmgmt.DeleteCLIFeature(ctrl.DB, id); err != nil {
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
	rows := []models.ConfigAssignment{*row}
	if err := cfgmgmt.RedactAssignmentSecrets(ctrl.DB, rows); err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows[0])
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

func (ctrl *Controller) ApiConfigResourcesFree(c *echo.Context) error {
	var interfaceID, deviceID uint
	var name string
	var family int
	_ = echo.QueryParamsBinder(c).
		Uint("interface_id", &interfaceID).
		Uint("device_id", &deviceID).
		String("name", &name).
		Int("family", &family).
		BindError()
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	out, err := cfgmgmt.AllocateResource(ctrl.DB, interfaceID, deviceID, name, family)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, out)
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
	rows, err := cfgmgmt.ListServiceTypes(ctrl.DB)
	if err != nil {
		return configWriteError(c, err)
	}
	out := make([]models.ServiceTypeDTO, len(rows))
	for i := range rows {
		out[i] = cfgmgmt.ServiceTypeDTO(&rows[i])
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiConfigServiceTypeGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	row, err := cfgmgmt.LoadServiceType(ctrl.DB, id)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, cfgmgmt.ServiceTypeDTO(row))
}

func bindServiceTypeDTO(c *echo.Context) (*models.ServiceTypeDTO, error) {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return nil, err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, err
	}
	if _, ok := probe["endpoint_roles"]; ok {
		return nil, errors.New("endpoint_roles is not supported; use interfaces")
	}
	var dto models.ServiceTypeDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (ctrl *Controller) ApiConfigServiceTypeCreate(c *echo.Context) error {
	dto, err := bindServiceTypeDTO(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	row := models.ServiceType{
		Name: dto.Name, Description: dto.Description,
		Schema: dto.Schema, Interfaces: dto.Interfaces,
		SyncSource: dto.SyncSource, NetboxType: dto.NetboxType,
	}
	if err := cfgmgmt.ValidateServiceType(&row); err != nil {
		return configWriteError(c, err)
	}
	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&models.ServiceType{}).Where("name = ?", row.Name).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return cfgmgmt.ErrServiceTypeNameTaken
		}
		if err := tx.Create(&row).Error; err != nil {
			if cfgmgmt.IsUniqueViolation(err) {
				return cfgmgmt.ErrServiceTypeNameTaken
			}
			return err
		}
		if err := cfgmgmt.ReplaceConnectionTypes(tx, row.ID, dto.ConnectionTypes); err != nil {
			return err
		}
		_, err := cfgmgmt.CatalogCLITypeFolder(tx, row.Name)
		return err
	}); err != nil {
		return configWriteError(c, err)
	}
	saved, err := cfgmgmt.LoadServiceType(ctrl.DB, row.ID)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusCreated, cfgmgmt.ServiceTypeDTO(saved))
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
	dto, err := bindServiceTypeDTO(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	oldName := existing.Name
	newName := oldName
	if dto.Name != "" {
		newName = dto.Name
	}
	existing.Description = dto.Description
	existing.Schema = dto.Schema
	existing.Interfaces = dto.Interfaces
	existing.SyncSource = dto.SyncSource
	existing.NetboxType = dto.NetboxType
	if err := cfgmgmt.ValidateServiceType(&existing); err != nil {
		return configWriteError(c, err)
	}
	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if newName != oldName {
			var n int64
			if err := tx.Model(&models.ServiceType{}).Where("name = ? AND id <> ?", newName, existing.ID).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				return cfgmgmt.ErrServiceTypeNameTaken
			}
			if err := tx.Model(&models.Service{}).Where("service_type = ?", oldName).
				Update("service_type", newName).Error; err != nil {
				return err
			}
			if err := cfgmgmt.RenameCatalogCLITypeFolder(tx, oldName, newName); err != nil {
				return err
			}
			existing.Name = newName
		}
		if err := tx.Save(&existing).Error; err != nil {
			if cfgmgmt.IsUniqueViolation(err) {
				return cfgmgmt.ErrServiceTypeNameTaken
			}
			return err
		}
		if err := cfgmgmt.ReplaceConnectionTypes(tx, existing.ID, dto.ConnectionTypes); err != nil {
			return err
		}
		_, err := cfgmgmt.CatalogCLITypeFolder(tx, existing.Name)
		return err
	}); err != nil {
		return configWriteError(c, err)
	}
	saved, err := cfgmgmt.LoadServiceType(ctrl.DB, existing.ID)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, cfgmgmt.ServiceTypeDTO(saved))
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
	var n int64
	if err := ctrl.DB.Model(&models.Service{}).Where("service_type = ?", existing.Name).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusConflict, map[string]any{"error": "service type is in use"})
	}
	if err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := cfgmgmt.DeleteTranslationCLIForType(tx, existing.ID, existing.Name); err != nil {
			return err
		}
		if err := tx.Where("service_type_id = ?", existing.ID).Delete(&models.ServiceConnectionType{}).Error; err != nil {
			return err
		}
		return tx.Delete(&existing).Error
	}); err != nil {
		return configWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) loadConnectionType(typeID, ctID uint) (*models.ServiceConnectionType, error) {
	var row models.ServiceConnectionType
	if err := ctrl.DB.Where("id = ? AND service_type_id = ?", ctID, typeID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (ctrl *Controller) ApiConfigConnectionTypeImageGet(c *echo.Context) error {
	typeID, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	ctID, err := echo.PathParam[uint](c, "ctid")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	row, err := ctrl.loadConnectionType(typeID, ctID)
	if err != nil {
		return configWriteError(c, err)
	}
	if len(row.Image) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "image not found"})
	}
	ct := row.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, ct, row.Image)
}

func (ctrl *Controller) ApiConfigConnectionTypeImagePut(c *echo.Context) error {
	typeID, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	ctID, err := echo.PathParam[uint](c, "ctid")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	row, err := ctrl.loadConnectionType(typeID, ctID)
	if err != nil {
		return configWriteError(c, err)
	}
	r := http.MaxBytesReader(c.Response(), c.Request().Body, maxConnectionTypeImage)
	data, err := io.ReadAll(r)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || errors.Is(err, io.ErrUnexpectedEOF) {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{"error": "image exceeds 512KiB"})
		}
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "failed to read image"})
	}
	if len(data) == 0 {
		row.Image = nil
		row.ContentType = ""
		if err := ctrl.DB.Select("Image", "ContentType").Save(row).Error; err != nil {
			return configWriteError(c, err)
		}
		return c.NoContent(http.StatusNoContent)
	}
	contentType, err := detectConnectionTypeImage(data, c.Request().Header.Get(echo.HeaderContentType))
	if err != nil {
		return configWriteError(c, err)
	}
	row.Image = data
	row.ContentType = contentType
	if err := ctrl.DB.Select("Image", "ContentType").Save(row).Error; err != nil {
		return configWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	riffMagic = []byte("RIFF")
	webpMagic = []byte("WEBP")
)

func detectConnectionTypeImage(data []byte, declared string) (string, error) {
	declared = strings.ToLower(strings.TrimSpace(declared))
	if i := strings.Index(declared, ";"); i >= 0 {
		declared = strings.TrimSpace(declared[:i])
	}
	if strings.Contains(declared, "svg") || looksLikeSVG(data) {
		return "", &cfgmgmt.StatusError{Status: http.StatusBadRequest, Message: "SVG images are not allowed"}
	}
	var detected string
	switch {
	case bytes.HasPrefix(data, pngMagic):
		detected = "image/png"
	case isWebP(data):
		detected = "image/webp"
	default:
		return "", &cfgmgmt.StatusError{Status: http.StatusBadRequest, Message: "image must be PNG or WebP"}
	}
	if declared != "" && declared != "application/octet-stream" && declared != detected {
		return "", &cfgmgmt.StatusError{Status: http.StatusBadRequest, Message: "content type does not match image data"}
	}
	return detected, nil
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && bytes.HasPrefix(data, riffMagic) && bytes.Equal(data[8:12], webpMagic)
}

func looksLikeSVG(data []byte) bool {
	s := strings.TrimSpace(string(data))
	if len(s) > 256 {
		s = s[:256]
	}
	ls := strings.ToLower(s)
	return strings.Contains(ls, "<svg") || strings.HasPrefix(ls, "<?xml")
}

func (ctrl *Controller) ApiConfigLegacyGone(c *echo.Context) error {
	return c.JSON(http.StatusGone, map[string]any{
		"error": "platform packs and config templates have been replaced by CLI objects",
	})
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

type configRenderEndpoint struct {
	Role        string          `json:"role"`
	DeviceID    uint            `json:"device_id"`
	InterfaceID uint            `json:"interface_id"`
	Fields      json.RawMessage `json:"fields"`
}

type configRenderRequest struct {
	DeviceID  uint                    `json:"device_id"`
	ServiceID uint                    `json:"service_id"`
	Endpoints *[]configRenderEndpoint `json:"endpoints"`
	Fields    json.RawMessage         `json:"fields"`
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
	if req.Endpoints != nil {
		eps := make([]models.ServiceEndpoint, 0, len(*req.Endpoints))
		for _, ep := range *req.Endpoints {
			eps = append(eps, models.ServiceEndpoint{
				ServiceID: req.ServiceID, Role: ep.Role,
				DeviceID: ep.DeviceID, InterfaceID: ep.InterfaceID, Fields: ep.Fields,
			})
		}
		sources, err := cfgmgmt.RenderServiceEndpoints(ctrl.DB, req.ServiceID, eps, req.Fields)
		if err != nil {
			return configWriteError(c, err)
		}
		return c.JSON(http.StatusOK, map[string]any{"sources": sources})
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
		role := d.Role
		if role == "" {
			role = models.EndpointRoleInterface
		}
		eps = append(eps, models.ServiceEndpoint{
			Role: role, DeviceID: d.DeviceID, InterfaceID: d.InterfaceID, Fields: d.Fields,
		})
	}
	if err := cfgmgmt.ValidateEndpoints(ctrl.DB, st, eps); err != nil {
		return configWriteError(c, err)
	}
	var canonFields json.RawMessage
	if len(body.Fields) > 0 && string(body.Fields) != "null" {
		canon, err := cfgmgmt.ValidateServiceFields(st, body.Fields)
		if err != nil {
			return configWriteError(c, err)
		}
		canonFields = canon
	}
	if err := cfgmgmt.ReplaceEndpoints(ctrl.DB, id, eps); err != nil {
		return configWriteError(c, err)
	}
	if canonFields != nil {
		if err := ctrl.DB.Model(&models.Service{}).Where("id = ?", id).Update("fields", canonFields).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}
	rows, err := cfgmgmt.ListEndpoints(ctrl.DB, id)
	if err != nil {
		return configWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

// ApiServicePush renders the translation CLI object and applies CLI sessions.
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
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	order := []uint{}
	byDev := map[uint][]models.ServiceEndpoint{}
	for _, ep := range eps {
		if _, ok := byDev[ep.DeviceID]; !ok {
			order = append(order, ep.DeviceID)
		}
		byDev[ep.DeviceID] = append(byDev[ep.DeviceID], ep)
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

	for _, deviceID := range order {
		device, ok := devicesByID[deviceID]
		if !ok {
			results = append(results, ApiServiceElinePushResult{
				Device: "device #" + strconv.FormatUint(uint64(deviceID), 10),
				Error:  "device not found",
			})
			continue
		}

		cliObj, err := cfgmgmt.LookupCLIObject(ctrl.DB, svc.ServiceType, device.Platform)
		if err != nil {
			results = append(results, ApiServiceElinePushResult{Device: device.Name, Error: err.Error()})
			continue
		}
		if cliObj == nil {
			results = append(results, ApiServiceElinePushResult{
				Device: device.Name,
				Error:  cfgmgmt.MissingCLIObjectMessage(svc.ServiceType, device.Platform),
			})
			continue
		}
		if err := cfgmgmt.RequireCLIObject(cliObj); err != nil {
			results = append(results, ApiServiceElinePushResult{Device: device.Name, Error: err.Error()})
			continue
		}
		cannotApply := "CLI object exists but this platform cannot apply CLI sessions yet"
		if !isSupportedDriverPlatform(&device) {
			results = append(results, ApiServiceElinePushResult{
				Device: device.Name,
				Error:  cannotApply,
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
				Error:  cannotApply,
			})
			continue
		}

		var cmds []string
		label := device.Name
		pushErr := ""
		cleanupDone := false
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
			part, err := cfgmgmt.RenderCLITranslation(ctrl.DB, cliObj, data, !cleanupDone)
			if err != nil {
				pushErr = err.Error()
				break
			}
			cmds = append(cmds, part...)
			cleanupDone = true
		}
		if pushErr != "" {
			results = append(results, ApiServiceElinePushResult{Device: label, Error: pushErr})
			continue
		}
		if err := applier.ApplyCLISession(svc.ServiceID, cmds); err != nil {
			results = append(results, ApiServiceElinePushResult{Device: label, Error: err.Error()})
			continue
		}
		results = append(results, ApiServiceElinePushResult{Device: label})
	}

	return c.JSON(http.StatusOK, map[string]any{"results": results})
}
