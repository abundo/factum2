package oxidized

//
// Low-level client for the Oxidized HTTP API, plus reading/writing its
// router.db device list file.
//

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

// ErrNotImplemented is returned by GetDevice, which oxidized has no single-
// device lookup API for - only a full router.db dump (LoadDevices/GetDevices)
// or a fetch of a device's last stored config (GetDeviceConfig).
var ErrNotImplemented = errors.New("not implemented")

// OxidizedDevice is one entry from oxidized's own router.db file.
type OxidizedDevice struct {
	Name  string
	IP    string
	Model string
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

type oxidizedClient struct {
	c       util.ConfigOxidized
	http    doer
	devices map[string]*OxidizedDevice
}

func NewOxidizedClient(c util.ConfigOxidized) *oxidizedClient {
	return newOxidizedClient(c, nil)
}

func newOxidizedClient(c util.ConfigOxidized, h doer) *oxidizedClient {
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	return &oxidizedClient{c: c, http: h}
}

// get issues a GET request against url, using basic auth if the config has
// a username set.
func (o *oxidizedClient) get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if o.c.User != "" {
		req.SetBasicAuth(o.c.User, o.c.Pass)
	}
	return o.http.Do(req)
}

// LoadDevices reads oxidized's own router.db (DestFile) into o.devices.
// Current lines are "name:ip:model". Legacy "name:model" lines (no IP
// column) are still accepted.
func (o *oxidizedClient) LoadDevices() error {
	f, err := os.Open(o.c.DestFile)
	if err != nil {
		return err
	}
	defer f.Close()

	devices := make(map[string]*OxidizedDevice)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		name, ip, model, ok := parseRouterDBLine(line)
		if !ok {
			fmt.Println("Ignoring line ", line)
			continue
		}
		devices[name] = &OxidizedDevice{Name: name, IP: ip, Model: model}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	o.devices = devices
	return nil
}

// GetDevice looks up a single device by name. Oxidized has no API for this,
// only a full router.db dump - not implemented, same as the Python original.
func (o *oxidizedClient) GetDevice(name string) (*OxidizedDevice, error) {
	return nil, ErrNotImplemented
}

// GetDevices returns the devices currently listed in oxidized's router.db,
// loading it first if not already loaded.
func (o *oxidizedClient) GetDevices() (map[string]*OxidizedDevice, error) {
	if o.devices == nil {
		if err := o.LoadDevices(); err != nil {
			return nil, err
		}
	}
	return o.devices, nil
}

// GetDeviceConfig fetches the last configuration oxidized stored for name.
// Short hostnames are qualified with the default domain first — oxidized
// nodes are always stored as FQDNs. Returns "", nil if oxidized has no
// configuration for it.
func (o *oxidizedClient) GetDeviceConfig(name string) (string, error) {
	url := fmt.Sprintf("%s/node/fetch/%s", o.c.URL, deviceFQDN(name, o.c.DefaultDomain))
	resp, err := o.get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// SaveDevices writes one "name:ip:model" line per device to filename —
// the router.db format oxidized (and LoadDevices above) expects when its
// CSV source maps name: 0, ip: 1, model: 2. Name is an FQDN (the same
// name factum2-dns publishes), not a short factum hostname; ip is the
// primary IPv4 without a prefix, which oxidized uses as the SSH target.
// A device whose name is already its own primary IPv4 is written with
// that address in both name and ip, to match the device-api naming used
// by the primary. Devices with no primary IPv4 are skipped. Named
// devices require DefaultDomain.
func (o *oxidizedClient) SaveDevices(filename string, devices []*models.Device) (int, error) {
	domain := strings.TrimSpace(o.c.DefaultDomain)
	if domain == "" {
		for _, device := range devices {
			if device.PrimaryIPv4 == "" {
				continue
			}
			if device.Name != routerDBIP(device) {
				return 0, fmt.Errorf("default_domain is not configured; oxidized router.db names must be FQDNs (the same names factum2-dns publishes)")
			}
		}
	}

	f, err := os.Create(filename)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	for _, device := range devices {
		if device.PrimaryIPv4 == "" {
			continue
		}
		model := device.Platform // device-api 'platform' is called 'model' in oxidized
		fmt.Fprintf(f, "%s:%s:%s\n", routerDBName(device, domain), routerDBIP(device), model)
		count++
	}
	return count, nil
}

// parseRouterDBLine splits one router.db line. Current format is
// name:ip:model; legacy name:model (no IP column) is still accepted.
func parseRouterDBLine(line string) (name, ip, model string, ok bool) {
	if line == "" {
		return "", "", "", false
	}
	parts := strings.SplitN(line, ":", 3)
	switch len(parts) {
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return "", "", "", false
		}
		return parts[0], "", parts[1], true
	case 3:
		if parts[0] == "" || parts[2] == "" {
			return "", "", "", false
		}
		return parts[0], parts[1], parts[2], true
	default:
		return "", "", "", false
	}
}

// routerDBName is the first field of a router.db line: oxidized's node
// name (git identity). Named devices are written as FQDNs; a device
// named after its own primary IPv4 is left as the address.
func routerDBName(device *models.Device, domain string) string {
	addr4 := routerDBIP(device)
	if device.Name == addr4 {
		return addr4
	}
	return deviceFQDN(device.Name, domain)
}

// routerDBIP is the second field of a router.db line: the primary IPv4
// without a CIDR prefix, which oxidized uses to connect.
func routerDBIP(device *models.Device) string {
	return strings.Split(device.PrimaryIPv4, "/")[0]
}

// deviceFQDN qualifies name with domain so oxidized can resolve it via DNS.
// Names that already end with the default domain, and IPv4/IPv6 literals,
// are returned unchanged. Unlike util.FormatName, extra labels such as
// "rtr1.site" are still qualified ("rtr1.site.example.com") so they match
// the absolute names factum2-dns publishes.
func deviceFQDN(name, domain string) string {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	domain = strings.Trim(strings.TrimSpace(domain), ".")
	if name == "" {
		return name
	}
	if net.ParseIP(name) != nil {
		return name
	}
	if domain == "" {
		return name
	}
	suffix := "." + domain
	if strings.EqualFold(name, domain) || strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
		return name
	}
	return name + suffix
}

// Reload asks oxidized to reload its router.db configuration file. Returns
// the HTTP status code.
func (o *oxidizedClient) Reload() (int, error) {
	url := fmt.Sprintf("%s/reload?format=json", o.c.URL)
	resp, err := o.get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
