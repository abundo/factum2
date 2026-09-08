package dns

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/abundo/factum2/internal/util"
	goyaml "github.com/goccy/go-yaml"
)

// Config is the runtime DNS config factum2-dns fetches from the primary.
// It extends util.ConfigDNS with the optional zone-editor payload used to
// generate dnsmgr2.yaml and extra JSON records-file sections.
type Config struct {
	util.ConfigDNS
	ZonesEnabled bool
	DhcpEnabled  bool
	ConfigFile   string
	DbFile       string
	Host         ConfigDNSHost
	SOATemplates []ConfigDNSSOA
	Templates    []ConfigDNSTemplate
	Zones        []ConfigDNSZone
	DHCP         ConfigDHCP
}

// ConfigDHCP is the Kea payload written into dnsmgr2.yaml when DhcpEnabled.
type ConfigDHCP struct {
	DnsServers []string           `json:"dns_servers"`
	Host       ConfigDHCPHost     `json:"host_template"`
	Prefixes   []ConfigDHCPPrefix `json:"prefixes"`
}

type ConfigDHCPHost struct {
	Name string          `json:"name"`
	Type string          `json:"type"`
	IPv4 ConfigDHCPProto `json:"ipv4"`
	IPv6 ConfigDHCPProto `json:"ipv6"`
}

type ConfigDHCPProto struct {
	Enable      bool   `json:"enable"`
	ConfigDir   string `json:"config_dir"`
	IncludeFile string `json:"include_file"`
	TmpDir      string `json:"tmp_dir"`
	CmdRestart  string `json:"cmd_restart"`
}

type ConfigDHCPPrefix struct {
	Name       string   `json:"name"`
	Range      string   `json:"range"`
	Gateway    string   `json:"gateway"`
	DnsServers []string `json:"dns_servers"`
}

// ConfigDNSHost is the BIND host template written into dnsmgr2.yaml.
type ConfigDNSHost struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	ConfigDir     string `json:"config_dir"`
	IncludeFile   string `json:"include_file"`
	ZonesDir      string `json:"zones_dir"`
	ZonesFile     string `json:"zones_file"`
	TmpDir        string `json:"tmp_dir"`
	CmdReloadAll  string `json:"cmd_reload_all"`
	CmdReloadZone string `json:"cmd_reload_zone"`
	CmdRestart    string `json:"cmd_restart"`
}

type ConfigDNSSOA struct {
	Name    string `json:"name"`
	Mname   string `json:"mname"`
	Rname   string `json:"rname"`
	Refresh int    `json:"refresh"`
	Retry   int    `json:"retry"`
	Expire  int    `json:"expire"`
	Minimum int    `json:"minimum"`
}

type ConfigDNSTemplate struct {
	Name         string   `json:"name"`
	SOA          string   `json:"soa"`
	DefaultTTL   string   `json:"default_ttl"`
	DNSSECPolicy string   `json:"dnssec_policy"`
	NS           []string `json:"ns"`
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

type yamlSource struct {
	Type string `yaml:"type"`
	Name string `yaml:"name"`
}

type yamlDest struct {
	Type string `yaml:"type"`
	Name string `yaml:"name"`
}

type yamlHostTemplate struct {
	Type          string `yaml:"type"`
	Configdir     string `yaml:"configdir"`
	IncludeFile   string `yaml:"includefile"`
	ZonesDir      string `yaml:"zonesdir"`
	Zonesfile     string `yaml:"zonesfile"`
	Tmpdir        string `yaml:"tmpdir"`
	CmdReloadAll  string `yaml:"cmd_reload_all"`
	CmdReloadZone string `yaml:"cmd_reload_zone"`
	CmdRestart    string `yaml:"cmd_restart"`
}

type yamlSOA struct {
	Mname        string `yaml:"mname"`
	Rname        string `yaml:"rname"`
	SerialFormat string `yaml:"serial_format"`
	Refresh      int    `yaml:"refresh"`
	Retry        int    `yaml:"retry"`
	Expire       int    `yaml:"expire"`
	Minimum      int    `yaml:"minimum"`
}

type yamlNS struct {
	Name  string `yaml:"name"`
	Type  string `yaml:"type"`
	Value string `yaml:"value"`
}

type yamlZoneTemplate struct {
	SOA          string   `yaml:"soa"`
	DefaultTTL   string   `yaml:"default_ttl"`
	DNSSECPolicy string   `yaml:"dnssec_policy,omitempty"`
	NS           []yamlNS `yaml:"ns"`
}

type yamlDNS struct {
	HostTemplates map[string]yamlHostTemplate `yaml:"host_templates"`
	SOATemplates  map[string]yamlSOA          `yaml:"soa_templates"`
	ZoneTemplates map[string]yamlZoneTemplate `yaml:"zone_templates"`
}

type yamlZone struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	DnsTemplate string `yaml:"dns_template"`
}

type yamlDHCPProto struct {
	Enable      bool   `yaml:"enable"`
	Configdir   string `yaml:"configdir"`
	IncludeFile string `yaml:"includefile"`
	Tmpdir      string `yaml:"tmpdir"`
	CmdRestart  string `yaml:"cmd_restart"`
}

type yamlDHCPHost struct {
	Type string        `yaml:"type"`
	IPv4 yamlDHCPProto `yaml:"ipv4"`
	IPv6 yamlDHCPProto `yaml:"ipv6"`
}

type yamlDHCP struct {
	DomainName    string                  `yaml:"domain_name,omitempty"`
	DNSServers    []string                `yaml:"dns_servers,omitempty"`
	HostTemplates map[string]yamlDHCPHost `yaml:"host_templates"`
}

type yamlPrefix struct {
	Name       string   `yaml:"name"`
	Range      string   `yaml:"range,omitempty"`
	Gateway    string   `yaml:"gateway,omitempty"`
	DnsServers []string `yaml:"dns_servers,omitempty"`
}

type yamlDataGroup struct {
	HostDnsTemplate  string       `yaml:"host_dns_template,omitempty"`
	HostDhcpTemplate string       `yaml:"host_dhcp_template,omitempty"`
	Prefixes         []yamlPrefix `yaml:"prefixes,omitempty"`
	Zones            []yamlZone   `yaml:"zones,omitempty"`
}

type yamlRoot struct {
	DefaultDomain string          `yaml:"default_domain,omitempty"`
	Dbfile        string          `yaml:"dbfile"`
	Sources       []yamlSource    `yaml:"sources"`
	Destinations  []yamlDest      `yaml:"destinations"`
	DNS           yamlDNS         `yaml:"dns,omitempty"`
	DHCP          *yamlDHCP       `yaml:"dhcp,omitempty"`
	Dnsmgr2       []yamlDataGroup `yaml:"dnsmgr2"`
}

func hostOrDefault(h ConfigDNSHost) ConfigDNSHost {
	if strings.TrimSpace(h.Name) == "" {
		h.Name = "isc_bind"
	}
	if strings.TrimSpace(h.Type) == "" {
		h.Type = "isc_bind"
	}
	if strings.TrimSpace(h.ConfigDir) == "" {
		h.ConfigDir = "/etc/bind"
	}
	if strings.TrimSpace(h.IncludeFile) == "" {
		h.IncludeFile = "named.conf.dnsmgr2"
	}
	if strings.TrimSpace(h.ZonesDir) == "" {
		h.ZonesDir = "/var/lib/bind"
	}
	if strings.TrimSpace(h.ZonesFile) == "" {
		h.ZonesFile = "{zone}"
	}
	if strings.TrimSpace(h.TmpDir) == "" {
		h.TmpDir = "/var/lib/dnsmgr2"
	}
	if strings.TrimSpace(h.CmdReloadAll) == "" {
		h.CmdReloadAll = "sudo rndc reload"
	}
	if strings.TrimSpace(h.CmdReloadZone) == "" {
		h.CmdReloadZone = "sudo rndc reload {zone}"
	}
	if strings.TrimSpace(h.CmdRestart) == "" {
		h.CmdRestart = "systemctl restart named.service"
	}
	return h
}

func dbFileOrDefault(path string) string {
	if strings.TrimSpace(path) == "" {
		return "/var/lib/dnsmgr2/dnsmgr2.sqlite"
	}
	return path
}

// RenderDnsmgrConfig builds a dnsmgr2.yaml document from the remote DNS config.
func RenderDnsmgrConfig(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("dns config is not loaded")
	}
	host := hostOrDefault(cfg.Host)
	soa := map[string]yamlSOA{}
	for _, s := range cfg.SOATemplates {
		if s.Name == "" {
			continue
		}
		soa[s.Name] = yamlSOA{
			Mname:        s.Mname,
			Rname:        s.Rname,
			SerialFormat: "date_serial",
			Refresh:      s.Refresh,
			Retry:        s.Retry,
			Expire:       s.Expire,
			Minimum:      s.Minimum,
		}
	}
	templates := map[string]yamlZoneTemplate{}
	for _, t := range cfg.Templates {
		if t.Name == "" {
			continue
		}
		ns := make([]yamlNS, 0, len(t.NS))
		for _, host := range t.NS {
			host = strings.TrimSpace(host)
			if host == "" {
				continue
			}
			ns = append(ns, yamlNS{Name: "@", Type: "NS", Value: host})
		}
		ttl := t.DefaultTTL
		if ttl == "" && t.Name != "" {
			ttl = "3600"
		}
		templates[t.Name] = yamlZoneTemplate{
			SOA:          t.SOA,
			DefaultTTL:   ttl,
			DNSSECPolicy: t.DNSSECPolicy,
			NS:           ns,
		}
	}
	zones := make([]yamlZone, 0, len(cfg.Zones))
	for _, z := range cfg.Zones {
		if strings.TrimSpace(z.Name) == "" {
			continue
		}
		typ := z.Type
		if typ == "" {
			typ = "forward"
		}
		zones = append(zones, yamlZone{
			Name:        z.Name,
			Type:        typ,
			DnsTemplate: z.DnsTemplate,
		})
	}
	includeDNS := cfg.ZonesEnabled || !cfg.DhcpEnabled
	group := yamlDataGroup{}
	var dests []yamlDest
	var dnsSection yamlDNS
	if includeDNS {
		dests = append(dests, yamlDest{Type: "dns_isc_bind", Name: host.Name})
		dnsSection = yamlDNS{
			HostTemplates: map[string]yamlHostTemplate{
				host.Name: {
					Type:          host.Type,
					Configdir:     host.ConfigDir,
					IncludeFile:   host.IncludeFile,
					ZonesDir:      host.ZonesDir,
					Zonesfile:     host.ZonesFile,
					Tmpdir:        host.TmpDir,
					CmdReloadAll:  host.CmdReloadAll,
					CmdReloadZone: host.CmdReloadZone,
					CmdRestart:    host.CmdRestart,
				},
			},
			SOATemplates:  soa,
			ZoneTemplates: templates,
		}
		group.HostDnsTemplate = host.Name
		group.Zones = zones
	}
	var dhcpSection *yamlDHCP
	if cfg.DhcpEnabled {
		dhcpHost := dhcpHostOrDefault(cfg.DHCP.Host, cfg.DHCP.Prefixes)
		dests = append(dests, yamlDest{Type: "dhcp_isc_kea", Name: dhcpHost.Name})
		dhcpSection = &yamlDHCP{
			DomainName: cfg.DefaultDomain,
			DNSServers: cfg.DHCP.DnsServers,
			HostTemplates: map[string]yamlDHCPHost{
				dhcpHost.Name: {
					Type: dhcpHost.Type,
					IPv4: yamlDHCPProto{
						Enable:      dhcpHost.IPv4.Enable,
						Configdir:   dhcpHost.IPv4.ConfigDir,
						IncludeFile: dhcpHost.IPv4.IncludeFile,
						Tmpdir:      dhcpHost.IPv4.TmpDir,
						CmdRestart:  dhcpHost.IPv4.CmdRestart,
					},
					IPv6: yamlDHCPProto{
						Enable:      dhcpHost.IPv6.Enable,
						Configdir:   dhcpHost.IPv6.ConfigDir,
						IncludeFile: dhcpHost.IPv6.IncludeFile,
						Tmpdir:      dhcpHost.IPv6.TmpDir,
						CmdRestart:  dhcpHost.IPv6.CmdRestart,
					},
				},
			},
		}
		group.HostDhcpTemplate = dhcpHost.Name
		group.Prefixes = dhcpPrefixesYAML(cfg.DHCP.Prefixes)
	}
	root := yamlRoot{
		DefaultDomain: cfg.DefaultDomain,
		Dbfile:        dbFileOrDefault(cfg.DbFile),
		Sources: []yamlSource{
			{Type: "json", Name: cfg.DestFile},
		},
		Destinations: dests,
		DNS:          dnsSection,
		DHCP:         dhcpSection,
		Dnsmgr2:      []yamlDataGroup{group},
	}
	return goyaml.Marshal(root)
}

func dhcpHostOrDefault(h ConfigDHCPHost, prefixes []ConfigDHCPPrefix) ConfigDHCPHost {
	if strings.TrimSpace(h.Name) == "" {
		h.Name = "isc_kea"
	}
	if strings.TrimSpace(h.Type) == "" {
		h.Type = "isc_kea"
	}
	h.IPv4 = dhcpProtoOrDefault(h.IPv4, "kea-dhcp4.conf", "systemctl restart kea-dhcp4-server")
	h.IPv6 = dhcpProtoOrDefault(h.IPv6, "kea-dhcp6.conf", "systemctl restart kea-dhcp6-server")
	has4, has6 := false, false
	for _, p := range prefixes {
		pfx, err := netip.ParsePrefix(strings.TrimSpace(p.Name))
		if err != nil {
			continue
		}
		if pfx.Addr().Is4() {
			has4 = true
		} else {
			has6 = true
		}
	}
	h.IPv4.Enable = has4 || !has6
	h.IPv6.Enable = has6
	return h
}

func dhcpProtoOrDefault(p ConfigDHCPProto, include, restart string) ConfigDHCPProto {
	if strings.TrimSpace(p.ConfigDir) == "" {
		p.ConfigDir = "/etc/kea"
	}
	if strings.TrimSpace(p.IncludeFile) == "" {
		p.IncludeFile = include
	}
	if strings.TrimSpace(p.TmpDir) == "" {
		p.TmpDir = "/var/lib/dnsmgr2"
	}
	if strings.TrimSpace(p.CmdRestart) == "" {
		p.CmdRestart = restart
	}
	return p
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
