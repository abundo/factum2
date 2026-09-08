package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/dns"
	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// DNSConfigResponse is what factum2-dns (internal/dns's FetchRemoteConfig)
// parses - keep the JSON tags in sync with that type.
type DNSConfigResponse struct {
	util.CommonConfig
	DestFile        string                  `json:"dest_file"`
	IgnoreModels    string                  `json:"ignore_models"`
	IgnorePlatforms string                  `json:"ignore_platforms"`
	ZonesEnabled    bool                    `json:"zones_enabled"`
	DhcpEnabled     bool                    `json:"dhcp_enabled"`
	ConfigFile      string                  `json:"config_file"`
	DbFile          string                  `json:"db_file"`
	Host            dns.ConfigDNSHost       `json:"host_template"`
	SOATemplates    []dns.ConfigDNSSOA      `json:"soa_templates"`
	Templates       []dns.ConfigDNSTemplate `json:"zone_templates"`
	Zones           []dns.ConfigDNSZone     `json:"zones"`
	DHCP            dns.ConfigDHCP          `json:"dhcp"`
}

// ApiDNSConfig returns the DNS sync settings from the database-backed
// Settings row, so factum2-dns - which typically runs on a different host
// than the primary - doesn't need a direct Postgres connection just to read
// these. When the zone editor is enabled it also includes templates, zones
// and records so the CLI can write dnsmgr2.yaml and the JSON records file.
func (ctrl *Controller) ApiDNSConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	resp := DNSConfigResponse{
		CommonConfig:    util.NewCommonConfig(settings),
		DestFile:        settings.DnsDestFile,
		IgnoreModels:    settings.DnsIgnoreModels,
		IgnorePlatforms: settings.DnsIgnorePlatforms,
		ZonesEnabled:    dns.ZonesEnabled(ctrl.DB),
		DhcpEnabled:     dns.DhcpEnabled(ctrl.DB),
		ConfigFile:      settings.DnsConfigFile,
		DbFile:          settings.DnsDbFile,
		Host: dns.ConfigDNSHost{
			Name:          settings.DnsHostTemplate,
			Type:          settings.DnsBindType,
			ConfigDir:     settings.DnsBindConfigDir,
			IncludeFile:   settings.DnsBindIncludeFile,
			ZonesDir:      settings.DnsBindZonesDir,
			ZonesFile:     settings.DnsBindZonesFile,
			TmpDir:        settings.DnsBindTmpDir,
			CmdReloadAll:  settings.DnsBindCmdReloadAll,
			CmdReloadZone: settings.DnsBindCmdReloadZone,
			CmdRestart:    settings.DnsBindCmdRestart,
		},
	}
	if resp.DhcpEnabled {
		dhcp, err := loadDHCPSyncPayload(ctrl.DB, settings)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		resp.DHCP = dhcp
	}
	if resp.ZonesEnabled {
		soas, templates, zones, err := loadDNSSyncPayload(ctrl.DB)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		resp.SOATemplates = soas
		resp.Templates = templates
		resp.Zones = zones
	}
	return c.JSON(http.StatusOK, resp)
}

func loadDNSSyncPayload(db *gorm.DB) ([]dns.ConfigDNSSOA, []dns.ConfigDNSTemplate, []dns.ConfigDNSZone, error) {
	var soaRows []models.DnsSOATemplate
	if err := db.Order("name").Find(&soaRows).Error; err != nil {
		return nil, nil, nil, err
	}
	soas := make([]dns.ConfigDNSSOA, 0, len(soaRows))
	for _, s := range soaRows {
		soas = append(soas, dns.ConfigDNSSOA{
			Name:    s.Name,
			Mname:   s.Mname,
			Rname:   s.Rname,
			Refresh: int(s.Refresh),
			Retry:   int(s.Retry),
			Expire:  int(s.Expire),
			Minimum: int(s.TTL),
		})
	}

	var tmplRows []models.DnsTemplate
	if err := db.Preload("SOATemplate").Preload("DNSSECPolicy").
		Preload("Nameservers", func(tx *gorm.DB) *gorm.DB { return tx.Order("rank") }).
		Order("name").Find(&tmplRows).Error; err != nil {
		return nil, nil, nil, err
	}
	templates := make([]dns.ConfigDNSTemplate, 0, len(tmplRows))
	for _, t := range tmplRows {
		ns := make([]string, 0, len(t.Nameservers))
		for _, n := range t.Nameservers {
			ns = append(ns, n.Hostname)
		}
		policy := ""
		if t.DNSSECPolicy != nil {
			policy = t.DNSSECPolicy.Name
		}
		ttl := ""
		if t.DefaultTTL > 0 {
			ttl = strconv.FormatUint(uint64(t.DefaultTTL), 10)
		}
		templates = append(templates, dns.ConfigDNSTemplate{
			Name:         t.Name,
			SOA:          t.SOATemplate.Name,
			DefaultTTL:   ttl,
			DNSSECPolicy: policy,
			NS:           ns,
		})
	}

	var zoneRows []models.DnsZone
	if err := db.Preload("DnsTemplate").
		Preload("Records", func(tx *gorm.DB) *gorm.DB { return tx.Order("rank") }).
		Order("name").Find(&zoneRows).Error; err != nil {
		return nil, nil, nil, err
	}
	zones := make([]dns.ConfigDNSZone, 0, len(zoneRows))
	for _, z := range zoneRows {
		recs := make([]dns.ConfigDNSRecord, 0, len(z.Records))
		for _, r := range z.Records {
			recs = append(recs, dns.ConfigDNSRecord{
				Name:        r.Name,
				TTL:         r.TTL,
				Type:        r.Type,
				Value:       r.Value,
				Description: r.Description,
				MAC:         r.MAC,
			})
		}
		zones = append(zones, dns.ConfigDNSZone{
			Name:        z.Name,
			Type:        z.Type,
			DnsTemplate: z.DnsTemplate.Name,
			Records:     recs,
		})
	}
	return soas, templates, zones, nil
}

func loadDHCPSyncPayload(db *gorm.DB, settings *models.Settings) (dns.ConfigDHCP, error) {
	rows, err := ipam.ListDhcpPrefixes(db)
	if err != nil {
		return dns.ConfigDHCP{}, err
	}
	prefixes := make([]dns.ConfigDHCPPrefix, 0, len(rows))
	for _, row := range rows {
		p := dns.ConfigDHCPPrefix{Name: row.Prefix, Gateway: strings.TrimSpace(row.DhcpGateway)}
		start := strings.TrimSpace(row.DhcpRangeStart)
		end := strings.TrimSpace(row.DhcpRangeEnd)
		if start != "" && end != "" {
			p.Range = start + "-" + end
		}
		p.DnsServers = ipam.SplitLines(row.DhcpDnsServers)
		prefixes = append(prefixes, p)
	}
	return dns.ConfigDHCP{
		DnsServers: ipam.SplitLines(settings.DhcpDnsServers),
		Host: dns.ConfigDHCPHost{
			Name: settings.DhcpHostTemplate,
			Type: settings.DhcpKeaType,
			IPv4: dns.ConfigDHCPProto{
				ConfigDir:   settings.DhcpKea4ConfigDir,
				IncludeFile: settings.DhcpKea4IncludeFile,
				TmpDir:      settings.DhcpKea4TmpDir,
				CmdRestart:  settings.DhcpKea4CmdRestart,
			},
			IPv6: dns.ConfigDHCPProto{
				ConfigDir:   settings.DhcpKea6ConfigDir,
				IncludeFile: settings.DhcpKea6IncludeFile,
				TmpDir:      settings.DhcpKea6TmpDir,
				CmdRestart:  settings.DhcpKea6CmdRestart,
			},
		},
		Prefixes: prefixes,
	}, nil
}
