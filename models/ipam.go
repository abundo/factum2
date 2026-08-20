package models

// IPAM is a factum-native prefix inventory, independent of NetBox's
// ipam.IPAddress rows (models.Address). A namespace is one unique address
// space; VRFs carve that space without overlap; allowed prefixes bound
// what may be allocated.

// IpamNamespace is one unique address space. Two namespaces may both hold
// 10.0.0.0/8. Created with a default VRF; allowed prefixes (pools) are
// added separately.
type IpamNamespace struct {
	FactumModel
	Name        string `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	Description string `json:"description" gorm:"type:varchar(255)"`
}

func (IpamNamespace) TableName() string { return "ipam_namespaces" }

type IpamNamespaceDTO struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// IpamNamespacePrefix is an allowed pool for a namespace. 0.0.0.0/0 and
// ::/0 mean any prefix of that family. Prefix is stored masked and
// canonical (10.1.2.3/24 → 10.1.2.0/24).
type IpamNamespacePrefix struct {
	FactumModel
	NamespaceID uint   `json:"namespace_id" gorm:"uniqueIndex:idx_ipam_ns_pool;not null"`
	Prefix      string `json:"prefix" gorm:"uniqueIndex:idx_ipam_ns_pool;type:varchar(80);not null"`
	Family      int    `json:"family"`
}

func (IpamNamespacePrefix) TableName() string { return "ipam_namespace_prefixes" }

type IpamNamespacePrefixDTO struct {
	ID          uint   `json:"id"`
	NamespaceID uint   `json:"namespace_id"`
	Prefix      string `json:"prefix"`
}

// IpamVRF is a routing domain inside a namespace. Address space is unique
// across VRFs of the same namespace (not classic overlapping-VRF
// semantics): once a prefix is allocated to one VRF, no other VRF may
// take it or anything that overlaps it. The default VRF is created with
// the namespace and cannot be deleted.
type IpamVRF struct {
	FactumModel
	NamespaceID uint   `json:"namespace_id" gorm:"uniqueIndex:idx_ipam_vrf_ns_name;not null"`
	Name        string `json:"name" gorm:"uniqueIndex:idx_ipam_vrf_ns_name;not null;type:varchar(255)"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	IsDefault   bool   `json:"is_default"`
}

func (IpamVRF) TableName() string { return "ipam_vrfs" }

type IpamVRFIDTO struct {
	ID          uint   `json:"id"`
	NamespaceID uint   `json:"namespace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// IpamPrefix is a CIDR allocated to exactly one VRF in a namespace.
// Parent/child relationships are computed from containment, not stored.
type IpamPrefix struct {
	FactumModel
	NamespaceID uint   `json:"namespace_id" gorm:"uniqueIndex:idx_ipam_alloc_ns_pfx;not null"`
	VRFID       uint   `json:"vrf_id" gorm:"index;not null"`
	Prefix      string `json:"prefix" gorm:"uniqueIndex:idx_ipam_alloc_ns_pfx;type:varchar(80);not null"`
	Family      int    `json:"family"`
	Description string `json:"description" gorm:"type:varchar(255)"`
}

func (IpamPrefix) TableName() string { return "ipam_prefixes" }

type IpamPrefixDTO struct {
	ID          uint   `json:"id"`
	NamespaceID uint   `json:"namespace_id"`
	VRFID       uint   `json:"vrf_id"`
	Prefix      string `json:"prefix"`
	Description string `json:"description"`
}
