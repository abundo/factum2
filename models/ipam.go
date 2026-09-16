package models

import "gorm.io/gorm"

const (
	VRFSourceFactum = "factum"
	VRFSourceNetbox = "netbox"
)

// IPAM is a factum-native prefix inventory, independent of NetBox's
// ipam.IPAddress rows (models.Address). A namespace is one unique address
// space; VRFs carve that space without overlap; allowed prefixes bound
// what may be allocated.

// IpamNamespace is one unique address space. Two namespaces may both hold
// 10.0.0.0/8. Created with a default VRF; allowed prefixes (pools) are
// added separately. The empty-name namespace is implicit: its prefixes
// and extra VRFs appear at the forest root so operators who do not use
// namespaces can allocate there directly.
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
	// RD is the BGP route distinguisher (e.g. "65000:1").
	RD string `json:"rd" gorm:"type:varchar(255)"`
	// ImportRT / ExportRT are route-targets, comma-separated when there
	// are several (NetBox import_targets / export_targets).
	ImportRT string `json:"import_rt" gorm:"type:text"`
	ExportRT string `json:"export_rt" gorm:"type:text"`
	// Source is "netbox" when upserted from sync, "factum" when created
	// in the UI. NetBox-synced rows are read-only.
	Source   string `json:"source" gorm:"type:varchar(32)"`
	NetboxID uint   `json:"netbox_id"`
}

func (IpamVRF) TableName() string { return "ipam_vrfs" }

func (v *IpamVRF) BeforeCreate(tx *gorm.DB) error {
	if v.Source == "" {
		if v.NetboxID != 0 {
			v.Source = VRFSourceNetbox
		} else {
			v.Source = VRFSourceFactum
		}
	}
	return nil
}

func (v IpamVRF) IsLocal() bool {
	return v.Source != VRFSourceNetbox
}

type IpamVRFIDTO struct {
	ID          uint   `json:"id"`
	NamespaceID uint   `json:"namespace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RD          string `json:"rd"`
	ImportRT    string `json:"import_rt"`
	ExportRT    string `json:"export_rt"`
}

// IpamPrefix is a CIDR allocated to exactly one VRF in a namespace.
// Parent/child relationships are computed from containment, not stored.
// DHCP fields are used when Settings.DhcpEnabled is on; turning that
// flag off only hides the UI — values stay on the row.
type IpamPrefix struct {
	FactumModel
	NamespaceID uint   `json:"namespace_id" gorm:"uniqueIndex:idx_ipam_alloc_ns_pfx;not null"`
	VRFID       uint   `json:"vrf_id" gorm:"index;not null"`
	Prefix      string `json:"prefix" gorm:"uniqueIndex:idx_ipam_alloc_ns_pfx;type:varchar(80);not null"`
	Family      int    `json:"family"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	// DhcpEnabled is the per-prefix "run a DHCP server here" checkbox.
	DhcpEnabled bool `json:"dhcp_enabled"`
	// DhcpRangeStart/End are the dynamic pool. Both empty is static-only.
	// When set they must sit inside Prefix.
	DhcpRangeStart string `json:"dhcp_range_start" gorm:"type:varchar(80)"`
	DhcpRangeEnd   string `json:"dhcp_range_end" gorm:"type:varchar(80)"`
	// DhcpGateway empty means "first usable address in the prefix"
	// (network + 1 for IPv4 /30 or shorter).
	DhcpGateway string `json:"dhcp_gateway" gorm:"type:varchar(80)"`
	// DhcpDnsServers is newline-separated. Empty means use the global
	// default (Settings.DhcpDnsServers).
	DhcpDnsServers string `json:"dhcp_dns_servers" gorm:"type:text"`
}

func (IpamPrefix) TableName() string { return "ipam_prefixes" }

type IpamPrefixDTO struct {
	ID             uint   `json:"id"`
	NamespaceID    uint   `json:"namespace_id"`
	VRFID          uint   `json:"vrf_id"`
	Prefix         string `json:"prefix"`
	Description    string `json:"description"`
	DhcpEnabled    bool   `json:"dhcp_enabled"`
	DhcpRangeStart string `json:"dhcp_range_start"`
	DhcpRangeEnd   string `json:"dhcp_range_end"`
	DhcpGateway    string `json:"dhcp_gateway"`
	DhcpDnsServers string `json:"dhcp_dns_servers"`
}
