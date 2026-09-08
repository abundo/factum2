package dns

//
// Manage DNS server via Dnsmgr
//

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"unicode"

	"github.com/abundo/dnsmgr2/dnsmgr"
	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

type DNSClient struct {
	Config *util.ConfigAgentRoot
	DNS    *Config
	// update, if set, replaces runDnsmgrUpdate — tests stub it so Sync
	// does not apply BIND/Kea.
	update func() error
}

// NewDNSClient fetches the DNS sync settings from the primary over REST -
// factum2-dns typically runs on a different host than the primary, so unlike
// before, it no longer opens a direct connection to factum's Postgres just
// to read these (see util.ConfigDNS's doc comment).
func NewDNSClient(config *util.ConfigAgentRoot) (*DNSClient, error) {
	client := new(DNSClient)
	client.Config = config

	var err error
	client.DNS, err = FetchRemoteConfig(&config.Factum)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// Get all devices from factum database
// Write a dnsmgr2 JSON records file and config, then apply BIND/Kea.
func (dns *DNSClient) Sync(reporter jobevent.Reporter) error {
	reporter.Emit(jobevent.Info, "DNS sync started")
	if err := dns.validate(); err != nil {
		reporter.EmitErr(err)
		return err
	}
	factumClient := factum.NewFactumClient(&dns.Config.Factum)
	devices, err := factumClient.GetDevicesWithInterfaces()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	return dns.syncDevices(reporter, devices)
}

func (dns *DNSClient) validate() error {
	if dns.DNS == nil {
		return errors.New("dns config is not loaded")
	}
	if strings.TrimSpace(dns.DNS.DefaultDomain) == "" {
		return errors.New("default_domain is not configured")
	}
	if dns.DNS.DestFile == "" {
		return errors.New("dns dest_file is not configured")
	}
	return nil
}

func (dns *DNSClient) syncDevices(reporter jobevent.Reporter, all []*models.Device) error {
	devices, counts := dns.filterDevices(all)
	reporter.Emit(jobevent.Info,
		"Filtered devices: %d not enabled, %d ignored model, %d ignored platform, %d empty DNS name",
		counts.notEnabled, counts.ignoreModel, counts.ignorePlatform, counts.emptyName)
	reporter.Emit(jobevent.Info, "Devices: %d of %d total", len(devices), len(all))

	tmpFile := dns.DNS.DestFile + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	recordCount, err := writeRecordsWithZones(f, dns.DNS.DefaultDomain, devices, dns.zoneSections())
	closeErr := f.Close()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if closeErr != nil {
		reporter.EmitErr(closeErr)
		return closeErr
	}
	reporter.Emit(jobevent.Info, "DNS sync: wrote %d record(s) to %s", recordCount, tmpFile)

	changed, err := installConfFile(tmpFile, dns.DNS.DestFile)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if changed {
		reporter.Emit(jobevent.Info, "DNS records changed")
	} else {
		reporter.Emit(jobevent.Info, "DNS records unchanged")
	}

	if (dns.DNS.ZonesEnabled || dns.DNS.DhcpEnabled) && strings.TrimSpace(dns.DNS.ConfigFile) != "" {
		yamlBytes, err := RenderDnsmgrConfig(dns.DNS)
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
		cfgTmp := dns.DNS.ConfigFile + ".tmp"
		if err := os.WriteFile(cfgTmp, yamlBytes, 0o644); err != nil {
			reporter.EmitErr(err)
			return err
		}
		cfgChanged, err := installConfFile(cfgTmp, dns.DNS.ConfigFile)
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
		if cfgChanged {
			reporter.Emit(jobevent.Info, "dnsmgr2 config changed: %s", dns.DNS.ConfigFile)
		} else {
			reporter.Emit(jobevent.Info, "dnsmgr2 config unchanged: %s", dns.DNS.ConfigFile)
		}
	}

	reporter.Emit(jobevent.Info, "running dnsmgr2 update")
	// Always apply: UpdateCommit skips BIND reload when zone content is
	// unchanged, and a previous failed update still needs a retry.
	if err := dns.runUpdate(); err != nil {
		reporter.EmitErr(err)
		return err
	}
	return nil
}

func (dns *DNSClient) runUpdate() error {
	if dns.update != nil {
		return dns.update()
	}
	return runDnsmgrUpdate(dns.dnsmgrConfigPath())
}

func (dns *DNSClient) dnsmgrConfigPath() string {
	if dns.DNS != nil {
		if p := strings.TrimSpace(dns.DNS.ConfigFile); p != "" {
			return p
		}
	}
	return "/etc/dnsmgr2/dnsmgr2.yaml"
}

func runDnsmgrUpdate(configFile string) error {
	cfg, err := dnsmgr.LoadConfigFile(configFile)
	if err != nil {
		return fmt.Errorf("dnsmgr2 config %s: %w", configFile, err)
	}
	dm, err := dnsmgr.NewDnsManager(cfg)
	if err != nil {
		return fmt.Errorf("dnsmgr2: %w", err)
	}
	if err := dm.Load(); err != nil {
		return fmt.Errorf("dnsmgr2 load: %w", err)
	}
	if err := dm.Sync(); err != nil {
		return fmt.Errorf("dnsmgr2 update: %w", err)
	}
	return nil
}

// installConfFile installs tmpFile as dst, but only if its content differs
// from what's already there.
func installConfFile(tmpFile, dst string) (bool, error) {
	newContent, err := os.ReadFile(tmpFile)
	if err != nil {
		return false, err
	}
	oldContent, err := os.ReadFile(dst)
	if err == nil && bytes.Equal(oldContent, newContent) {
		os.Remove(tmpFile)
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.Rename(tmpFile, dst); err != nil {
		return false, err
	}
	return true, nil
}

// isInList reports whether value appears in list, a newline-separated set
// of values (Settings.DnsIgnore*).
func isInList(value, list string) bool {
	if value == "" {
		return false
	}
	for _, item := range strings.Split(list, "\n") {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

type filterCounts struct {
	notEnabled     int
	ignoreModel    int
	ignorePlatform int
	emptyName      int
}

// filterDevices keeps devices that should appear in the dnsmgr2 records
// file: enabled, not on IgnoreModels/IgnorePlatforms, and with a name that
// sanitizes to at least one DNS label.
func (dns *DNSClient) filterDevices(all []*models.Device) ([]*models.Device, filterCounts) {
	var counts filterCounts
	var devices []*models.Device
	domain := ""
	ignoreModels := ""
	ignorePlatforms := ""
	if dns.DNS != nil {
		domain = dns.DNS.DefaultDomain
		ignoreModels = dns.DNS.IgnoreModels
		ignorePlatforms = dns.DNS.IgnorePlatforms
	}
	for _, device := range all {
		if !device.Enabled {
			counts.notEnabled++
			continue
		}
		if isInList(device.ModelName, ignoreModels) {
			counts.ignoreModel++
			continue
		}
		if isInList(device.Platform, ignorePlatforms) {
			counts.ignorePlatform++
			continue
		}
		if dnsDeviceName(device.Name, domain) == "" {
			counts.emptyName++
			continue
		}
		devices = append(devices, device)
	}
	return devices, counts
}

func (dns *DNSClient) zoneSections() []ConfigDNSZone {
	if dns.DNS == nil || !dns.DNS.ZonesEnabled {
		return nil
	}
	return dns.DNS.Zones
}

func parseRecord(cidr string) (ip, rrtype string, ok bool) {
	host := strings.Split(cidr, "/")[0]
	if host == "" {
		return "", "", false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", "", false
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String(), "A", true
	}
	return addr.String(), "AAAA", true
}

// dnsDeviceName turns a factum device name into a relative DNS name for a
// dnsmgr2 records file: strip a trailing "."+domain suffix so we don't
// double-append the default domain, then sanitize each label like
// dnsInterfaceLabel. Empty if nothing usable remains.
func dnsDeviceName(name, domain string) string {
	name = strings.TrimSuffix(name, ".")
	name = util.ShortName(name, domain)
	var labels []string
	for _, part := range strings.Split(name, ".") {
		label := dnsInterfaceLabel(part)
		if label == "" {
			continue
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, ".")
}

// dnsInterfaceLabel turns an interface name into a single DNS label:
// lowercase, non-alphanumerics collapsed to '-', trimmed. "Ethernet1/1.100"
// becomes "ethernet1-1-100". Empty if nothing usable remains.
func dnsInterfaceLabel(name string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if b.Len() > 0 && !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-")
	}
	return s
}
