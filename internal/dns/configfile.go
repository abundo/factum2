package dns

import (
	"fmt"
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
	ZonesEnabled bool
	DhcpEnabled  bool
	ZonesFile    string
	PrefixesFile string
	Zones        []ConfigDNSZone
	DHCP         ConfigDHCP
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

type ConfigDNSZone struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	DnsTemplate string            `json:"dns_template"`
	Records     []ConfigDNSRecord `json:"records"`
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
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	DnsTemplate string `yaml:"dns_template"`
}

type yamlPrefix struct {
	Name       string   `yaml:"name"`
	Range      string   `yaml:"range,omitempty"`
	Gateway    string   `yaml:"gateway,omitempty"`
	DnsServers []string `yaml:"dns_servers,omitempty"`
}

type yamlZoneInclude struct {
	Zones []yamlZone `yaml:"zones"`
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
			Name:        z.Name,
			Type:        typ,
			DnsTemplate: z.DnsTemplate,
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
	return marshalInclude(yamlZoneInclude{Zones: zoneListYAML(cfg.Zones)})
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
