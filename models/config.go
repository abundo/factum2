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
	ConfigScopeKindResource        = "resource"
	// ConfigScopeKindServiceRef is virtual: ScopeTree injects it, it is not stored.
	ConfigScopeKindServiceRef = "service_ref"

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
	ID            uint                `json:"id"`
	ParentID      *uint               `json:"parent_id"`
	Name          *string             `json:"name"`
	Kind          *string             `json:"kind"`
	SiteID        *uint               `json:"site_id"`
	DeviceID      *uint               `json:"device_id"`
	InterfaceID   *uint               `json:"interface_id"`
	ServiceID     *uint               `json:"service_id"`
	ServiceTypeID *uint               `json:"service_type_id"`
	Platform      *string             `json:"platform"`
	PayloadKind   *string             `json:"payload_kind"`
	Enabled       *bool               `json:"enabled"`
	SortOrder     *int                `json:"sort_order"`
	Payload       *ConfigScopePayload `json:"payload"`
	// Attach creates a new CN/CI inventory row plus a canonical service
	// node with zero endpoints. Mutually exclusive with ServiceID (attach existing).
	Attach *ServiceDTO `json:"attach,omitempty"`
}

// MoveScopeRequest is POST /api/config/scopes/:id/move. SortOrder nil = last sibling.
type MoveScopeRequest struct {
	ParentID  uint `json:"parent_id"`
	SortOrder *int `json:"sort_order"`
}

// ConfigScopePayload is kind-specific data stored as JSON on ConfigScope.
type ConfigScopePayload struct {
	Description string         `json:"description,omitempty"`
	Platforms   []string       `json:"platforms,omitempty"`
	Context     *CLIContext    `json:"context,omitempty"`
	Role        string         `json:"role,omitempty"`
	Fields      map[string]any `json:"fields,omitempty"`
	// CIDRs is the prefix pool for kind=resource (canonical Masked strings).
	CIDRs []string `json:"cidrs,omitempty"`
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

// EndpointRoleInterface is the sentinel stored in ServiceEndpoint.Role.
const EndpointRoleInterface = "interface"

const (
	FieldTypeString     = VarTypeString
	FieldTypeInt        = VarTypeInt
	FieldTypeBool       = VarTypeBool
	FieldTypeEnum       = VarTypeEnum
	FieldTypeVLAN       = VarTypeVLAN
	FieldTypeMAC        = "mac"
	FieldTypeSNPA       = "snpa" // alias of mac; stored as mac
	FieldTypeIPv4       = "ipv4"
	FieldTypeIPv6       = "ipv6"
	FieldTypeIP         = VarTypeIP
	FieldTypeIPv4Prefix = "ipv4_prefix"
	FieldTypeIPv6Prefix = "ipv6_prefix"
	FieldTypePrefix     = VarTypePrefix
	FieldTypeServiceID  = "service_id"
	FieldTypeList       = VarTypeList
)

type EnumChoice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// FieldSchema is one typed field on a service type or interfaces spec.
// Nested Items is the same shape; list items are nameless.
type FieldSchema struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`

	// Inclusive bounds for int/vlan values and list length.
	// VLAN defaults unset Min/Max to 1 and 4094 and clamps custom bounds to that range; int is unbounded.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Unit string   `json:"unit,omitempty"`

	BoolTrueLabel  string `json:"bool_true_label,omitempty"`
	BoolFalseLabel string `json:"bool_false_label,omitempty"`

	Enum []EnumChoice `json:"enum,omitempty"`

	Items *FieldSchema `json:"items,omitempty"`

	// Resource names a kind=resource pool. Prefix-typed nodes only, including items.
	Resource string `json:"resource,omitempty"`
}

// ServiceInterfacesSpec is the homogeneous UNI spec for a service type.
// Max == 0 means unlimited.
type ServiceInterfacesSpec struct {
	Min    int           `json:"min"`
	Max    int           `json:"max"`
	Unique bool          `json:"unique"`
	Fields []FieldSchema `json:"fields"`
}

// ServiceType is a vendor-agnostic service class (ELINE, ELAN, …).
type ServiceType struct {
	FactumModel
	Name        string                `json:"name" gorm:"uniqueIndex;not null;type:varchar(64)"`
	Description string                `json:"description" gorm:"type:varchar(255)"`
	Schema      []FieldSchema         `json:"schema" gorm:"serializer:json"`
	Interfaces  ServiceInterfacesSpec `json:"interfaces" gorm:"serializer:json"`
	Builtin     bool                  `json:"builtin"`
	// SyncSource names the on-device collection device-sync reads
	// (eline / elan / l3vpn). Empty means the type is GUI-only.
	SyncSource string `json:"sync_source" gorm:"type:varchar(32)"`
	// NetboxType is the NetBox object to upsert for SyncSource
	// (evpl / vpls / vrf).
	NetboxType      string                  `json:"netbox_type" gorm:"type:varchar(32)"`
	ConnectionTypes []ServiceConnectionType `json:"connection_types" gorm:"foreignKey:ServiceTypeID"`
}

func (ServiceType) TableName() string { return "service_types" }

type ServiceTypeDTO struct {
	ID              uint                       `json:"id"`
	Name            string                     `json:"name"`
	Description     string                     `json:"description"`
	Schema          []FieldSchema              `json:"schema"`
	Interfaces      ServiceInterfacesSpec      `json:"interfaces"`
	SyncSource      string                     `json:"sync_source"`
	NetboxType      string                     `json:"netbox_type"`
	ConnectionTypes []ServiceConnectionTypeDTO `json:"connection_types"`
}

// ServiceConnectionType is a named connection choice on a definition.
type ServiceConnectionType struct {
	FactumModel
	ServiceTypeID uint   `json:"service_type_id" gorm:"uniqueIndex:idx_svc_ct_type_name;not null"`
	Name          string `json:"name" gorm:"uniqueIndex:idx_svc_ct_type_name;not null;type:varchar(64)"`
	SortOrder     int    `json:"sort_order"`
	Image         []byte `json:"-" gorm:"type:bytea"`
	ContentType   string `json:"content_type" gorm:"type:varchar(64)"`
}

func (ServiceConnectionType) TableName() string { return "service_connection_types" }

type ServiceConnectionTypeDTO struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	SortOrder   int    `json:"sort_order"`
	ContentType string `json:"content_type,omitempty"`
	HasImage    bool   `json:"has_image"`
	ImageURL    string `json:"image_url,omitempty"`
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

// ServiceEndpoint is a service termination. Role is always "interface".
type ServiceEndpoint struct {
	FactumModel
	ServiceID   uint            `json:"service_id" gorm:"index;not null"`
	Role        string          `json:"role" gorm:"type:varchar(64);not null"`
	DeviceID    uint            `json:"device_id" gorm:"index;not null"`
	InterfaceID uint            `json:"interface_id" gorm:"index;not null"`
	Fields      json.RawMessage `json:"fields" gorm:"serializer:json"`
	// Applied* is the last successful push snapshot for this binding.
	AppliedDeviceID uint            `json:"-"`
	AppliedIface    string          `json:"-"`
	AppliedPlatform string          `json:"-"`
	AppliedFields   json.RawMessage `json:"-" gorm:"serializer:json"`
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
