package dns

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/abundo/factum2/models"
)

// JSON records file consumed by dnsmgr2 (sources[].type: json).
const recordsJSONVersion = 1

type recordsJSON struct {
	Version int                 `json:"version"`
	Domains []recordsJSONDomain `json:"domains"`
}

type recordsJSONDomain struct {
	Name    string              `json:"name"`
	Records []recordsJSONRecord `json:"records,omitempty"`
}

type recordsJSONRecord struct {
	Name  string `json:"name"`
	TTL   uint   `json:"ttl,omitempty"`
	Type  string `json:"type"`
	Value string `json:"value"`
	MAC   string `json:"mac,omitempty"`
}

// writeRecords emits a dnsmgr2 JSON records file: one domain, then one
// A/AAAA per device primary IP, then one A/AAAA per interface address
// named <sanitized-interface>.<sanitized-device>, then A/AAAA for each
// Netbox dns_name on those addresses.
func writeRecords(w io.Writer, domain string, devices []*models.Device) (int, error) {
	return writeRecordsWithZones(w, domain, devices, nil)
}

func writeRecordsWithZones(w io.Writer, domain string, devices []*models.Device, zones []ConfigDNSZone) (int, error) {
	doc := recordsJSON{Version: recordsJSONVersion}
	recordCount := 0
	writtenDefault := false
	for _, zone := range zones {
		name := strings.TrimSpace(zone.Name)
		if name == "" {
			continue
		}
		// Reverse zones are CIDR names in dnsmgr2.yaml, not JSON domains.
		if isReverseZoneType(zone.Type) {
			continue
		}
		d := recordsJSONDomain{Name: name}
		if domain != "" && strings.EqualFold(name, domain) {
			recs := deviceRecordsJSON(domain, devices)
			d.Records = append(d.Records, recs...)
			recordCount += len(recs)
			writtenDefault = true
		} else {
			recs := addressDNSNameRecords(devices, name, false)
			d.Records = append(d.Records, recs...)
			recordCount += len(recs)
		}
		recs := zoneRecordsJSON(zone.Records)
		d.Records = append(d.Records, recs...)
		recordCount += len(recs)
		doc.Domains = append(doc.Domains, d)
	}
	if !writtenDefault && domain != "" {
		recs := deviceRecordsJSON(domain, devices)
		doc.Domains = append(doc.Domains, recordsJSONDomain{Name: domain, Records: recs})
		recordCount += len(recs)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return 0, err
	}
	return recordCount, nil
}

func deviceRecordsJSON(domain string, devices []*models.Device) []recordsJSONRecord {
	var out []recordsJSONRecord
	for _, device := range devices {
		host := dnsDeviceName(device.Name, domain)
		if host == "" {
			continue
		}
		out = appendAddressJSON(out, host, device.PrimaryIPv4)
		out = appendAddressJSON(out, host, device.PrimaryIPv6)
		for _, intf := range device.Interfaces {
			label := dnsInterfaceLabel(intf.Name)
			if label == "" {
				continue
			}
			name := label + "." + host
			for _, addr := range intf.Addresses {
				out = appendAddressJSON(out, name, addr.Address)
			}
		}
	}
	out = append(out, addressDNSNameRecords(devices, domain, true)...)
	return out
}

// addressDNSNameRecords emits A/AAAA records from Netbox dns_name on
// interface IP addresses that belong in zone.
func addressDNSNameRecords(devices []*models.Device, zone string, allowUnqualified bool) []recordsJSONRecord {
	var out []recordsJSONRecord
	for _, device := range devices {
		for _, intf := range device.Interfaces {
			for _, addr := range intf.Addresses {
				if addr.DNSName == "" {
					continue
				}
				name := dnsNameRelative(addr.DNSName, zone, allowUnqualified)
				if name == "" {
					continue
				}
				out = appendAddressJSON(out, name, addr.Address)
			}
		}
	}
	return out
}

func appendAddressJSON(out []recordsJSONRecord, name, cidr string) []recordsJSONRecord {
	ip, rrtype, ok := parseRecord(cidr)
	if !ok {
		return out
	}
	return append(out, recordsJSONRecord{Name: name, Type: rrtype, Value: ip})
}

func isReverseZoneType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case models.DnsZoneTypeReverse4, models.DnsZoneTypeReverse6:
		return true
	default:
		return false
	}
}

func zoneRecordsJSON(recs []ConfigDNSRecord) []recordsJSONRecord {
	var out []recordsJSONRecord
	for _, rec := range recs {
		switch strings.ToUpper(rec.Type) {
		case zoneRecordTypeComment, zoneRecordTypeDomain:
			continue
		default:
			if rec.Name == "" || rec.Type == "" || rec.Value == "" {
				continue
			}
			jr := recordsJSONRecord{
				Name:  rec.Name,
				Type:  rec.Type,
				Value: rec.Value,
				MAC:   strings.TrimSpace(rec.MAC),
			}
			if rec.TTL != nil && *rec.TTL > 0 {
				jr.TTL = *rec.TTL
			}
			out = append(out, jr)
		}
	}
	return out
}
