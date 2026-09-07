package dns

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/util"
	goyaml "github.com/goccy/go-yaml"
)

// Config is the runtime DNS config factum2-dns fetches from the primary.
// It extends util.ConfigDNS with the optional zone-editor payload used to
// generate dnsmgr2.yaml and extra records-file sections.
type Config struct {
	util.ConfigDNS
	ZonesEnabled bool
	ConfigFile   string
	DbFile       string
	Host         ConfigDNSHost
	SOATemplates []ConfigDNSSOA
	Templates    []ConfigDNSTemplate
	Zones        []ConfigDNSZone
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

type yamlDataGroup struct {
	HostDnsTemplate string     `yaml:"host_dns_template"`
	Zones           []yamlZone `yaml:"zones"`
}

type yamlRoot struct {
	DefaultDomain string          `yaml:"default_domain,omitempty"`
	Dbfile        string          `yaml:"dbfile"`
	Sources       []yamlSource    `yaml:"sources"`
	Destinations  []yamlDest      `yaml:"destinations"`
	DNS           yamlDNS         `yaml:"dns"`
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
	root := yamlRoot{
		DefaultDomain: cfg.DefaultDomain,
		Dbfile:        dbFileOrDefault(cfg.DbFile),
		Sources: []yamlSource{
			{Type: "file", Name: cfg.DestFile},
		},
		Destinations: []yamlDest{
			{Type: "dns_isc_bind", Name: host.Name},
		},
		DNS: yamlDNS{
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
		},
		Dnsmgr2: []yamlDataGroup{
			{
				HostDnsTemplate: host.Name,
				Zones:           zones,
			},
		},
	}
	return goyaml.Marshal(root)
}

func writeZoneRecords(w io.Writer, recs []ConfigDNSRecord) int {
	n := 0
	for _, rec := range recs {
		switch strings.ToUpper(rec.Type) {
		case zoneRecordTypeComment:
			line := "; " + rec.Value
			if rec.Value == "" {
				line = ";"
			}
			fmt.Fprintf(w, "%s\n", line)
		case zoneRecordTypeDomain:
			if strings.TrimSpace(rec.Name) == "" {
				continue
			}
			fmt.Fprintf(w, "$DOMAIN %s\n", strings.TrimSpace(rec.Name))
		default:
			if rec.Name == "" || rec.Type == "" || rec.Value == "" {
				continue
			}
			ttl := ""
			if rec.TTL != nil && *rec.TTL > 0 {
				ttl = strconv.FormatUint(uint64(*rec.TTL), 10)
			}
			if ttl != "" {
				fmt.Fprintf(w, "%-40s  %-8s %-9s %s\n", rec.Name, ttl, rec.Type, rec.Value)
			} else {
				fmt.Fprintf(w, "%-40s  %-9s %s\n", rec.Name, rec.Type, rec.Value)
			}
			n++
		}
	}
	return n
}
