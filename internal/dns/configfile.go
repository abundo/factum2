package dns

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/util"
	goyaml "github.com/goccy/go-yaml"
)

const zoneIncludeWarning = "# WARNING! do not edit, factum2-dns will overwrite your changes\n"

// Config is the runtime DNS config factum2-dns fetches from the primary.
// It extends util.ConfigDNS with the optional zone-editor payload used to
// generate dnsmgr2 zone/prefix include files and extra JSON records-file sections.
type Config struct {
	util.ConfigDNS
	ZonesEnabled   bool
	DhcpEnabled    bool
	ZonesFile      string
	PrefixesFile   string
	Zones          []ConfigDNSZone
	SOATemplates   []ConfigSOATemplate
	ZoneTemplates  []ConfigZoneTemplate
	DNSSECPolicies []ConfigDNSSECPolicy
	DHCP           ConfigDHCP
}

// ConfigSOATemplate is one dnsmgr2 soa_templates entry. Serial is not
// stored; sync always writes serial_format date_serial.
type ConfigSOATemplate struct {
	Name    string `json:"name"`
	Mname   string `json:"mname"`
	Rname   string `json:"rname"`
	Refresh uint   `json:"refresh"`
	Retry   uint   `json:"retry"`
	Expire  uint   `json:"expire"`
	Minimum uint   `json:"minimum"`
}

// ConfigZoneTemplate is one dnsmgr2 zone_templates entry (a Factum DNS
// template). Nameservers are hostnames; addresses stay in the JSON records
// file as in-zone A/AAAA glue.
type ConfigZoneTemplate struct {
	Name         string                `json:"name"`
	SOA          string                `json:"soa"`
	DefaultTTL   uint                  `json:"default_ttl"`
	DNSSECPolicy string                `json:"dnssec_policy,omitempty"`
	Nameservers  []ConfigDNSNameserver `json:"nameservers,omitempty"`
}

// ConfigDHCP is the prefix list written into the DHCP include when DhcpEnabled.
type ConfigDHCP struct {
	DnsServers []string           `json:"dns_servers"`
	Prefixes   []ConfigDHCPPrefix `json:"prefixes"`
}

type ConfigDHCPPrefix struct {
	Name       string   `json:"name"`
	Range      string   `json:"range"`
	Gateway    string   `json:"gateway"`
	DnsServers []string `json:"dns_servers"`
}

type ConfigDNSNameserver struct {
	Hostname string `json:"hostname"`
	Address  string `json:"address,omitempty"`
}

// ConfigDNSSECPolicy is one BIND dnssec-policy. Sync writes it into the
// zone include; dnsmgr2 emits the block at the top of named.conf.
type ConfigDNSSECPolicy struct {
	Name                     string `json:"name" yaml:"name"`
	KSKLifetime              string `json:"ksk_lifetime" yaml:"ksk_lifetime,omitempty"`
	KSKAlgorithm             string `json:"ksk_algorithm" yaml:"ksk_algorithm,omitempty"`
	ZSKLifetime              string `json:"zsk_lifetime" yaml:"zsk_lifetime,omitempty"`
	ZSKAlgorithm             string `json:"zsk_algorithm" yaml:"zsk_algorithm,omitempty"`
	PurgeKeys                string `json:"purge_keys" yaml:"purge_keys,omitempty"`
	SignaturesValidity       string `json:"signatures_validity" yaml:"signatures_validity,omitempty"`
	SignaturesValidityDNSKEY string `json:"signatures_validity_dnskey" yaml:"signatures_validity_dnskey,omitempty"`
	SignaturesRefresh        string `json:"signatures_refresh" yaml:"signatures_refresh,omitempty"`
}

type ConfigDNSZone struct {
	Name         string                `json:"name"`
	Type         string                `json:"type"`
	DnsTemplate  string                `json:"dns_template"`
	DNSSECPolicy string                `json:"dnssec_policy,omitempty"`
	Nameservers  []ConfigDNSNameserver `json:"nameservers,omitempty"`
	Records      []ConfigDNSRecord     `json:"records"`
}

type ConfigDNSRecord struct {
	Name        string `json:"name"`
	TTL         *uint  `json:"ttl"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description"`
	MAC         string `json:"mac"`
}

type yamlZone struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"`
	DnsTemplate  string `yaml:"dns_template"`
	DNSSECPolicy string `yaml:"dnssec_policy,omitempty"`
}

type yamlDNSSECPolicy struct {
	Name                     string `yaml:"name"`
	KSKLifetime              string `yaml:"ksk_lifetime,omitempty"`
	KSKAlgorithm             string `yaml:"ksk_algorithm,omitempty"`
	ZSKLifetime              string `yaml:"zsk_lifetime,omitempty"`
	ZSKAlgorithm             string `yaml:"zsk_algorithm,omitempty"`
	PurgeKeys                string `yaml:"purge_keys,omitempty"`
	SignaturesValidity       string `yaml:"signatures_validity,omitempty"`
	SignaturesValidityDNSKEY string `yaml:"signatures_validity_dnskey,omitempty"`
	SignaturesRefresh        string `yaml:"signatures_refresh,omitempty"`
}

type yamlPrefix struct {
	Name       string   `yaml:"name"`
	Range      string   `yaml:"range,omitempty"`
	Gateway    string   `yaml:"gateway,omitempty"`
	DnsServers []string `yaml:"dns_servers,omitempty"`
}

type yamlSOATemplate struct {
	Mname        string `yaml:"mname"`
	Rname        string `yaml:"rname"`
	SerialFormat string `yaml:"serial_format"`
	Refresh      uint   `yaml:"refresh"`
	Retry        uint   `yaml:"retry"`
	Expire       uint   `yaml:"expire"`
	Minimum      uint   `yaml:"minimum"`
}

type yamlTemplateNS struct {
	Name  string `yaml:"name"`
	Type  string `yaml:"type"`
	Value string `yaml:"value"`
}

type yamlZoneTemplate struct {
	SOA          string           `yaml:"soa"`
	DefaultTTL   string           `yaml:"default_ttl,omitempty"`
	NS           []yamlTemplateNS `yaml:"ns,omitempty"`
	DNSSECPolicy string           `yaml:"dnssec_policy,omitempty"`
}

type yamlZoneInclude struct {
	SOATemplates   map[string]yamlSOATemplate  `yaml:"soa_templates,omitempty"`
	ZoneTemplates  map[string]yamlZoneTemplate `yaml:"zone_templates,omitempty"`
	DNSSECPolicies []yamlDNSSECPolicy          `yaml:"dnssec_policies,omitempty"`
	Zones          []yamlZone                  `yaml:"zones"`
}

type yamlPrefixInclude struct {
	Prefixes []yamlPrefix `yaml:"prefixes"`
}

func zoneListYAML(zones []ConfigDNSZone) []yamlZone {
	out := make([]yamlZone, 0, len(zones))
	for _, z := range zones {
		if strings.TrimSpace(z.Name) == "" {
			continue
		}
		typ := z.Type
		if typ == "" {
			typ = "forward"
		}
		out = append(out, yamlZone{
			Name:         z.Name,
			Type:         typ,
			DnsTemplate:  z.DnsTemplate,
			DNSSECPolicy: strings.TrimSpace(z.DNSSECPolicy),
		})
	}
	return out
}

func dhcpPrefixesYAML(prefixes []ConfigDHCPPrefix) []yamlPrefix {
	out := make([]yamlPrefix, 0, len(prefixes))
	for _, p := range prefixes {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		out = append(out, yamlPrefix{
			Name:       name,
			Range:      strings.TrimSpace(p.Range),
			Gateway:    strings.TrimSpace(p.Gateway),
			DnsServers: p.DnsServers,
		})
	}
	return out
}

// RenderZoneInclude builds a dnsmgr2 zone-include YAML document. The
// administrator-managed dnsmgr2.yaml lists this file as a separate
// dnsmgr2 item: `- include: /etc/dnsmgr2/zones.yaml`.
func RenderZoneInclude(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("dns config is not loaded")
	}
	return marshalInclude(yamlZoneInclude{
		SOATemplates:   soaTemplatesYAML(cfg.SOATemplates),
		ZoneTemplates:  zoneTemplatesYAML(cfg.ZoneTemplates),
		DNSSECPolicies: dnssecPoliciesYAML(cfg.DNSSECPolicies),
		Zones:          zoneListYAML(cfg.Zones),
	})
}

func soaTemplatesYAML(items []ConfigSOATemplate) map[string]yamlSOATemplate {
	out := make(map[string]yamlSOATemplate, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		out[name] = yamlSOATemplate{
			Mname:        nameserverRdata(item.Mname),
			Rname:        nameserverRdata(item.Rname),
			SerialFormat: "date_serial",
			Refresh:      item.Refresh,
			Retry:        item.Retry,
			Expire:       item.Expire,
			Minimum:      item.Minimum,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func zoneTemplatesYAML(items []ConfigZoneTemplate) map[string]yamlZoneTemplate {
	out := make(map[string]yamlZoneTemplate, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		tpl := yamlZoneTemplate{
			SOA:          strings.TrimSpace(item.SOA),
			NS:           zoneTemplateNS(item.Nameservers),
			DNSSECPolicy: strings.TrimSpace(item.DNSSECPolicy),
		}
		if item.DefaultTTL > 0 {
			tpl.DefaultTTL = strconv.FormatUint(uint64(item.DefaultTTL), 10)
		}
		out[name] = tpl
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func zoneTemplateNS(rows []ConfigDNSNameserver) []yamlTemplateNS {
	seen := make(map[string]struct{}, len(rows))
	out := make([]yamlTemplateNS, 0, len(rows))
	for _, row := range rows {
		host := strings.TrimSpace(row.Hostname)
		if host == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSuffix(host, "."))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, yamlTemplateNS{Name: "@", Type: "NS", Value: nameserverRdata(host)})
	}
	return out
}

func dnssecPoliciesYAML(policies []ConfigDNSSECPolicy) []yamlDNSSECPolicy {
	out := make([]yamlDNSSECPolicy, 0, len(policies))
	for _, p := range policies {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		out = append(out, yamlDNSSECPolicy{
			Name:                     name,
			KSKLifetime:              strings.TrimSpace(p.KSKLifetime),
			KSKAlgorithm:             strings.TrimSpace(p.KSKAlgorithm),
			ZSKLifetime:              strings.TrimSpace(p.ZSKLifetime),
			ZSKAlgorithm:             strings.TrimSpace(p.ZSKAlgorithm),
			PurgeKeys:                strings.TrimSpace(p.PurgeKeys),
			SignaturesValidity:       strings.TrimSpace(p.SignaturesValidity),
			SignaturesValidityDNSKEY: strings.TrimSpace(p.SignaturesValidityDNSKEY),
			SignaturesRefresh:        strings.TrimSpace(p.SignaturesRefresh),
		})
	}
	return out
}

// RenderPrefixInclude builds a dnsmgr2 prefix-include YAML document. The
// administrator-managed dnsmgr2.yaml lists this file as a separate
// dnsmgr2 item after host_dhcp_template:
// `- include: /etc/dnsmgr2/prefixes.yaml`.
func RenderPrefixInclude(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("dns config is not loaded")
	}
	return marshalInclude(yamlPrefixInclude{Prefixes: dhcpPrefixesYAML(cfg.DHCP.Prefixes)})
}

func marshalInclude(doc any) ([]byte, error) {
	body, err := goyaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(zoneIncludeWarning)+len(body))
	out = append(out, zoneIncludeWarning...)
	out = append(out, body...)
	return out, nil
}
