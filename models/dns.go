package models

// DNS zone editor tables are all prefixed dns_ so they never collide with
// other factum resources (or with a future generic "zones" table).

const (
	DnsZoneTypeForward  = "forward"
	DnsZoneTypeReverse4 = "reverse4"
	DnsZoneTypeReverse6 = "reverse6"
)

// DnsSOATemplate is a named SOA used by DNS templates when dnsmgr2 writes
// zone files. Serial is not stored — dnsmgr2 assigns YYYYMMDDnn itself.
type DnsSOATemplate struct {
	FactumModel
	Name    string `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	Mname   string `json:"mname" gorm:"type:varchar(255)"`
	Rname   string `json:"rname" gorm:"type:varchar(255)"`
	Refresh uint   `json:"refresh"`
	Retry   uint   `json:"retry"`
	Expire  uint   `json:"expire"`
	TTL     uint   `json:"ttl"` // SOA minimum
}

func (DnsSOATemplate) TableName() string { return "dns_soa_templates" }

type DnsSOATemplateDTO struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Mname   string `json:"mname"`
	Rname   string `json:"rname"`
	Refresh uint   `json:"refresh"`
	Retry   uint   `json:"retry"`
	Expire  uint   `json:"expire"`
	TTL     uint   `json:"ttl"`
}

// DnsDNSSECPolicy is a BIND dnssec-policy (name, KSK/ZSK, signature timings).
// factum2-dns writes every policy into the zone include. dnsmgr2 emits the
// block in named.conf and points each zone at the policy named by its DNS
// template. The built-in BIND policy "default" cannot be redefined.
type DnsDNSSECPolicy struct {
	FactumModel
	Name                     string `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	KSKLifetime              string `json:"ksk_lifetime" gorm:"type:varchar(64)"`
	KSKAlgorithm             string `json:"ksk_algorithm" gorm:"type:varchar(64)"`
	ZSKLifetime              string `json:"zsk_lifetime" gorm:"type:varchar(64)"`
	ZSKAlgorithm             string `json:"zsk_algorithm" gorm:"type:varchar(64)"`
	PurgeKeys                string `json:"purge_keys" gorm:"type:varchar(64)"`
	SignaturesValidity       string `json:"signatures_validity" gorm:"type:varchar(64)"`
	SignaturesValidityDNSKEY string `json:"signatures_validity_dnskey" gorm:"column:signatures_validity_dnskey;type:varchar(64)"`
	SignaturesRefresh        string `json:"signatures_refresh" gorm:"type:varchar(64)"`
}

func (DnsDNSSECPolicy) TableName() string { return "dns_dnssec_policies" }

type DnsDNSSECPolicyDTO struct {
	ID                       uint   `json:"id"`
	Name                     string `json:"name"`
	KSKLifetime              string `json:"ksk_lifetime"`
	KSKAlgorithm             string `json:"ksk_algorithm"`
	ZSKLifetime              string `json:"zsk_lifetime"`
	ZSKAlgorithm             string `json:"zsk_algorithm"`
	PurgeKeys                string `json:"purge_keys"`
	SignaturesValidity       string `json:"signatures_validity"`
	SignaturesValidityDNSKEY string `json:"signatures_validity_dnskey"`
	SignaturesRefresh        string `json:"signatures_refresh"`
}

// DnsTemplate is a zone template (dns_template in dnsmgr2): SOA, default TTL,
// optional DNSSEC policy, and the NS list written into every zone that uses it.
// Each nameserver row is one hostname plus an optional IPv4 or IPv6 address.
// The same hostname may appear on several rows when it has more than one address.
type DnsTemplate struct {
	FactumModel
	Name           string                  `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	SOATemplateID  uint                    `json:"soa_template_id"`
	SOATemplate    DnsSOATemplate          `json:"soa_template,omitempty" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	DefaultTTL     uint                    `json:"default_ttl"`
	DNSSECPolicyID *uint                   `json:"dnssec_policy_id"`
	DNSSECPolicy   *DnsDNSSECPolicy        `json:"dnssec_policy,omitempty" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	Nameservers    []DnsTemplateNameserver `json:"nameservers" gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (DnsTemplate) TableName() string { return "dns_templates" }

// DnsTemplateNameserver is one NS hostname on a DNS template, in display order.
// Address is optional and is either an IPv4 or an IPv6 address, never both.
// Another row with the same hostname carries another address.
type DnsTemplateNameserver struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	DnsTemplateID uint   `json:"dns_template_id" gorm:"not null;uniqueIndex:idx_dns_template_ns_rank"`
	Rank          uint   `json:"rank" gorm:"not null;uniqueIndex:idx_dns_template_ns_rank"`
	Hostname      string `json:"hostname" gorm:"type:varchar(255);not null"`
	Address       string `json:"address" gorm:"type:varchar(64);not null;default:''"`
}

func (DnsTemplateNameserver) TableName() string { return "dns_template_nameservers" }

type DnsTemplateDTO struct {
	ID             uint                       `json:"id"`
	Name           string                     `json:"name"`
	SOATemplateID  uint                       `json:"soa_template_id"`
	SOATemplate    string                     `json:"soa_template"`
	DefaultTTL     uint                       `json:"default_ttl"`
	DNSSECPolicyID *uint                      `json:"dnssec_policy_id"`
	DNSSECPolicy   string                     `json:"dnssec_policy"`
	Nameservers    []DnsTemplateNameserverDTO `json:"nameservers"`
}

// DnsTemplateNameserverDTO is one nameserver row on the DNS template API.
// Address is empty, or a single IPv4 or IPv6 address.
type DnsTemplateNameserverDTO struct {
	Hostname string `json:"hostname"`
	Address  string `json:"address,omitempty"`
}

// DnsZone is one managed zone. Type is forward / reverse4 / reverse6, matching
// dnsmgr2. Records are extra RRs. SOA, apex NS, and in-zone nameserver
// addresses come from DnsTemplate.
type DnsZone struct {
	FactumModel
	Name          string          `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	Type          string          `json:"type" gorm:"type:varchar(16);not null;default:'forward'"`
	DnsTemplateID uint            `json:"dns_template_id"`
	DnsTemplate   DnsTemplate     `json:"dns_template,omitempty" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	Comment       string          `json:"comment" gorm:"type:text"`
	Records       []DnsZoneRecord `json:"records" gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (DnsZone) TableName() string { return "dns_zones" }

// DnsZoneRecord is one DNS resource record in a zone.
// Rank is the display / records-file order (0-based).
// TTL is seconds; nil means unspecified (use the template default). A stored 0
// is treated as unspecified — DNS TTL 0 is not used.
// Description is an optional operator note; it is not part of the DNS RDATA.
// MAC is a DHCP host reservation (A/AAAA only). It is its own column, not a
// comment; factum2-dns writes it as a `mac` field on the JSON A/AAAA record
// so dnsmgr2 can emit a Kea reservation.
type DnsZoneRecord struct {
	ID          uint   `json:"id" gorm:"primaryKey"`
	DnsZoneID   uint   `json:"dns_zone_id" gorm:"not null;uniqueIndex:idx_dns_zone_record_rank"`
	Rank        uint   `json:"rank" gorm:"not null;uniqueIndex:idx_dns_zone_record_rank"`
	Name        string `json:"name" gorm:"type:varchar(255);not null"`
	TTL         *uint  `json:"ttl"`
	Type        string `json:"type" gorm:"column:record_type;type:varchar(16);not null"`
	Value       string `json:"value" gorm:"type:text;not null"`
	Description string `json:"description" gorm:"type:text"`
	MAC         string `json:"mac" gorm:"type:varchar(32)"`
}

func (DnsZoneRecord) TableName() string { return "dns_zone_records" }

type DnsZoneRecordDTO struct {
	Name        string `json:"name"`
	TTL         *uint  `json:"ttl"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description"`
	MAC         string `json:"mac"`
}

type DnsZoneDTO struct {
	ID            uint               `json:"id"`
	Name          string             `json:"name"`
	Type          string             `json:"type"`
	DnsTemplateID uint               `json:"dns_template_id"`
	DnsTemplate   string             `json:"dns_template"`
	Comment       string             `json:"comment"`
	Records       []DnsZoneRecordDTO `json:"records,omitempty"`
}
