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

// LoadDevices reads oxidized's own router.db (DestFile), one "name:model"
// per line, into o.devices.
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
		tmp := strings.SplitN(line, ":", 2)
		if len(tmp) < 2 {
			fmt.Println("Ignoring line ", line)
			continue
		}
		devices[tmp[0]] = &OxidizedDevice{Name: tmp[0], Model: tmp[1]}
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
	url := fmt.Sprintf("%s/node/fetch/%s", o.c.URL, util.FormatName(o.c.DefaultDomain, name))
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

// SaveDevices writes one "name:model" line per device to filename - the
// router.db format oxidized (and LoadDevices above) expects. Oxidized
// always uses FQDNs, so a short hostname (no '.') is qualified with
// DefaultDomain. A device whose name is already its own primary IPv4
// address is written as "address:model" instead, to match the device-api
// naming used by the primary. Devices with no primary IPv4 are skipped.
func (o *oxidizedClient) SaveDevices(filename string, devices []*models.Device) (int, error) {
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
		addr4 := strings.Split(device.PrimaryIPv4, "/")[0]
		if device.Name == addr4 {
			fmt.Fprintf(f, "%s:%s\n", addr4, model)
		} else {
			fmt.Fprintf(f, "%s:%s\n", util.FormatName(o.c.DefaultDomain, device.Name), model)
		}
		count++
	}
	return count, nil
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
