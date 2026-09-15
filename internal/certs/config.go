package certs

import "github.com/abundo/factum2/internal/util"

// Config is what factum2-certs fetches from the primary and writes as
// .lego.yaml plus dotenv, then runs lego.
type Config struct {
	util.CommonConfig
	LegoYaml                string      `json:"lego_yaml"`
	EnvFile                 string      `json:"env_file"`
	LegoBin                 string      `json:"lego_bin"`
	LegoStorage             string      `json:"lego_storage"`
	DefaultKeyType          string      `json:"default_key_type"`
	DefaultEnableCommonName bool        `json:"default_enable_common_name"`
	Accounts                []Account   `json:"accounts"`
	Challenges              []Challenge `json:"challenges"`
	Certificates            []Cert      `json:"certificates"`
}

type Account struct {
	Name                  string `json:"name"`
	Email                 string `json:"email"`
	Server                string `json:"server"`
	KeyType               string `json:"key_type"`
	AcceptsTermsOfService bool   `json:"accepts_terms_of_service"`
	EABKID                string `json:"eab_kid"`
	EABHMACKey            string `json:"eab_hmac_key"`
}

type Challenge struct {
	Name                string   `json:"name"`
	Kind                string   `json:"kind"`
	Provider            string   `json:"provider"`
	DNSTimeout          int      `json:"dns_timeout"`
	Resolvers           []string `json:"resolvers"`
	DisableAuthNS       bool     `json:"disable_authoritative_nameservers"`
	DisableRecursiveNS  bool     `json:"disable_recursive_nameservers"`
	PropagationWait     string   `json:"propagation_wait"`
	RFC2136Nameserver   string   `json:"rfc2136_nameserver"`
	RFC2136TSIGAlgo     string   `json:"rfc2136_tsig_algorithm"`
	RFC2136TSIGKey      string   `json:"rfc2136_tsig_key"`
	RFC2136TSIGSecret   string   `json:"rfc2136_tsig_secret"`
	RFC2136TSIGFile     string   `json:"rfc2136_tsig_file"`
	RFC2136TTL          int      `json:"rfc2136_ttl"`
	RFC2136PropTimeout  string   `json:"rfc2136_propagation_timeout"`
	RFC2136PollInterval string   `json:"rfc2136_polling_interval"`
	ExtraEnv            string   `json:"extra_env"`
}

type Cert struct {
	Name             string   `json:"name"`
	Account          string   `json:"account"`
	Challenge        string   `json:"challenge"`
	KeyType          string   `json:"key_type"`
	EnableCommonName *bool    `json:"enable_common_name"`
	Domains          []string `json:"domains"`
}
