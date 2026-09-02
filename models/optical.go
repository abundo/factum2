package models

import "time"

// Optical chassis kinds. Transponder vs muxponder is not a device kind —
// it is derived from tributary xconnect cardinality on a line port.
const (
	OpticalKindNone     = ""
	OpticalKindROADM    = "roadm"
	OpticalKindWDMShelf = "wdm_shelf"
	OpticalKindILA      = "ila"
	OpticalKindPassive  = "passive"
)

// OpticalKindAliases maps legacy / operator-facing names onto stored kinds.
var OpticalKindAliases = map[string]string{
	"transponder": OpticalKindWDMShelf,
	"muxponder":   OpticalKindWDMShelf,
}

// AllowedOpticalKinds is the stored set (aliases are normalized first).
var AllowedOpticalKinds = map[string]bool{
	OpticalKindROADM:    true,
	OpticalKindWDMShelf: true,
	OpticalKindILA:      true,
	OpticalKindPassive:  true,
}

func IsOpticalKind(kind string) bool {
	return AllowedOpticalKinds[kind]
}

// Optical port roles.
const (
	PortTXPClient    = "txp_client"
	PortTXPLine      = "txp_line"
	PortROADMAddDrop = "roadm_adddrop"
	PortROADMDegree  = "roadm_degree"
	PortFiber        = "fiber_port"
)

// AllowedOpticalPortRoles is the stored set for OpticalPort.Role / the
// NetBox optical_role custom field on dcim.interface.
var AllowedOpticalPortRoles = map[string]bool{
	PortTXPClient:    true,
	PortTXPLine:      true,
	PortROADMAddDrop: true,
	PortROADMDegree:  true,
	PortFiber:        true,
}

// Optical xconnect kinds.
const (
	XCTributary   = "tributary"
	XCAddDrop     = "roadm_adddrop"
	XCExpress     = "roadm_express"
	XCPassthrough = "passthrough"

	// XCSourceOpenROADM marks xconnects written by optical inventory sync.
	// Operator-created rows keep Source empty; sync only deletes its own.
	XCSourceOpenROADM = "openroadm"
)

// Service path / hop kinds and statuses.
const (
	HopInterface  = "interface"
	HopDevice     = "device"
	HopConnection = "connection"
	HopXConnect   = "xconnect"

	PathNone       = "none"
	PathComplete   = "complete"
	PathIncomplete = "incomplete"
	PathStale      = "stale"
	PathConflict   = "conflict"

	TraceModeWDM   = "wdm"
	TraceModeFiber = "fiber"

	StartCustomerPort = "customer_port"
	StartTXPClient    = "txp_client"
	StartFiberPort    = "fiber_port"
)

// Maintenance constants.
const (
	MaintResourceConnection = "connection"
	MaintResourceDevice     = "device"
	MaintResourceInterface  = "interface"

	MaintDraft      = "draft"
	MaintPlanned    = "planned"
	MaintNotified   = "notified"
	MaintInProgress = "in_progress"
	MaintCompleted  = "completed"
	MaintCancelled  = "cancelled"

	MaintNotifyPending = "pending"
	MaintNotifySent    = "sent"
	MaintNotifyFailed  = "failed"
	MaintNotifySkipped = "skipped"
)

// OpticalKindMap maps a NetBox device role display name (lowercased) to a
// Factum optical kind. Admin-editable.
type OpticalKindMap struct {
	FactumModel
	NetboxRoleName string `json:"netbox_role_name" gorm:"uniqueIndex;type:varchar(255);not null"`
	OpticalKind    string `json:"optical_kind" gorm:"type:varchar(32);not null"`
}

type OpticalKindMapDTO struct {
	ID             uint   `json:"id"`
	NetboxRoleName string `json:"netbox_role_name"`
	OpticalKind    string `json:"optical_kind"`
}

// OpticalPort is Factum-owned optical metadata for a NetBox-synced
// interface. Separate table so syncInterfaces UpdateAll cannot wipe it.
type OpticalPort struct {
	FactumModel
	InterfaceID uint   `json:"interface_id" gorm:"uniqueIndex;not null"`
	Role        string `json:"role" gorm:"type:varchar(32);not null;index"`
	FreqHz      uint64 `json:"freq_hz"`
	ITUChannel  *int   `json:"itu_channel"`
	Notes       string `json:"notes" gorm:"type:varchar(255)"`
}

// OpticalXConnect is one intra-device optical adjacency.
type OpticalXConnect struct {
	FactumModel
	DeviceID     uint   `json:"device_id" gorm:"index;not null"`
	Kind         string `json:"kind" gorm:"type:varchar(32);not null;index"`
	InterfaceAID uint   `json:"interface_a_id" gorm:"index;not null"`
	InterfaceBID uint   `json:"interface_b_id" gorm:"index;not null"`
	FreqHz       uint64 `json:"freq_hz"`
	Source       string `json:"source" gorm:"type:varchar(32);index"`
}

// ServicePath is the attached optical/fiber path for one VL/VI/LF/LI service.
type ServicePath struct {
	FactumModel
	ServiceID            uint         `json:"service_id" gorm:"uniqueIndex;not null"`
	Mode                 string       `json:"mode" gorm:"type:varchar(16);not null"`
	Status               string       `json:"status" gorm:"type:varchar(16);not null;index"`
	EndpointAInterfaceID uint         `json:"endpoint_a_interface_id" gorm:"index;not null"`
	EndpointZInterfaceID uint         `json:"endpoint_z_interface_id" gorm:"index;not null"`
	StartKindA           string       `json:"start_kind_a" gorm:"type:varchar(32)"`
	StartKindZ           string       `json:"start_kind_z" gorm:"type:varchar(32)"`
	FreqHz               uint64       `json:"freq_hz"`
	LastTracedAt         *time.Time   `json:"last_traced_at"`
	LastTraceError       string       `json:"last_trace_error" gorm:"type:text"`
	Hops                 []ServiceHop `json:"hops,omitempty" gorm:"foreignKey:ServiceID;references:ServiceID"`
}

// ServiceHop is one row of a materialized path. Exactly one Kind / FK.
type ServiceHop struct {
	FactumModel
	ServiceID    uint   `json:"service_id" gorm:"index;not null"`
	Seq          int    `json:"seq" gorm:"not null"`
	Kind         string `json:"kind" gorm:"type:varchar(16);not null;index"`
	InterfaceID  *uint  `json:"interface_id" gorm:"index"`
	ConnectionID *uint  `json:"connection_id" gorm:"index"`
	XConnectID   *uint  `json:"xconnect_id" gorm:"index"`
	DeviceID     *uint  `json:"device_id" gorm:"index"`
	FreqHz       uint64 `json:"freq_hz"`
	Label        string `json:"label" gorm:"type:varchar(255)"`
}

// MaintenanceWindow is a scheduled outage on a connection, device, or interface.
type MaintenanceWindow struct {
	FactumModel
	Title        string    `json:"title" gorm:"type:varchar(255);not null"`
	Description  string    `json:"description" gorm:"type:text"`
	ResourceType string    `json:"resource_type" gorm:"type:varchar(16);not null;index"`
	ResourceID   uint      `json:"resource_id" gorm:"index;not null"`
	StartsAt     time.Time `json:"starts_at" gorm:"index;not null"`
	EndsAt       time.Time `json:"ends_at"`
	Status       string    `json:"status" gorm:"type:varchar(16);not null;index"`
	CreatedBy    uint      `json:"created_by"`
}

// MaintenanceNotification is one recipient row for a window.
type MaintenanceNotification struct {
	FactumModel
	WindowID   uint       `json:"window_id" gorm:"index;not null"`
	CustomerID uint       `json:"customer_id" gorm:"index;not null"`
	ServiceIDs []uint     `json:"service_ids" gorm:"serializer:json"`
	ContactID  *uint      `json:"contact_id"`
	Email      string     `json:"email"`
	SentAt     *time.Time `json:"sent_at"`
	Status     string     `json:"status" gorm:"type:varchar(16)"`
	Error      string     `json:"error" gorm:"type:text"`
}

// CustomerContact joins a contact to a customer for maintenance notify.
type CustomerContact struct {
	FactumModel
	CustomerID uint `json:"customer_id" gorm:"uniqueIndex:idx_customer_contacts;not null"`
	ContactID  uint `json:"contact_id" gorm:"uniqueIndex:idx_customer_contacts;not null;index"`
}
