// syncs interfaces, addresses, VRF-aware address
// dedup, LLDP-discovered cable connections, and on-device ELINEs (as
// NetBox L2VPNs of type EVPL) from live network devices into NetBox. The
// device/interface/address snapshot read for comparison comes from
// factum's own DB (already synced from Netbox by internal/netbox, see
// FactumAPI) rather than Netbox directly - only the actual writes go
// straight to Netbox (see NetboxAPI).
package devicesync

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"golang.org/x/sync/errgroup"
)

// syncWorkers bounds how many devices are handled at once, both while
// connecting (SSH/NETCONF to the device itself) and in each forEachPair
// phase (REST calls to Netbox) - each phase still runs to completion across
// every pair before the next phase starts (see run()), so this only
// parallelizes independent, same-phase, per-device work. This is I/O-bound
// work (waiting on the device or on Netbox, not on CPU), so unlike a
// CPU-bound worker pool there's no reason to tie this to runtime.NumCPU() -
// the ceiling is whatever concurrent load the devices/Netbox can take
// without erroring or rate-limiting, which in practice is comfortably above
// core count. 8 is an untuned starting point; raise it if a real run shows
// headroom (no errors/timeouts) and lower it if Netbox starts rejecting
// requests.
const syncWorkers = 8

// SyncOptions controls which devices are synced and how destructive
// changes are confirmed
type SyncOptions struct {
	Name       string // sync only the device with this name, "" for all
	Platform   string // sync only devices with this Netbox platform, "" for all
	Unattended bool   // don't prompt before deleting an interface/address
}

// FactumAPI narrows *factum.FactumClient to what DeviceSync needs, so tests
// can substitute a fake - same shape as NetboxAPI.
type FactumAPI interface {
	// GetDevices returns every device, without interfaces/addresses
	// populated - used only for the device-selection filter pass.
	GetDevices() ([]*models.Device, error)
	// GetDeviceByName returns one device with interfaces/addresses
	// populated.
	GetDeviceByName(name string) (*models.Device, error)
}

// devicePair is a factum device (already synced from Netbox) paired with
// its parsed live config and the driver used to fetch it (kept around for
// syncConnections' GetNeighbors call)
type devicePair struct {
	nbDevice *models.Device
	config   *drivers.DeviceConfig
	driver   drivers.DriverClient
}

type DeviceSync struct {
	nb       *NetboxMgr
	factum   FactumAPI
	cfg      *util.ConfigDeviceSync
	reporter jobevent.Reporter
	opts     SyncOptions
	pairs    []devicePair

	// connMu serializes syncConnection: an LLDP-discovered link's two ends
	// belong to two different pairs, each processed by its own
	// forEachPair worker, and both sides read-then-act on the same cable -
	// without this, two workers can race to create/repair/delete it
	// concurrently.
	connMu sync.Mutex

	// deviceCache memoizes factum.GetDeviceByName within one run: it's
	// called both to build each pair's full snapshot (connectDevice) and to
	// resolve LLDP neighbors (syncConnections), and the same device
	// commonly shows up as both - e.g. two routers directly cabled together
	// that are both being synced in this run.
	deviceCache   map[string]*models.Device
	deviceCacheMu sync.Mutex
}

// Sync fetches every eligible device from factum (already synced from
// Netbox), connects to each over its driver, and syncs
// interfaces/addresses/connections/prefixes back to Netbox.
func Sync(api NetboxAPI, factumAPI FactumAPI, cfg *util.ConfigDeviceSync, reporter jobevent.Reporter, opts SyncOptions) error {
	ds := &DeviceSync{
		nb:          NewNetboxMgr(api, reporter),
		factum:      factumAPI,
		cfg:         cfg,
		reporter:    reporter,
		opts:        opts,
		deviceCache: map[string]*models.Device{},
	}
	return ds.run()
}

// factumDevice returns the named device (with interfaces/addresses
// populated) from factum, from ds.deviceCache unless this is the first
// lookup for that name this run.
func (ds *DeviceSync) factumDevice(name string) (*models.Device, error) {
	ds.deviceCacheMu.Lock()
	defer ds.deviceCacheMu.Unlock()
	if d, ok := ds.deviceCache[name]; ok {
		return d, nil
	}
	d, err := ds.factum.GetDeviceByName(name)
	if err != nil {
		return nil, err
	}
	ds.deviceCache[name] = d
	return d, nil
}

func (ds *DeviceSync) run() error {
	ds.reporter.Emit(jobevent.Info, "device-sync started")

	if ds.opts.Platform != "" {
		if _, ok := drivers.SupportedPlatforms[strings.ToLower(ds.opts.Platform)]; !ok {
			ds.reporter.Emit(jobevent.Error, "unknown platform %q", ds.opts.Platform)
			return nil
		}
	}

	devices, err := ds.devicesToSync()
	if err != nil {
		return err
	}

	var toConnect []*models.Device
	for _, d := range devices {
		if ds.deviceSelected(d) {
			toConnect = append(toConnect, d)
		}
	}
	ds.connectDevices(toConnect)

	// VLAN group, resolved once up front (not per device) - every VLAN-
	// touching phase below no-ops if VlanGroupName is unset, making VLAN
	// sync opt-in via Settings.
	if ds.cfg.VlanGroupName != "" {
		if _, err := ds.nb.EnsureVlanGroup(ds.cfg.VlanGroupName); err != nil {
			ds.reporter.Emit(jobevent.Error, "resolve netbox vlan group %q: %v", ds.cfg.VlanGroupName, err)
		}
	}

	// Interfaces, order matters: delete before create so a moved
	// interface can't collide with itself, description/vrf sync last.
	ds.forEachPair(ds.interfacesDelete)
	ds.forEachPair(ds.interfacesCreate)
	ds.forEachPair(ds.syncInterfaceValues)

	// Vlans: definitions first, then each interface's tagged/untagged
	// assignment (which needs the vlans to already exist in netbox).
	// NOTE: Disabled for now, problem with same VLAN in multiple devices (in same site)
	// ds.forEachPair(ds.syncVlans)
	// ds.forEachPair(ds.syncInterfaceVlans)

	// Addresses, order matters: delete before create before update.
	ds.forEachPair(ds.addressesDelete)
	ds.forEachPair(ds.addressesCreate)
	ds.forEachPair(ds.addressesUpdate)

	// Connections between devices, based on LLDP.
	ds.forEachPair(ds.syncConnections)

	// Prefixes for the addresses just synced.
	ds.forEachPair(ds.syncPrefixes)

	// ELINEs as Netbox L2VPNs (type EVPL) + terminations on the local AC
	// interface(s). Runs after interfacesCreate so subinterfaces/SAPs
	// already exist in Netbox and can be terminated on.
	ds.forEachPair(ds.syncELINEs)

	return nil
}

// devicesToSync returns the devices run() should consider: just the named
// device (a single, server-side-filtered factum query) when opts.Name is
// set, or the full device list otherwise - a --name run has no reason to
// fetch every other device from factum first. This is the shallow list
// (no interfaces/addresses - deviceSelected only needs Name/Status/
// Platform); connectDevice fetches each selected device's full snapshot
// separately. deviceSelected still applies its usual filters
// (ignore-list/states/platform) to the result either way.
func (ds *DeviceSync) devicesToSync() ([]*models.Device, error) {
	if ds.opts.Name == "" {
		return ds.factum.GetDevices()
	}
	device, err := ds.factum.GetDeviceByName(ds.opts.Name)
	if err != nil {
		ds.reporter.Emit(jobevent.Error, "device %q not found in factum: %v", ds.opts.Name, err)
		return nil, nil
	}
	return []*models.Device{device}, nil
}

// forEachPair runs fn over every pair, up to syncWorkers at a time, and
// waits for all of them to finish before returning - callers rely on that to
// sequence phases (run()'s delete-before-create-before-update comments).
// Pairs are independent devices, so this is safe in general; the exceptions
// (netboxtool.Cache, EnsurePrefix's check-then-create, syncConnection's
// cross-pair cable) are locked internally rather than handled here.
func (ds *DeviceSync) forEachPair(fn func(*devicePair)) {
	var g errgroup.Group
	g.SetLimit(syncWorkers)
	for i := range ds.pairs {
		pair := &ds.pairs[i]
		g.Go(func() error {
			fn(pair)
			return nil
		})
	}
	g.Wait() //nolint:errcheck // fn never returns an error
}

// deviceSelected reports whether d passes the device-states/ignore-list/
// name/platform filters.
func (ds *DeviceSync) deviceSelected(d *models.Device) bool {
	if d.Name == "" {
		ds.reporter.Emit(jobevent.Error, "factum device id=%d has no name, ignored", d.NetboxID)
		return false
	}
	if ds.opts.Name != "" && d.Name != ds.opts.Name {
		return false
	}
	if contains(ds.cfg.DeviceIgnore, d.Name) {
		return false
	}
	if len(ds.cfg.DeviceStates) > 0 && !contains(ds.cfg.DeviceStates, d.Status) {
		return false
	}
	if ds.opts.Platform != "" && !strings.EqualFold(d.Platform, ds.opts.Platform) {
		return false
	}
	if _, ok := drivers.SupportedPlatforms[strings.ToLower(d.Platform)]; !ok {
		return false
	}
	return true
}

// connectDevices connects to every device in devices concurrently, up to
// syncWorkers at a time, and sets ds.pairs to the ones that succeeded. Each
// device's own SSH/NETCONF round trip dominates connectDevice's cost, so
// this is the same I/O-bound fan-out as forEachPair - see syncWorkers.
// Results are collected into a devices-length slice indexed by position
// rather than appended by whichever goroutine finishes first, so ds.pairs
// ends up in devices' order regardless of which connection was slowest;
// that's not load-bearing for correctness (every later phase fans back out
// over all pairs anyway) but keeps log/output order stable across runs.
func (ds *DeviceSync) connectDevices(devices []*models.Device) {
	results := make([]*devicePair, len(devices))
	var g errgroup.Group
	g.SetLimit(syncWorkers)
	for i, d := range devices {
		g.Go(func() error {
			results[i] = ds.connectDevice(d)
			return nil
		})
	}
	g.Wait() //nolint:errcheck // connectDevice never returns an error

	for _, pair := range results {
		if pair != nil {
			ds.pairs = append(ds.pairs, *pair)
		}
	}
}

// connectDevice connects to nbDevice over its platform's driver and parses
// its running config, returning the resulting pair or nil if either step
// failed - errors (auth failure, connect failure, unknown platform, or the
// device vanishing from factum between the shallow list and here) are
// reported and the device is skipped. nbDevice as passed in is the shallow
// (no interfaces/addresses) copy from devicesToSync; connectDevice re-fetches
// the full snapshot from factum before using it, since that's the one place
// in the run a full per-device fetch happens (see ds.factumDevice).
func (ds *DeviceSync) connectDevice(nbDevice *models.Device) *devicePair {
	slog.Debug("connect", "device", nbDevice.Name)
	full, err := ds.factumDevice(nbDevice.Name)
	if err != nil {
		ds.reporter.Emit(jobevent.Error, "%s: fetch from factum: %v", nbDevice.Name, err)
		return nil
	}
	auth, ok := ds.cfg.Auth[full.Name]
	if !ok {
		auth = ds.cfg.Auth["default"]
	}
	platform := strings.ToLower(full.Platform)
	driver, err := drivers.NewDriver(drivers.DriverParam{
		Name:     drivers.DeviceFQDN(full.Name, ds.cfg.DefaultDomain),
		Platform: platform,
		Username: auth.Username,
		Password: auth.Password,
	})
	if err != nil {
		ds.reporter.Emit(jobevent.Error, "%s: unknown platform %s: %v", full.Name, full.Platform, err)
		return nil
	}
	config, err := driver.GetDeviceConfig()
	if err != nil {
		ds.reporter.Emit(jobevent.Error, "%s: connect/get config: %v", full.Name, err)
		return nil
	}
	return &devicePair{nbDevice: full, config: config, driver: driver}
}

// ----- Interfaces -----

// interfacesDelete removes Netbox interfaces that don't exist in the
// device's parsed config, except ones defined by the device type's
// template: an interface Netbox created because it's part of the device
// type's template is left alone (Netbox, or an admin re-applying the
// template, would just recreate it).
func (ds *DeviceSync) interfacesDelete(pair *devicePair) {
	var toDelete []string
	for _, nbIface := range pair.nbDevice.Interfaces {
		if _, ok := pair.config.InterfacesByName[nbIface.Name]; !ok {
			toDelete = append(toDelete, nbIface.Name)
		}
	}
	sort.Strings(toDelete)
	if len(toDelete) == 0 {
		return
	}

	templateTypes, err := ds.nb.TemplateInterfaceTypes(pair.nbDevice.Manufacturer, pair.nbDevice.ModelName)
	if err != nil {
		ds.reporter.Emit(jobevent.Warning, "%s: fetch device type interface templates: %v", pair.nbDevice.Name, err)
	}

	deleted := map[string]bool{}
	for _, name := range toDelete {
		if _, ok := templateTypes[name]; ok {
			continue
		}
		nbIface := findInterfaceByName(pair.nbDevice.Interfaces, name)
		if nbIface == nil {
			continue
		}
		if !ds.confirmDelete("interface", pair.nbDevice.Name, name, "") {
			continue
		}
		if err := ds.nb.DeleteInterface(pair.nbDevice, nbIface); err == nil {
			deleted[name] = true
		}
	}
	if len(deleted) > 0 {
		kept := pair.nbDevice.Interfaces[:0]
		for _, iface := range pair.nbDevice.Interfaces {
			if !deleted[iface.Name] {
				kept = append(kept, iface)
			}
		}
		pair.nbDevice.Interfaces = kept
	}
}

// interfacesCreate creates Netbox interfaces that exist in the device's
// parsed config but not yet in Netbox. When an interface being created is
// defined by the device type's template, the template's own type
// (admin/vendor-curated per-port data) is used instead of the driver's
// guessed one - see NetboxMgr.TemplateInterfaceTypes.
func (ds *DeviceSync) interfacesCreate(pair *devicePair) {
	nbNames := interfaceNameSet(pair.nbDevice.Interfaces)
	var toCreate []string
	for name := range pair.config.InterfacesByName {
		if !nbNames[name] {
			toCreate = append(toCreate, name)
		}
	}
	sort.Strings(toCreate)

	templateTypes, err := ds.nb.TemplateInterfaceTypes(pair.nbDevice.Manufacturer, pair.nbDevice.ModelName)
	if err != nil {
		ds.reporter.Emit(jobevent.Warning, "%s: fetch device type interface templates: %v", pair.nbDevice.Name, err)
	}

	for _, name := range toCreate {
		iface := pair.config.InterfacesByName[name]
		if templateType, ok := templateTypes[name]; ok && templateType != "" && templateType != iface.Type {
			ifaceCopy := *iface
			ifaceCopy.Type = templateType
			iface = &ifaceCopy
		}
		created, err := ds.nb.CreateInterface(pair.nbDevice, iface)
		if err != nil {
			continue
		}
		// Recorded in-memory (rather than re-fetched) so addressesCreate,
		// which runs right after this phase, can find this interface - see
		// "Replacing RefreshDevice" in the device-sync plan.
		pair.nbDevice.Interfaces = append(pair.nbDevice.Interfaces, models.Interface{
			DeviceID:    pair.nbDevice.ID,
			NetboxID:    created.ID,
			Name:        created.Name,
			Description: iface.Description,
			VRF:         iface.VRF,
			Type:        iface.Type,
		})
	}
}

// syncInterfaceValues updates description/vrf/type/label/parent on existing
// interfaces. VRF is only ever updated when both sides already have one set
// and they differ - never set from empty. Type is compared against the
// device type's own template (not the driver's guessed type -
// syncInterfaceValues only runs against interfaces that already exist in
// Netbox, so whatever created them, admin-curated template data is the more
// trustworthy source of truth) and corrected when it differs. Label/Parent
// are only ever pushed when the driver has an opinion (deviceIface.Label/
// Parent set) - like VRF, never cleared from empty, since most interfaces
// (anything but an SR OS SAP - see srosAddSapInterface) have no opinion on
// either and Netbox may carry admin-set values there.
func (ds *DeviceSync) syncInterfaceValues(pair *devicePair) {
	templateTypes, err := ds.nb.TemplateInterfaceTypes(pair.nbDevice.Manufacturer, pair.nbDevice.ModelName)
	if err != nil {
		ds.reporter.Emit(jobevent.Warning, "%s: fetch device type interface templates: %v", pair.nbDevice.Name, err)
	}

	for i := range pair.nbDevice.Interfaces {
		nbIface := &pair.nbDevice.Interfaces[i]
		deviceIface, ok := pair.config.InterfacesByName[nbIface.Name]
		if !ok {
			continue
		}
		changes := map[string]any{}
		if nbIface.Description != deviceIface.Description {
			changes["description"] = deviceIface.Description
		}
		if deviceIface.VRF != "" && nbIface.VRF != "" && nbIface.VRF != deviceIface.VRF {
			changes["vrf"] = map[string]string{"name": deviceIface.VRF}
		}
		if templateType, ok := templateTypes[nbIface.Name]; ok && templateType != "" && nbIface.Type != templateType {
			changes["type"] = templateType
		}
		if deviceIface.Label != "" && nbIface.Label != deviceIface.Label {
			changes["label"] = deviceIface.Label
		}
		if deviceIface.Parent != "" {
			if id, ok := resolveParentID(pair.nbDevice.Interfaces, deviceIface.Parent); ok {
				if nbIface.ParentID != id {
					changes["parent"] = id
				}
			} else {
				ds.reporter.Emit(jobevent.Warning, "%s: interface %s: parent %s not found in netbox, skipping parent sync", pair.nbDevice.Name, nbIface.Name, deviceIface.Parent)
			}
		}
		if len(changes) > 0 {
			ds.nb.UpdateInterface(pair.nbDevice, nbIface, changes)
		}
	}
}

// ----- Addresses -----

// normalizeVRF returns "" if vrf is in vrfInGlobal (addresses in those VRFs
// are allocated in the global table to avoid duplicates - see the
// vrf_in_global config comment), otherwise vrf unchanged.
func normalizeVRF(vrf string, vrfInGlobal []string) string {
	if contains(vrfInGlobal, vrf) {
		return ""
	}
	return vrf
}

// addressMatch reports whether two addresses (one from Netbox, one parsed
// from the device) represent the same address+VRF.
func addressMatch(nbAddr netip.Prefix, nbVRF string, devAddr netip.Prefix, devVRF string, vrfInGlobal []string) bool {
	if nbAddr != devAddr {
		return false
	}
	return normalizeVRF(nbVRF, vrfInGlobal) == normalizeVRF(devVRF, vrfInGlobal)
}

// addressesDelete removes Netbox addresses that don't exist (address+VRF)
// in the device's parsed config.
func (ds *DeviceSync) addressesDelete(pair *devicePair) {
	for i := range pair.nbDevice.Interfaces {
		nbIface := &pair.nbDevice.Interfaces[i]
		deviceIface := pair.config.InterfacesByName[nbIface.Name]
		deletedIDs := map[uint]bool{}
		for j := range nbIface.Addresses {
			nbAddr := &nbIface.Addresses[j]
			nbPrefix, err := netip.ParsePrefix(nbAddr.Address)
			if err != nil {
				continue
			}
			found := false
			if deviceIface != nil {
				for _, devAddr := range deviceIface.IPAddresses {
					if addressMatch(nbPrefix, nbAddr.VRF, devAddr.Address, devAddr.VRF, ds.cfg.VRFInGlobal) {
						found = true
						break
					}
				}
			}
			if found {
				continue
			}
			ds.reportAddressDeleteReason(pair.nbDevice.Name, nbIface, nbAddr, deviceIface)
			if !ds.confirmDelete("address", pair.nbDevice.Name, nbAddr.Address, "interface "+nbIface.Name) {
				continue
			}
			if err := ds.nb.DeleteAddress(pair.nbDevice, nbIface, nbAddr); err == nil {
				deletedIDs[nbAddr.NetboxID] = true
			}
		}
		if len(deletedIDs) > 0 {
			kept := nbIface.Addresses[:0]
			for _, addr := range nbIface.Addresses {
				if !deletedIDs[addr.NetboxID] {
					kept = append(kept, addr)
				}
			}
			nbIface.Addresses = kept
		}
	}
}

// reportAddressDeleteReason explains, before addressesDelete removes it, why
// nbAddr (on nbIface) didn't match anything in deviceIface's parsed
// addresses - deviceIface is nil when the interface itself no longer exists
// in the device's config. Listing the device's own addresses (with their
// VRFs) alongside Netbox's is what makes a VRF-string mismatch between the
// two sides (as opposed to a genuinely removed address) obvious from the
// log alone, rather than something that has to be debugged after the fact.
func (ds *DeviceSync) reportAddressDeleteReason(deviceName string, nbIface *models.Interface, nbAddr *models.Address, deviceIface *drivers.Interface) {
	nbVRF := nbAddr.VRF
	if nbVRF == "" {
		nbVRF = "-"
	}
	if deviceIface == nil {
		ds.reporter.Emit(jobevent.Info, "%s: interface %s: no longer exists in the device's config, deleting netbox address %s (vrf=%s)",
			deviceName, nbIface.Name, nbAddr.Address, nbVRF)
		return
	}
	if len(deviceIface.IPAddresses) == 0 {
		ds.reporter.Emit(jobevent.Info, "%s: interface %s: device has no addresses configured, deleting netbox address %s (vrf=%s)",
			deviceName, nbIface.Name, nbAddr.Address, nbVRF)
		return
	}
	have := make([]string, 0, len(deviceIface.IPAddresses))
	for _, devAddr := range deviceIface.IPAddresses {
		vrf := devAddr.VRF
		if vrf == "" {
			vrf = "-"
		}
		have = append(have, fmt.Sprintf("%s (vrf=%s)", devAddr.Address, vrf))
	}
	ds.reporter.Emit(jobevent.Info, "%s: interface %s: netbox address %s (vrf=%s) not found among device's addresses [%s], deleting",
		deviceName, nbIface.Name, nbAddr.Address, nbVRF, strings.Join(have, ", "))
}

// addressesCreate creates Netbox addresses that exist on the device but not
// yet in Netbox
func (ds *DeviceSync) addressesCreate(pair *devicePair) {
	for _, deviceIface := range pair.config.Interfaces {
		nbIface := findInterfaceByName(pair.nbDevice.Interfaces, deviceIface.Name)
		if nbIface == nil {
			ds.reporter.Emit(jobevent.Error, "%s: netbox interface %s not found for address sync", pair.nbDevice.Name, deviceIface.Name)
			continue
		}
		for _, devAddr := range deviceIface.IPAddresses {
			found := false
			for _, nbAddr := range nbIface.Addresses {
				nbPrefix, err := netip.ParsePrefix(nbAddr.Address)
				if err != nil {
					continue
				}
				if addressMatch(nbPrefix, nbAddr.VRF, devAddr.Address, devAddr.VRF, ds.cfg.VRFInGlobal) {
					found = true
					break
				}
			}
			if found {
				continue
			}
			extra := map[string]any{}
			var role, vrf string
			if strings.Contains(strings.ToLower(nbIface.Description), "anycast") {
				extra["role"] = "anycast"
				role = "anycast"
			}
			if devAddr.VRF != "" && !contains(ds.cfg.VRFInGlobal, devAddr.VRF) {
				extra["vrf"] = map[string]string{"name": devAddr.VRF}
				vrf = devAddr.VRF
			}
			created, err := ds.nb.CreateAddress(pair.nbDevice, nbIface, devAddr.Address.String(), extra)
			if err != nil {
				continue
			}
			// Recorded in-memory - see interfacesCreate's comment.
			nbIface.Addresses = append(nbIface.Addresses, models.Address{
				InterfaceID: nbIface.ID,
				NetboxID:    created.NetboxID,
				Address:     devAddr.Address.String(),
				VRF:         vrf,
				Role:        role,
			})
		}
	}
}

// addressesUpdate toggles an address's "anycast" role based on whether its
// interface's description mentions "anycast". nbAddr is a pointer into
// pair.nbDevice, so setting nbAddr.Role after a successful update keeps
// pair.nbDevice consistent with Netbox for any later phase - no re-fetch
// needed.
func (ds *DeviceSync) addressesUpdate(pair *devicePair) {
	for i := range pair.nbDevice.Interfaces {
		nbIface := &pair.nbDevice.Interfaces[i]
		anycast := strings.Contains(strings.ToLower(nbIface.Description), "anycast")
		for j := range nbIface.Addresses {
			nbAddr := &nbIface.Addresses[j]
			isAnycast := strings.EqualFold(nbAddr.Role, "anycast")
			switch {
			case isAnycast && !anycast:
				if err := ds.nb.UpdateAddress(pair.nbDevice, nbIface, nbAddr, map[string]any{"role": nil}); err == nil {
					nbAddr.Role = ""
				}
			case !isAnycast && nbAddr.Role == "" && anycast:
				if err := ds.nb.UpdateAddress(pair.nbDevice, nbIface, nbAddr, map[string]any{"role": "anycast"}); err == nil {
					nbAddr.Role = "anycast"
				}
			}
		}
	}
}

// ----- Connections (LLDP-based cables) -----

// syncConnections uses LLDP to find connected devices and syncs Netbox
// cables to match.
func (ds *DeviceSync) syncConnections(pair *devicePair) {
	neighbors, err := pair.driver.GetNeighbors()
	if err != nil {
		ds.reporter.Emit(jobevent.Warning, "%s: get neighbors: %v", pair.nbDevice.Name, err)
		return
	}
	for _, n := range neighbors {
		remoteName := strings.ToLower(n.RemoteName)
		if remoteName == "" {
			ds.reporter.Emit(jobevent.Warning, "%s: interface %s: LLDP remote device did not return any name", pair.nbDevice.Name, n.LocalInterface)
			continue
		}
		if i := strings.Index(remoteName, "."); i >= 0 {
			remoteName = remoteName[:i] // FQDN -> hostname
		}

		remoteDevice, err := ds.factumDevice(remoteName)
		if err != nil {
			continue // unknown remote device, ignore
		}

		localIf := findInterfaceByName(pair.nbDevice.Interfaces, n.LocalInterface)
		if localIf == nil {
			ds.reporter.Emit(jobevent.Warning, "%s: LLDP local interface %s does not exist in netbox", pair.nbDevice.Name, n.LocalInterface)
			continue
		}
		// some devices report their interface shortname over LLDP instead of
		// the full name that exists in Netbox - expand it per-platform.
		remoteInterface := expandInterfaceName(remoteDevice.Platform, n.RemoteInterface)

		remoteIf := findInterfaceByName(remoteDevice.Interfaces, remoteInterface)
		if remoteIf == nil {
			ds.reporter.Emit(jobevent.Warning, "%s: interface %s: LLDP remote device %s interface %s does not exist in netbox", pair.nbDevice.Name, n.LocalInterface, n.RemoteName, remoteInterface)
			continue
		}
		ds.syncConnection(pair.nbDevice, remoteDevice, localIf, remoteIf)
	}
}

// syncConnection ensures localIf and remoteIf are connected by exactly one
// cable, creating one if neither side has one, deleting a stale duplicate
// if both sides disagree, and repairing whichever termination is wrong
// otherwise.
func (ds *DeviceSync) syncConnection(device, remoteDevice *models.Device, localIf, remoteIf *models.Interface) {
	ds.connMu.Lock()
	defer ds.connMu.Unlock()

	if ds.shouldSkipLLDPCabling(device, remoteDevice, localIf, remoteIf) {
		ds.reporter.Emit(jobevent.Info, "%s: interface %s: LLDP neighbor ignored: optical/fiber handoff", device.Name, localIf.Name)
		return
	}

	var cable *netboxtool.NBCable
	var err error
	switch {
	case localIf.CableID != 0:
		cable, err = ds.nb.api.GetCable(localIf.CableID)
		if err != nil {
			ds.reporter.EmitErr(err)
			return
		}
	case remoteIf.CableID != 0:
		cable, err = ds.nb.api.GetCable(remoteIf.CableID)
		if err != nil {
			ds.reporter.EmitErr(err)
			return
		}
	}

	if cable != nil && cable.Label != "lldp" {
		// Manual / optical plant cable — never retarget or delete.
		if !cableJoins(cable, localIf.NetboxID, remoteIf.NetboxID) {
			ds.reporter.Emit(jobevent.Warning, "%s: interface %s: LLDP says %s %s but a manual cable already exists — leaving it",
				device.Name, localIf.Name, remoteDevice.Name, remoteIf.Name)
		}
		return
	}

	if cable != nil && remoteIf.CableID != 0 && remoteIf.CableID != localIf.CableID {
		if remoteCable, err := ds.nb.api.GetCable(remoteIf.CableID); err == nil && remoteCable != nil {
			if remoteCable.Label == "lldp" {
				ds.nb.DeleteCable(remoteCable)
			}
		}
	}

	if cable == nil {
		ds.nb.CreateCable(device, remoteDevice, localIf, remoteIf)
		return
	}

	local, remote := 0, 0
	if cable.AInterface == localIf.NetboxID {
		local = 1
	}
	if cable.AInterface == remoteIf.NetboxID {
		remote = 1
	}
	if cable.BInterface == localIf.NetboxID {
		local = 2
	}
	if cable.BInterface == remoteIf.NetboxID {
		remote = 2
	}

	switch {
	case local != 0 && remote != 0:
		// both sides already correct
	case local != 0:
		ds.nb.SetConnectionTermination(remoteDevice, cable, 3-local, remoteIf)
	case remote != 0:
		ds.nb.SetConnectionTermination(device, cable, 3-remote, localIf)
	}
}

func cableJoins(cable *netboxtool.NBCable, a, b uint) bool {
	return (cable.AInterface == a && cable.BInterface == b) ||
		(cable.AInterface == b && cable.BInterface == a)
}

func (ds *DeviceSync) shouldSkipLLDPCabling(localDev, remoteDev *models.Device, localIf, remoteIf *models.Interface) bool {
	if models.IsOpticalKind(localDev.OpticalKind) || models.IsOpticalKind(remoteDev.OpticalKind) {
		return true
	}
	if ds.interfaceHasOpticalPort(localIf.ID) || ds.interfaceHasOpticalPort(remoteIf.ID) {
		return true
	}
	if ds.cabledToOptical(localIf) || ds.cabledToOptical(remoteIf) {
		return true
	}
	return false
}

func (ds *DeviceSync) interfaceHasOpticalPort(interfaceID uint) bool {
	if interfaceID == 0 {
		return false
	}
	// Best-effort: factum snapshot may not include OpticalPort; look at
	// whatever we have loaded. Device-sync talks to Factum over HTTP and
	// only has models.Interface — OpticalPort is not on that struct.
	// Skip-by-port is applied when the factum device payload later grows
	// a Ports map; for now kind + far-end cable are the signals.
	return false
}

func (ds *DeviceSync) cabledToOptical(iface *models.Interface) bool {
	if iface == nil || iface.CableID == 0 {
		return false
	}
	cable, err := ds.nb.api.GetCable(iface.CableID)
	if err != nil || cable == nil {
		return false
	}
	other := cable.AInterface
	if other == iface.NetboxID {
		other = cable.BInterface
	}
	// Resolve the far-end device from the in-memory cache if we can.
	ds.deviceCacheMu.Lock()
	defer ds.deviceCacheMu.Unlock()
	for _, d := range ds.deviceCache {
		if !models.IsOpticalKind(d.OpticalKind) {
			continue
		}
		for _, i := range d.Interfaces {
			if i.NetboxID == other {
				return true
			}
		}
	}
	return false
}

// ----- Vlans -----

// vlanIntsEqual reports whether a and b contain the same VIDs, regardless
// of order - TaggedVLANs isn't guaranteed to come back from Netbox/the
// device parsers in a stable order.
func vlanIntsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[int]int, len(a))
	for _, v := range a {
		set[v]++
	}
	for _, v := range b {
		set[v]--
	}
	for _, count := range set {
		if count != 0 {
			return false
		}
	}
	return true
}

// syncVlans ensures every VLAN referenced by the device's parsed config -
// both explicit GlobalVLANs entries and any VID an interface merely
// references as its untagged/tagged VLAN without a matching global
// declaration (e.g. VRP's "port trunk allow-pass vlan" can list VIDs never
// defined via a "vlan" block) - exists in the configured global Netbox VLAN
// group (Settings.DeviceSyncVlanGroupName), creating or renaming as needed.
// A no-op if VLAN sync is disabled or the device has no global VLANs
// (bridge-domain platforms never populate GlobalVLANs - see
// drivers.Interface's doc comment).
func (ds *DeviceSync) syncVlans(pair *devicePair) {
	if ds.cfg.VlanGroupName == "" {
		return
	}
	seen := map[int]bool{}
	ensure := func(vid int, name string) {
		if vid == 0 || seen[vid] {
			return
		}
		seen[vid] = true
		if name == "" {
			// Netbox rejects a blank vlan name; VRP's "vlan batch <id-list>"
			// form declares a vlan by ID alone with no name (see
			// vrpParseGlobalVlans), and a VID only ever seen on a trunk's
			// allowed list has no name at all - fall back to a generated
			// one rather than failing the vlan outright.
			name = fmt.Sprintf("VLAN-%d", vid)
		}
		if _, err := ds.nb.EnsureVlan(vid, name); err != nil {
			ds.reporter.Emit(jobevent.Warning, "%s: sync vlan %d: %v", pair.nbDevice.Name, vid, err)
		}
	}
	for _, vlan := range pair.config.GlobalVLANs {
		ensure(vlan.ID, vlan.Name)
	}
	for _, iface := range pair.config.Interfaces {
		ensure(iface.UntaggedVLAN, "")
		for _, vid := range iface.TaggedVLANs {
			ensure(vid, "")
		}
	}
}

// syncInterfaceVlans pushes each interface's untagged/tagged VLAN
// assignment to Netbox when it differs from what factum last synced from
// Netbox (nbIface.UntaggedVLAN/TaggedVLANs). A no-op if VLAN sync is
// disabled.
func (ds *DeviceSync) syncInterfaceVlans(pair *devicePair) {
	if ds.cfg.VlanGroupName == "" {
		return
	}
	for i := range pair.nbDevice.Interfaces {
		nbIface := &pair.nbDevice.Interfaces[i]
		deviceIface, ok := pair.config.InterfacesByName[nbIface.Name]
		if !ok {
			continue
		}

		// A q-in-q interface ("dot1q-tunnel" on VRP/EOS, same SwitchportMode
		// value both drivers' generic "port link-type"/"switchport mode"
		// parsing already captures verbatim) carries its outer/S-VLAN tag
		// in Netbox's qinq_svlan field instead of untagged_vlan - the two
		// are mutually exclusive in Netbox, same UntaggedVLAN source value
		// either way (see netboxtool's parseDevices, which merges Netbox's
		// untagged_vlan/qinq_svlan back into one field on read).
		vlanField := "untagged_vlan"
		if deviceIface.SwitchportMode == "dot1q-tunnel" {
			vlanField = "qinq_svlan"
		}

		changes := map[string]any{}
		untaggedChanged := false
		if nbIface.UntaggedVLAN != deviceIface.UntaggedVLAN {
			if deviceIface.UntaggedVLAN == 0 {
				changes[vlanField] = nil
				untaggedChanged = true
			} else if id, ok := ds.nb.VlanNetboxID(deviceIface.UntaggedVLAN); ok {
				changes[vlanField] = id
				untaggedChanged = true
			} else {
				ds.reporter.Emit(jobevent.Warning, "%s: interface %s: vlan %d not synced to netbox, skipping",
					pair.nbDevice.Name, nbIface.Name, deviceIface.UntaggedVLAN)
			}
		}
		// Only send tagged_vlans if every tagged VID resolved to a Netbox
		// ID - a partial list would otherwise silently drop the
		// unresolved VLANs from the interface in Netbox (and, once
		// recorded in-memory below, look "in sync" on every later run).
		taggedChanged := false
		if !vlanIntsEqual(nbIface.TaggedVLANs, deviceIface.TaggedVLANs) {
			ids := make([]uint, 0, len(deviceIface.TaggedVLANs))
			resolved := true
			for _, vid := range deviceIface.TaggedVLANs {
				if id, ok := ds.nb.VlanNetboxID(vid); ok {
					ids = append(ids, id)
				} else {
					resolved = false
					ds.reporter.Emit(jobevent.Warning, "%s: interface %s: tagged vlan %d not synced to netbox, skipping tagged_vlans update",
						pair.nbDevice.Name, nbIface.Name, vid)
				}
			}
			if resolved {
				changes["tagged_vlans"] = ids
				taggedChanged = true
			}
		}
		if !untaggedChanged && !taggedChanged {
			continue
		}
		// Netbox rejects untagged_vlan/tagged_vlans unless the interface's
		// own "mode" is set to a value that supports them ("Interface mode
		// does not support untagged vlan") - mode has no factum-side field
		// to diff against, so it's just resent alongside every vlan change,
		// mapped from the same SwitchportMode driven the vlan values
		// themselves. "" (not a switchport any more) clears it.
		changes["mode"] = models.SwitchportModeToNetboxMode(deviceIface.SwitchportMode)
		if err := ds.nb.UpdateInterface(pair.nbDevice, nbIface, changes); err == nil {
			if untaggedChanged {
				nbIface.UntaggedVLAN = deviceIface.UntaggedVLAN
			}
			if taggedChanged {
				nbIface.TaggedVLANs = deviceIface.TaggedVLANs
			}
		}
	}
}

// ----- Prefixes -----

// syncPrefixes ensures a Netbox prefix exists for every network containing
// an address just synced from this device, skipping host-only prefixes
// (/32, /128).
func (ds *DeviceSync) syncPrefixes(pair *devicePair) {
	seen := map[string]bool{}
	for _, iface := range pair.config.Interfaces {
		for _, addr := range iface.IPAddresses {
			network := addr.Address.Masked()
			bits := network.Bits()
			if (network.Addr().Is4() && bits == 32) || (network.Addr().Is6() && bits == 128) {
				continue
			}
			key := network.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			ds.nb.EnsurePrefix(key)
		}
	}
}

// ----- ELINEs (Netbox L2VPN type EVPL) -----

// elineLocalSide is what one device's parsed ELINE contributes to Netbox:
// zero or more attachment-circuit interface names (EOS/XR *Interface, SR OS
// SAP id string) plus an optional pseudowire identifier. A cross-device
// ELINE has one interface + one PWID; a same-device patch has two
// interfaces and no PWID.
type elineLocalSide struct {
	ifaceNames []string
	pwid       int
}

// elineLocalSideOf extracts the local AC interface name(s) and optional
// pseudowire ID from a parsed drivers.ELINE. Conn1/Conn2 are typed as any
// (*Interface, string SAP id, or *Pseudowire) - see drivers.ELINE.
func elineLocalSideOf(eline *drivers.ELINE) elineLocalSide {
	var side elineLocalSide
	for _, conn := range []any{eline.Conn1, eline.Conn2} {
		if name, ok := elineInterfaceName(conn); ok {
			side.ifaceNames = append(side.ifaceNames, name)
			continue
		}
		if pw := elinePseudowire(conn); pw != nil && pw.PWID > 0 {
			side.pwid = pw.PWID
		}
	}
	return side
}

// elineInterfaceName returns the interface/SAP name for an ELINE connection
// leg, if conn is an attachment circuit rather than a pseudowire.
func elineInterfaceName(conn any) (string, bool) {
	switch c := conn.(type) {
	case *drivers.Interface:
		if c != nil && c.Name != "" {
			return c.Name, true
		}
	case string:
		if c != "" {
			return c, true
		}
	}
	return "", false
}

// elinePseudowire returns conn if it is a *Pseudowire, else nil.
func elinePseudowire(conn any) *drivers.Pseudowire {
	pw, _ := conn.(*drivers.Pseudowire)
	return pw
}

// syncELINEs ensures each ELINE parsed from the device's running config
// exists in Netbox as an L2VPN of type "evpl", with an L2VPNTermination on
// every local AC interface that is already present in Netbox (created by
// the earlier interfacesCreate phase). Cross-device ELINEs only contribute
// this device's end; the peer's termination is added when that peer is
// synced. Does not delete L2VPNs or terminations that no longer appear on
// the device - those may be owned by factum services (web ApiServiceElineUpdate)
// or by the other endpoint, and a partial --name run must not tear them down.
func (ds *DeviceSync) syncELINEs(pair *devicePair) {
	if len(pair.config.ELINEs) == 0 {
		return
	}
	// Stable order for logs/tests.
	names := make([]string, 0, len(pair.config.ELINEs))
	for name := range pair.config.ELINEs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		eline := pair.config.ELINEs[name]
		if eline == nil || eline.Name == "" {
			continue
		}
		side := elineLocalSideOf(eline)
		if len(side.ifaceNames) == 0 {
			ds.reporter.Emit(jobevent.Warning, "%s: eline %q has no local interface attachment, skipping", pair.nbDevice.Name, eline.Name)
			continue
		}

		l2vpn, err := ds.nb.EnsureL2VPNEVPL(pair.nbDevice.Name, eline.Name, side.pwid)
		if err != nil {
			continue
		}

		terms, err := ds.nb.GetL2VPNTerminations(l2vpn.NetboxID)
		if err != nil {
			ds.reporter.Emit(jobevent.Error, "%s: l2vpn %q: list terminations: %v", pair.nbDevice.Name, l2vpn.Name, err)
			continue
		}

		for _, ifaceName := range side.ifaceNames {
			nbIface := findInterfaceByName(pair.nbDevice.Interfaces, ifaceName)
			if nbIface == nil {
				ds.reporter.Emit(jobevent.Warning, "%s: eline %q: interface %s not in netbox yet, skipping termination", pair.nbDevice.Name, eline.Name, ifaceName)
				continue
			}
			if nbIface.NetboxID == 0 {
				ds.reporter.Emit(jobevent.Warning, "%s: eline %q: interface %s has no netbox id, skipping termination", pair.nbDevice.Name, eline.Name, ifaceName)
				continue
			}
			_ = ds.nb.EnsureL2VPNTermination(pair.nbDevice.Name, l2vpn.Name, l2vpn.NetboxID, nbIface.NetboxID, ifaceName, &terms)
		}
	}
}

// ----- Shared helpers -----

// interfaceRename maps a shortname pattern (as reported by LLDP) to the
// full interface name Netbox expects, e.g. "Te0/0/27" -> "TenGigabitEthernet0/0/27".
// Repl follows regexp.ReplaceAllString syntax ($1 etc).
type interfaceRename struct {
	re   *regexp.Regexp
	repl string
}

// interfaceShortNames holds, per platform (lowercased), the ordered list of
// shortname patterns to try. First match wins. Add an entry here whenever a
// new platform's LLDP neighbor output is observed using a shortname that
// doesn't match its Netbox interface name.
var interfaceShortNames = map[string][]interfaceRename{
	"ios-xe": {
		{regexp.MustCompile(`^Te(\d.*)$`), "TenGigabitEthernet$1"},
		{regexp.MustCompile(`^Gi(\d.*)$`), "GigabitEthernet$1"},
		{regexp.MustCompile(`^Fa(\d.*)$`), "FastEthernet$1"},
	},
}

// expandInterfaceName converts a platform-specific LLDP interface shortname
// to the full name used in Netbox, if a rule matches. Returns name
// unchanged if no rule applies.
func expandInterfaceName(platform, name string) string {
	for _, r := range interfaceShortNames[strings.ToLower(platform)] {
		if r.re.MatchString(name) {
			return r.re.ReplaceAllString(name, r.repl)
		}
	}
	return name
}

func findInterfaceByName(interfaces []models.Interface, name string) *models.Interface {
	for i := range interfaces {
		if interfaces[i].Name == name {
			return &interfaces[i]
		}
	}
	return nil
}

// resolveParentID looks up parentName (a drivers.Interface.Parent value,
// e.g. an SR OS SAP's physical port - see srosAddSapInterface) against
// interfaces by name and returns its Netbox ID - used both when creating an
// interface (NetboxMgr.CreateInterface) and when correcting one that
// already exists (syncInterfaceValues). false if parentName isn't present
// yet (e.g. the port itself hasn't been synced to Netbox).
func resolveParentID(interfaces []models.Interface, parentName string) (uint, bool) {
	parent := findInterfaceByName(interfaces, parentName)
	if parent == nil {
		return 0, false
	}
	return parent.NetboxID, true
}

func interfaceNameSet(interfaces []models.Interface) map[string]bool {
	set := make(map[string]bool, len(interfaces))
	for _, iface := range interfaces {
		set[iface.Name] = true
	}
	return set
}

func contains(list []string, val string) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}

// confirmDelete asks for interactive y/n confirmation before a destructive
// change - skipped (treated as confirmed) when Unattended is set or stdin
// isn't an interactive
// terminal, since there's nowhere to put a prompt in either case (a --job
// run's stdout is JSON lines, and a non-interactive stdin has no user to
// answer). detail, if non-empty, is appended in parentheses - addressesDelete
// uses it to name the interface an address lives on, since an address alone
// doesn't say which interface it'd be removed from.
func (ds *DeviceSync) confirmDelete(kind, deviceName, name, detail string) bool {
	if ds.opts.Unattended {
		return true
	}
	stat, err := os.Stdin.Stat()
	if err != nil || (stat.Mode()&os.ModeCharDevice) == 0 {
		return true
	}
	prompt := fmt.Sprintf("Delete %s %q on device %s", kind, name, deviceName)
	if detail != "" {
		prompt += " (" + detail + ")"
	}
	fmt.Fprintf(os.Stdout, "%s? [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
