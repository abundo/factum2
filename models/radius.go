package models

import "time"

// RadiusClient is one router or switch allowed to send RADIUS
// Access-Request. Address is the source IP of the packet. Secret is the
// shared secret and is not included in list responses.
type RadiusClient struct {
	FactumModel
	Name    string `gorm:"type:varchar(255);not null" json:"name"`
	Address string `gorm:"type:varchar(64);not null;uniqueIndex" json:"address"`
	Secret  string `gorm:"type:text;not null" json:"-"`
	Enabled bool   `json:"enabled"`
}

// RadiusPolicy maps one LDAP group DN to NetBox device roles. AllDevices
// permits every device, which is the administrator group. Roles is a
// newline-separated list of role names when AllDevices is false.
type RadiusPolicy struct {
	FactumModel
	GroupDN    string `gorm:"column:group_dn;type:varchar(512);not null;uniqueIndex" json:"group_dn"`
	AllDevices bool   `gorm:"column:all_devices" json:"all_devices"`
	Roles      string `gorm:"type:text" json:"roles"`
}

// RadiusEvent is one login attempt reported by a RADIUS worker. The
// password is never stored.
type RadiusEvent struct {
	FactumModel
	ReportedAt time.Time `json:"reported_at"`
	Username   string    `json:"username" gorm:"type:varchar(255);index"`
	NASIP      string    `json:"nas_ip" gorm:"column:nas_ip;type:varchar(64)"`
	DeviceName string    `json:"device_name" gorm:"type:varchar(255)"`
	DeviceRole string    `json:"device_role" gorm:"type:varchar(255)"`
	Result     string    `json:"result" gorm:"type:varchar(16);index"`
	Reason     string    `json:"reason" gorm:"type:varchar(255)"`
	Worker     string    `json:"worker" gorm:"type:varchar(255)"`
}
