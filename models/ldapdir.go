package models

// LdapOU is one folder in the internal directory. People and roles live
// in a folder; folders nest. A nil ParentID is a folder under the base DN.
type LdapOU struct {
	FactumModel
	Name     string `gorm:"not null;type:varchar(64)" json:"name"`
	ParentID *uint  `gorm:"index" json:"parent_id"`
}

// RoleMember records that MemberRoleID is a direct member of RoleID.
// Membership is expanded when the directory is served, the same way
// Active Directory expands nested groups.
type RoleMember struct {
	RoleID       uint `gorm:"primaryKey" json:"role_id"`
	MemberRoleID uint `gorm:"primaryKey" json:"member_role_id"`
}

func (RoleMember) TableName() string { return "role_members" }
