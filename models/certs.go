package models

const (
	CertChallengeKindDNS01 = "dns-01"
	CertProviderRFC2136    = "rfc2136"
)

// CertAccount is a lego ACME account (email, server, key type).
type CertAccount struct {
	FactumModel
	Name                  string `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	Email                 string `json:"email" gorm:"type:varchar(255)"`
	Server                string `json:"server" gorm:"type:varchar(512)"`
	KeyType               string `json:"key_type" gorm:"type:varchar(32)"`
	AcceptsTermsOfService bool   `json:"accepts_terms_of_service"`
	EABKID                string `json:"eab_kid" gorm:"column:eab_kid;type:varchar(255)"`
	EABHMACKey            string `json:"eab_hmac_key" gorm:"column:eab_hmac_key;type:text"`
}

func (CertAccount) TableName() string { return "cert_accounts" }

type CertAccountDTO struct {
	ID                    uint   `json:"id"`
	Name                  string `json:"name"`
	Email                 string `json:"email"`
	Server                string `json:"server"`
	KeyType               string `json:"key_type"`
	AcceptsTermsOfService bool   `json:"accepts_terms_of_service"`
	EABKID                string `json:"eab_kid"`
	EABHMACKey            string `json:"eab_hmac_key"`
}

// CertChallenge is a named DNS-01 solver, currently RFC2136 (dynamic update).
type CertChallenge struct {
	FactumModel
	Name               string `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	Kind               string `json:"kind" gorm:"type:varchar(32);not null;default:'dns-01'"`
	Provider           string `json:"provider" gorm:"type:varchar(64);not null;default:'rfc2136'"`
	DNSTimeout         int    `json:"dns_timeout"`
	Resolvers          string `json:"resolvers" gorm:"type:text"`
	DisableAuthNS      bool   `json:"disable_authoritative_nameservers" gorm:"column:disable_authoritative_nameservers"`
	DisableRecursiveNS bool   `json:"disable_recursive_nameservers" gorm:"column:disable_recursive_nameservers"`
	PropagationWait    string `json:"propagation_wait" gorm:"type:varchar(32)"`

	RFC2136Nameserver   string `json:"rfc2136_nameserver" gorm:"type:varchar(255)"`
	RFC2136TSIGAlgo     string `json:"rfc2136_tsig_algorithm" gorm:"column:rfc2136_tsig_algorithm;type:varchar(128)"`
	RFC2136TSIGKey      string `json:"rfc2136_tsig_key" gorm:"type:varchar(255)"`
	RFC2136TSIGSecret   string `json:"rfc2136_tsig_secret" gorm:"type:text"`
	RFC2136TSIGFile     string `json:"rfc2136_tsig_file" gorm:"type:varchar(512)"`
	RFC2136TTL          int    `json:"rfc2136_ttl"`
	RFC2136PropTimeout  string `json:"rfc2136_propagation_timeout" gorm:"column:rfc2136_propagation_timeout;type:varchar(32)"`
	RFC2136PollInterval string `json:"rfc2136_polling_interval" gorm:"column:rfc2136_polling_interval;type:varchar(32)"`

	// ExtraEnv is KEY=value lines appended to the challenge dotenv.
	ExtraEnv string `json:"extra_env" gorm:"type:text"`
}

func (CertChallenge) TableName() string { return "cert_challenges" }

type CertChallengeDTO struct {
	ID                  uint   `json:"id"`
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	Provider            string `json:"provider"`
	DNSTimeout          int    `json:"dns_timeout"`
	Resolvers           string `json:"resolvers"`
	DisableAuthNS       bool   `json:"disable_authoritative_nameservers"`
	DisableRecursiveNS  bool   `json:"disable_recursive_nameservers"`
	PropagationWait     string `json:"propagation_wait"`
	RFC2136Nameserver   string `json:"rfc2136_nameserver"`
	RFC2136TSIGAlgo     string `json:"rfc2136_tsig_algorithm"`
	RFC2136TSIGKey      string `json:"rfc2136_tsig_key"`
	RFC2136TSIGSecret   string `json:"rfc2136_tsig_secret"`
	RFC2136TSIGFile     string `json:"rfc2136_tsig_file"`
	RFC2136TTL          int    `json:"rfc2136_ttl"`
	RFC2136PropTimeout  string `json:"rfc2136_propagation_timeout"`
	RFC2136PollInterval string `json:"rfc2136_polling_interval"`
	ExtraEnv            string `json:"extra_env"`
}

// Certificate is one lego certificate: name, domains, account, challenge.
// KeyType and EnableCommonName nil/empty inherit Settings.CertsDefault*.
type Certificate struct {
	FactumModel
	Name             string        `json:"name" gorm:"uniqueIndex;not null;type:varchar(255)"`
	AccountID        uint          `json:"account_id"`
	Account          CertAccount   `json:"account,omitempty" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	ChallengeID      uint          `json:"challenge_id"`
	Challenge        CertChallenge `json:"challenge,omitempty" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	KeyType          string        `json:"key_type" gorm:"type:varchar(32)"`
	EnableCommonName *bool         `json:"enable_common_name"`
	// Host is the IPv4/IPv6/hostname Icinga connects to when checking
	// this certificate. Empty skips Icinga cert checks for this cert.
	Host    string              `json:"host" gorm:"type:varchar(255)"`
	Domains []CertificateDomain `json:"domains" gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (Certificate) TableName() string { return "certificates" }

type CertificateDomain struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	CertificateID uint   `json:"certificate_id" gorm:"not null;uniqueIndex:idx_cert_domain_rank"`
	Rank          uint   `json:"rank" gorm:"not null;uniqueIndex:idx_cert_domain_rank"`
	Name          string `json:"name" gorm:"type:varchar(255);not null"`
}

func (CertificateDomain) TableName() string { return "certificate_domains" }

type CertificateDTO struct {
	ID               uint     `json:"id"`
	Name             string   `json:"name"`
	AccountID        uint     `json:"account_id"`
	Account          string   `json:"account"`
	ChallengeID      uint     `json:"challenge_id"`
	Challenge        string   `json:"challenge"`
	KeyType          string   `json:"key_type"`
	EnableCommonName *bool    `json:"enable_common_name"`
	Host             string   `json:"host"`
	Domains          []string `json:"domains"`
}
