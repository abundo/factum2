package dns

import (
	"fmt"
	"net"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const (
	zoneRecordTypeComment = "COMMENT"
	zoneRecordTypeDomain  = "$DOMAIN"
)

var zoneRecordTypes = []string{
	"A", "AAAA", "CNAME", "MX", "NS", "PTR", "SRV", "TLSA", "TXT",
	zoneRecordTypeComment,
	zoneRecordTypeDomain,
}

var zoneRecordTypeSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(zoneRecordTypes))
	for _, t := range zoneRecordTypes {
		m[t] = struct{}{}
	}
	return m
}()

func ParseZoneRecords(reqs []models.DnsZoneRecordDTO) ([]models.DnsZoneRecord, error) {
	out := make([]models.DnsZoneRecord, 0, len(reqs))
	for i, req := range reqs {
		rec, err := parseZoneRecord(req)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", i+1, err)
		}
		rec.Rank = uint(i)
		out = append(out, rec)
	}
	return out, nil
}

func parseZoneRecord(req models.DnsZoneRecordDTO) (models.DnsZoneRecord, error) {
	typ := strings.ToUpper(strings.TrimSpace(req.Type))
	if typ == "" {
		return models.DnsZoneRecord{}, fmt.Errorf("type is required")
	}
	if _, ok := zoneRecordTypeSet[typ]; !ok {
		return models.DnsZoneRecord{}, fmt.Errorf("unknown record type %q", req.Type)
	}
	if typ == zoneRecordTypeComment {
		return models.DnsZoneRecord{
			Name:  ";",
			Type:  zoneRecordTypeComment,
			Value: strings.TrimSpace(req.Value),
		}, nil
	}
	if typ == zoneRecordTypeDomain {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return models.DnsZoneRecord{}, fmt.Errorf("name is required")
		}
		return models.DnsZoneRecord{
			Name: name,
			Type: zoneRecordTypeDomain,
		}, nil
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return models.DnsZoneRecord{}, fmt.Errorf("name is required")
	}
	value := strings.TrimSpace(req.Value)
	if value == "" {
		return models.DnsZoneRecord{}, fmt.Errorf("value is required")
	}
	if err := validateRecordValue(typ, value); err != nil {
		return models.DnsZoneRecord{}, err
	}
	mac, err := parseRecordMAC(typ, req.MAC)
	if err != nil {
		return models.DnsZoneRecord{}, err
	}
	return models.DnsZoneRecord{
		Name:        name,
		TTL:         normalizeTTL(req.TTL),
		Type:        typ,
		Value:       value,
		Description: strings.TrimSpace(req.Description),
		MAC:         mac,
	}, nil
}

// parseRecordMAC returns canonical lowercase colon form, or "" if unset.
// MAC is only valid on A/AAAA (DHCP host reservations).
func parseRecordMAC(typ, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if typ != "A" && typ != "AAAA" {
		return "", fmt.Errorf("MAC is only valid on A and AAAA records")
	}
	mac, err := NormalizeMAC(raw)
	if err != nil {
		return "", err
	}
	return mac, nil
}

// NormalizeMAC parses a MAC and returns canonical lowercase colon form
// (aa:bb:cc:dd:ee:ff). Accepted input: aa:bb:cc:dd:ee:ff, aa-bb-cc-dd-ee-ff,
// aabb.ccdd.eeff, aabbccddeeff — matching dnsmgr2.
func NormalizeMAC(v string) (string, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "", fmt.Errorf("empty MAC address")
	}
	var hexChars []byte
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == ':' || c == '-' || c == '.':
			continue
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
			hexChars = append(hexChars, c)
		default:
			return "", fmt.Errorf("invalid MAC address")
		}
	}
	if len(hexChars) != 12 {
		return "", fmt.Errorf("invalid MAC address")
	}
	out := make([]byte, 0, 17)
	for i, c := range hexChars {
		if i > 0 && i%2 == 0 {
			out = append(out, ':')
		}
		out = append(out, c)
	}
	return string(out), nil
}

func normalizeTTL(ttl *uint) *uint {
	if ttl != nil && *ttl == 0 {
		return nil
	}
	return ttl
}

func validateRecordValue(typ, value string) error {
	switch typ {
	case "A":
		if !isIPv4Address(value) {
			return fmt.Errorf("A record value must be an IPv4 address")
		}
	case "AAAA":
		if !isIPv6Address(value) {
			return fmt.Errorf("AAAA record value must be an IPv6 address")
		}
	}
	return nil
}

func isIPv4Address(s string) bool {
	if strings.Contains(s, ":") {
		return false
	}
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

func isIPv6Address(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	return ip.To4() == nil
}

func ReplaceZoneRecords(tx *gorm.DB, zoneID uint, recs []models.DnsZoneRecord) error {
	if err := tx.Where("dns_zone_id = ?", zoneID).Delete(&models.DnsZoneRecord{}).Error; err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil
	}
	for i := range recs {
		recs[i].ID = 0
		recs[i].DnsZoneID = zoneID
		recs[i].Rank = uint(i)
	}
	return tx.Create(&recs).Error
}

func ZoneRecordsDTO(recs []models.DnsZoneRecord) []models.DnsZoneRecordDTO {
	out := make([]models.DnsZoneRecordDTO, 0, len(recs))
	for i := range recs {
		r := recs[i]
		out = append(out, models.DnsZoneRecordDTO{
			Name:        r.Name,
			TTL:         r.TTL,
			Type:        r.Type,
			Value:       r.Value,
			Description: r.Description,
			MAC:         r.MAC,
		})
	}
	return out
}

func ParseTemplateNameservers(hosts []string) ([]models.DnsTemplateNameserver, error) {
	out := make([]models.DnsTemplateNameserver, 0, len(hosts))
	for i, h := range hosts {
		host := strings.TrimSpace(h)
		if host == "" {
			return nil, fmt.Errorf("nameserver hostname is required")
		}
		out = append(out, models.DnsTemplateNameserver{
			Rank:     uint(i),
			Hostname: host,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one nameserver is required")
	}
	return out, nil
}

func ReplaceTemplateNameservers(tx *gorm.DB, templateID uint, ns []models.DnsTemplateNameserver) error {
	if err := tx.Where("dns_template_id = ?", templateID).Delete(&models.DnsTemplateNameserver{}).Error; err != nil {
		return err
	}
	if len(ns) == 0 {
		return nil
	}
	for i := range ns {
		ns[i].ID = 0
		ns[i].DnsTemplateID = templateID
		ns[i].Rank = uint(i)
	}
	return tx.Create(&ns).Error
}

func OptionalPolicyID(id *uint) *uint {
	if id == nil || *id == 0 {
		return nil
	}
	return id
}
