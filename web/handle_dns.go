package web

import (
	"net/http"
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
	DestFile        string              `json:"dest_file"`
	IgnoreModels    string              `json:"ignore_models"`
	IgnorePlatforms string              `json:"ignore_platforms"`
	ZonesEnabled    bool                `json:"zones_enabled"`
	DhcpEnabled     bool                `json:"dhcp_enabled"`
	ZonesFile       string              `json:"zones_file"`
	PrefixesFile    string              `json:"prefixes_file"`
	Zones           []dns.ConfigDNSZone `json:"zones"`
	DHCP            dns.ConfigDHCP      `json:"dhcp"`
}

// ApiDNSConfig returns the DNS sync settings from the database-backed
// Settings row, so factum2-dns - which typically runs on a different host
// than the primary - doesn't need a direct Postgres connection just to read
// these. When the zone editor is enabled it also includes zones and
// records so the CLI can write dnsmgr2 zone/prefix includes and the JSON records file.
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
		ZonesFile:       settings.DnsZonesFile,
		PrefixesFile:    settings.DhcpPrefixesFile,
	}
	if resp.DhcpEnabled {
		dhcp, err := loadDHCPSyncPayload(ctrl.DB, settings)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		resp.DHCP = dhcp
	}
	if resp.ZonesEnabled {
		zones, err := loadDNSSyncPayload(ctrl.DB)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		resp.Zones = zones
	}
	return c.JSON(http.StatusOK, resp)
}

func loadDNSSyncPayload(db *gorm.DB) ([]dns.ConfigDNSZone, error) {
	var zoneRows []models.DnsZone
	if err := db.Preload("DnsTemplate").
		Preload("Records", func(tx *gorm.DB) *gorm.DB { return tx.Order("rank") }).
		Order("name").Find(&zoneRows).Error; err != nil {
		return nil, err
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
	return zones, nil
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
		Prefixes:   prefixes,
	}, nil
}
