package models

import (
	"encoding/json"
)

const (
	ConfigScopeKindFolder          = "folder"
	ConfigScopeKindSite            = "site"
	ConfigScopeKindLocation        = "location"
	ConfigScopeKindDevice          = "device"
	ConfigScopeKindInterface       = "interface"
	ConfigScopeKindParameter       = "parameter"
	ConfigScopeKindCLI             = "cli"
	ConfigScopeKindService         = "service"
	ConfigScopeKindServiceEndpoint = "service_endpoint"

	ConfigRootName            = "global"
	ConfigCatalogName         = "_catalog"
	ConfigCatalogCLIName      = "cli"
	ConfigServicesFolderName  = "_services"
	ConfigParametersChildName = "parameters"

	VarTypeString       = "string"
	VarTypeInt          = "int"
	VarTypeBool         = "bool"
	VarTypeEnum         = "enum"
	VarTypeIP           = "ip"
	VarTypePrefix       = "prefix"
	VarTypeVLAN         = "vlan"
	VarTypeInterfaceRef = "interface_ref"
	VarTypeSecret       = "secret"
	// List is a JSON array. Type entries with constraints.items
	// ("ip" or {"type":"int","min":1}); min/max are length.
	VarTypeList = "list"
	// Map is a JSON object (hash/dictionary). Type keys and values with
	// constraints.keys and constraints.values; min/max are size.
	VarTypeMap = "map"

	PayloadKindCLI      = "cli"
	PayloadKindNETCONF  = "netconf"
	PayloadKindRESTCONF = "restconf"

	// SyncSource* is which parsed DeviceConfig collection device-sync
	// reads for a service type. Empty means device-sync ignores the type.
	SyncSourceELINE = "eline"
	SyncSourceELAN  = "elan"
	SyncSourceL3VPN = "l3vpn"

	// NetboxType* is the NetBox object kind device-sync upserts for a
	// SyncSource. L2VPN sources use the L2VPN type slug; L3VPN uses VRF.
	NetboxTypeEVPL = "evpl"
	NetboxTypeVPLS = "vpls"
	NetboxTypeVRF  = "vrf"
)

// ConfigScope is one node in an arbitrary configuration hierarchy.
type ConfigScope struct {
	FactumModel
	ParentID      *uint              `json:"parent_id"`
	Name          string             `json:"name" gorm:"not null;type:varchar(255)"`
	Kind          string             `json:"kind" gorm:"not null;type:varchar(32)"`
	SiteID        *uint              `json:"site_id" gorm:"index"`
	DeviceID      *uint              `json:"device_id" gorm:"index"`
	InterfaceID   *uint              `json:"interface_id" gorm:"index"`
	ServiceID     *uint              `json:"service_id" gorm:"index"`
	ServiceTypeID *uint              `json:"service_type_id" gorm:"index"`
	Platform      string             `json:"platform" gorm:"type:varchar(64)"`
	PayloadKind   string             `json:"payload_kind" gorm:"type:varchar(32)"`
	Enabled       bool               `json:"enabled" gorm:"not null;default:true"`
	SortOrder     int                `json:"sort_order"`
	Payload       ConfigScopePayload `json:"payload" gorm:"serializer:json"`
	SeedChecksum  string             `json:"-" gorm:"type:varchar(64)"`
}

func (ConfigScope) TableName() string { return "config_scopes" }

type ConfigScopeDTO struct {
	ID            uint               `json:"id"`
	ParentID      *uint              `json:"parent_id"`
	Name          string             `json:"name"`
	Kind          string             `json:"kind"`
	SiteID        *uint              `json:"site_id"`
	DeviceID      *uint              `json:"device_id"`
	InterfaceID   *uint              `json:"interface_id"`
	ServiceID     *uint              `json:"service_id"`
	ServiceTypeID *uint              `json:"service_type_id"`
	Platform      string             `json:"platform"`
	PayloadKind   string             `json:"payload_kind"`
	Enabled       bool               `json:"enabled"`
	SortOrder     int                `json:"sort_order"`
	Payload       ConfigScopePayload `json:"payload"`
}

// ConfigScopePayload is kind-specific data stored as JSON on ConfigScope.
type ConfigScopePayload struct {
	Description string      `json:"description,omitempty"`
	Platforms   []string    `json:"platforms,omitempty"`
	Context     *CLIContext `json:"context,omitempty"`
}

// CLIContext is the optional CLI mode wrapping for a kind=cli object.
type CLIContext struct {
	Pattern  string            `json:"pattern"`
	Enter    string            `json:"enter"`
	Exit     string            `json:"exit"`
	Captures map[string]string `json:"captures,omitempty"`
}

// ConfigCLIFeature is one ordered command-blob set on a kind=cli scope.
type ConfigCLIFeature struct {
	FactumModel
	ScopeID        uint   `json:"scope_id" gorm:"uniqueIndex:idx_cfg_feat_scope_name;not null"`
	Name           string `json:"name" gorm:"uniqueIndex:idx_cfg_feat_scope_name;not null;type:varchar(128)"`
	SortOrder      int    `json:"sort_order"`
	AddCommands    string `json:"add_commands" gorm:"type:text"`
	UpdateCommands string `json:"update_commands" gorm:"type:text"`
	RemoveCommands string `json:"remove_commands" gorm:"type:text"`
	RemoveAtRoot   bool   `json:"remove_at_root"`
}

func (ConfigCLIFeature) TableName() string { return "config_cli_features" }

type ConfigCLIFeatureDTO struct {
	ID             uint   `json:"id"`
	ScopeID        uint   `json:"scope_id"`
	Name           string `json:"name"`
	SortOrder      int    `json:"sort_order"`
	AddCommands    string `json:"add_commands"`
	UpdateCommands string `json:"update_commands"`
	RemoveCommands string `json:"remove_commands"`
	RemoveAtRoot   bool   `json:"remove_at_root"`
}

// ConfigVariableDef is a typed variable that can be assigned on any scope.
type ConfigVariableDef struct {
	FactumModel
	Name         string          `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	Type         string          `json:"type" gorm:"not null;type:varchar(32)"`
	Description  string          `json:"description" gorm:"type:varchar(255)"`
	DefaultValue json.RawMessage `json:"default_value" gorm:"serializer:json"`
	Constraints  json.RawMessage `json:"constraints" gorm:"serializer:json"`
	Secret       bool            `json:"secret"`
	Required     bool            `json:"required"`
	Platforms    json.RawMessage `json:"platforms" gorm:"serializer:json"`
}

func (ConfigVariableDef) TableName() string { return "config_variable_defs" }

type ConfigVariableDefDTO struct {
	ID           uint            `json:"id"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Description  string          `json:"description"`
	DefaultValue json.RawMessage `json:"default_value"`
	Constraints  json.RawMessage `json:"constraints"`
	Secret       bool            `json:"secret"`
	Required     bool            `json:"required"`
	Platforms    json.RawMessage `json:"platforms"`
}

// ConfigAssignment binds a variable def to a value at one scope.
type ConfigAssignment struct {
	FactumModel
	VariableDefID uint            `json:"variable_def_id" gorm:"uniqueIndex:idx_cfg_assign_var_scope;not null"`
	ScopeID       uint            `json:"scope_id" gorm:"uniqueIndex:idx_cfg_assign_var_scope;not null"`
	Value         json.RawMessage `json:"value" gorm:"serializer:json"`
}

func (ConfigAssignment) TableName() string { return "config_assignments" }

type ConfigAssignmentDTO struct {
	ID            uint            `json:"id"`
	VariableDefID uint            `json:"variable_def_id"`
	ScopeID       uint            `json:"scope_id"`
	Value         json.RawMessage `json:"value"`
}

// Well-known ServiceType.Schema field names that are also copied onto
// dedicated Service columns so list views and older API clients can read
// them without parsing Fields.
const (
	SchemaFieldBandwidthMbps   = "bandwidth_mbps"
	SchemaFieldMaxMacAddresses = "max_mac_addresses"
)

// FieldSchema is one typed field on a service type or endpoint role.
type FieldSchema struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// EndpointRole describes how many endpoints of a role a service may have.
type EndpointRole struct {
	Name   string        `json:"name"`
	Min    int           `json:"min"`
	Max    int           `json:"max"`
	Fields []FieldSchema `json:"fields"`
}

// ServiceType is a vendor-agnostic service class (ELINE, ELAN, …).
type ServiceType struct {
	FactumModel
	Name          string         `json:"name" gorm:"uniqueIndex;not null;type:varchar(64)"`
	Description   string         `json:"description" gorm:"type:varchar(255)"`
	Schema        []FieldSchema  `json:"schema" gorm:"serializer:json"`
	EndpointRoles []EndpointRole `json:"endpoint_roles" gorm:"serializer:json"`
	Builtin       bool           `json:"builtin"`
	// SyncSource names the on-device collection device-sync reads
	// (eline / elan / l3vpn). Empty means the type is GUI-only.
	SyncSource string `json:"sync_source" gorm:"type:varchar(32)"`
	// NetboxType is the NetBox object to upsert for SyncSource
	// (evpl / vpls / vrf).
	NetboxType string `json:"netbox_type" gorm:"type:varchar(32)"`
}

func (ServiceType) TableName() string { return "service_types" }

type ServiceTypeDTO struct {
	ID            uint           `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Schema        []FieldSchema  `json:"schema"`
	EndpointRoles []EndpointRole `json:"endpoint_roles"`
	SyncSource    string         `json:"sync_source"`
	NetboxType    string         `json:"netbox_type"`
}

// PlatformPack is one platform's apply/cleanup templates for a service type.
type PlatformPack struct {
	FactumModel
	ServiceTypeID   uint   `json:"service_type_id" gorm:"uniqueIndex:idx_cfg_pack_type_plat;not null"`
	Platform        string `json:"platform" gorm:"uniqueIndex:idx_cfg_pack_type_plat;not null;type:varchar(64)"`
	PayloadKind     string `json:"payload_kind" gorm:"type:varchar(32)"`
	ApplyTemplate   string `json:"apply_template" gorm:"type:text"`
	CleanupTemplate string `json:"cleanup_template" gorm:"type:text"`
	// SeedChecksum is sha256 of the embed body last written by Seed. Empty
	// or matching the stored body means the row is untouched and may be
	// refreshed; a mismatch means an operator edited it.
	SeedChecksum string `json:"-" gorm:"type:varchar(64)"`
}

func (PlatformPack) TableName() string { return "platform_packs" }

type PlatformPackDTO struct {
	ID              uint   `json:"id"`
	ServiceTypeID   uint   `json:"service_type_id"`
	Platform        string `json:"platform"`
	PayloadKind     string `json:"payload_kind"`
	ApplyTemplate   string `json:"apply_template"`
	CleanupTemplate string `json:"cleanup_template"`
}

// ConfigTemplate is a baseline/golden snippet attached to a scope.
type ConfigTemplate struct {
	FactumModel
	Name        string `json:"name" gorm:"not null;type:varchar(255)"`
	Platform    string `json:"platform" gorm:"type:varchar(64)"`
	PayloadKind string `json:"payload_kind" gorm:"type:varchar(32)"`
	Body        string `json:"body" gorm:"type:text"`
	ScopeID     *uint  `json:"scope_id"`
	Enabled     bool   `json:"enabled"`
}

func (ConfigTemplate) TableName() string { return "config_templates" }

type ConfigTemplateDTO struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	PayloadKind string `json:"payload_kind"`
	Body        string `json:"body"`
	ScopeID     *uint  `json:"scope_id"`
	Enabled     bool   `json:"enabled"`
}

// ConfigMacro is a named snippet templates can {{include}}.
type ConfigMacro struct {
	FactumModel
	Name string `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	Body string `json:"body" gorm:"type:text"`
}

func (ConfigMacro) TableName() string { return "config_macros" }

type ConfigMacroDTO struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Body string `json:"body"`
}

// ServiceEndpoint is a service termination (including ELINE a/b).
type ServiceEndpoint struct {
	FactumModel
	ServiceID   uint            `json:"service_id" gorm:"index;not null"`
	Role        string          `json:"role" gorm:"type:varchar(64);not null"`
	DeviceID    uint            `json:"device_id" gorm:"index;not null"`
	InterfaceID uint            `json:"interface_id" gorm:"index;not null"`
	Fields      json.RawMessage `json:"fields" gorm:"serializer:json"`
}

func (ServiceEndpoint) TableName() string { return "service_endpoints" }

type ServiceEndpointDTO struct {
	ID          uint            `json:"id"`
	ServiceID   uint            `json:"service_id"`
	Role        string          `json:"role"`
	DeviceID    uint            `json:"device_id"`
	InterfaceID uint            `json:"interface_id"`
	Fields      json.RawMessage `json:"fields"`
}
