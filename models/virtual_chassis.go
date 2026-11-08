package models

// A NetBox virtual chassis groups physical devices. For a ROADM, the
// chassis is the ROADM: the NetBox master is the shelf controller and
// every other member is one degree (ordered by VCPosition).
type VirtualChassis struct {
	FactumModel
	NetboxID uint   `json:"netbox_id" gorm:"uniqueIndex"`
	Name     string `json:"name" gorm:"type:varchar(255)"`
	Domain   string `json:"domain" gorm:"type:varchar(255)"`
	// MasterNetboxID is the NetBox dcim.device id of the chassis master.
	// 0 when NetBox has no master.
	MasterNetboxID uint `json:"master_netbox_id"`
	// MasterDeviceID is the Factum device id of that master, 0 when the
	// master device is not in Factum yet.
	MasterDeviceID uint `json:"master_device_id" gorm:"index"`
}

const (
	// VCMemberController is the NetBox virtual-chassis master.
	VCMemberController = "controller"
	// VCMemberDegree is every other member of the chassis.
	VCMemberDegree = "degree"
)

// VirtualChassisMember is one device in a chassis, for the device API.
type VirtualChassisMember struct {
	DeviceID uint   `json:"device_id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Master   bool   `json:"master"`
	Member   string `json:"member"`
}

// VirtualChassisRef is the chassis a device belongs to. Filled when
// devices are loaded for the API. Not a database column.
type VirtualChassisRef struct {
	ID       uint                   `json:"id"`
	NetboxID uint                   `json:"netbox_id"`
	Name     string                 `json:"name"`
	Domain   string                 `json:"domain,omitempty"`
	Master   bool                   `json:"master"`
	Member   string                 `json:"member"`
	Members  []VirtualChassisMember `json:"members,omitempty"`
}
