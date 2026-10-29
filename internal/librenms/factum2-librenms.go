package librenms

// Sync devices in librenms, based on factum
// If device is missing in librenms, create it
// If device exist in librenms but not in factum, quarantine it
// (disabled + ignore, display stamped with the scheduled date) and
// delete it after Settings.LibrenmsDelayedDeleteDays, or on the next
// run if the pending row has ForceDelete set.
// All LibreNMS hostnames must already be IPv4 or IPv6 addresses; Sync
// aborts otherwise (run normalize-hostnames first).
// A device created here uses the primary IPv4 as its hostname and the
// Factum name with Settings.DefaultDomain appended as its display name.
// Renames and field updates address the LibreNMS device id stored in
// the NetBox destination custom field (librenms:<id>).
// For each linked device, check and adjust librenms
// - device
//     enabled flag
//     display name
//     location name
//     location coordinates (Factum latitude/longitude)
//     parents
// - device interfaces
// 	   ignore flag
//

// note:
// - factum uses "enabled" true/false for devices and interfaces
//   librenms uses "Ignore alerts" true/false for devices and interfaces

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/joho/godotenv"
)

type factumPendingStore struct {
	client *factum.FactumClient
}

func (s factumPendingStore) Upsert(row *models.LibrenmsPendingDelete) (*models.LibrenmsPendingDelete, error) {
	return s.client.UpsertLibrenmsPendingDelete(row)
}

func (s factumPendingStore) Remove(deviceID int) error {
	return s.client.DeleteLibrenmsPendingDelete(deviceID)
}

// splitLines turns a newline-separated Settings field (e.g.
// Settings.LibrenmsRolesEnabled) into its non-empty, trimmed lines.
func splitLines(list string) []string {
	var out []string
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

type FactumLibrenmsClient struct {
	Config                     *util.ConfigFactum
	DB                         *util.ConfigDB
	Factum                     *factum.FactumClient
	Librenms                   *LibrenmsClient
	Netbox                     *netboxtool.NetboxClient
	FactumDevices              []*models.Device
	LibrenmsDevices            []*LibrenmsDevice   // cache of devices
	LibrenmsPorts              []*LibrenmsPort     // cache of ports
	LibrenmsLocations          []*LibrenmsLocation // cache of locations
	RolesEnabledCompiled       []*regexp.Regexp
	InterfacesDisabledCompiled []*regexp.Regexp
}

// NewFactumLibrenmsClient fetches the LibreNMS config (REST API URL/key and
// the Sync regex-filter maps) from the primary over REST - config.Librenms
// is never used, since factum2-librenms-cli typically runs on a different
// host than the primary and has no local librenms config of its own (see
// util.ConfigLibrenms's doc comment).
func NewFactumLibrenmsClient(config *util.ConfigFactum) (*FactumLibrenmsClient, error) {
	client := new(FactumLibrenmsClient)
	client.Config = config

	var err error
	client.Librenms, err = RemoteClient(config)
	if err != nil {
		return nil, err
	}
	for _, r := range splitLines(client.Librenms.P.RolesEnabled) {
		client.RolesEnabledCompiled = append(client.RolesEnabledCompiled, regexp.MustCompile(r))
	}
	for _, r := range splitLines(client.Librenms.P.InterfacesDisabled) {
		client.InterfacesDisabledCompiled = append(client.InterfacesDisabledCompiled, regexp.MustCompile(r))
	}

	// get the librenms mysql credentials - assumes this app runs on the
	// librenms server. The credentials can be read straight out of librenms's
	// own .env file rather than configured separately
	// (see util.ConfigLibrenms's doc comment). LIBRENMS_ENV_FILE overrides
	// the default search path (used by the compose lab on a laptop).
	dbcfg, err := readLibrenmsDBConfig()
	if err != nil {
		return nil, err
	}
	client.DB = dbcfg
	client.Librenms.DBConfig = client.DB
	return client, nil
}

func librenmsEnvFileCandidates() []string {
	if p := strings.TrimSpace(os.Getenv("LIBRENMS_ENV_FILE")); p != "" {
		return []string{p}
	}
	return []string{"/opt/librenms/.env", "/opt/librenms-docker/.env"}
}

func dbConfigFromLibrenmsEnv(env map[string]string) *util.ConfigDB {
	if database := env["MYSQL_DATABASE"]; database != "" {
		port := env["MYSQL_PORT"]
		if port == "" {
			port = "3306"
		}
		host := env["MYSQL_HOST"]
		if host == "" {
			host = "127.0.0.1"
		}
		return &util.ConfigDB{
			Host:     host,
			Database: database,
			Port:     port,
			User:     env["MYSQL_USER"],
			Pass:     env["MYSQL_PASSWORD"],
		}
	}
	port := env["DB_PORT"]
	if port == "" {
		port = "3306"
	}
	return &util.ConfigDB{
		Host:     env["DB_HOST"],
		Database: env["DB_DATABASE"],
		Port:     port,
		User:     env["DB_USERNAME"],
		Pass:     env["DB_PASSWORD"],
	}
}

func readLibrenmsDBConfig() (*util.ConfigDB, error) {
	for _, src := range librenmsEnvFileCandidates() {
		if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		env, err := godotenv.Read(src)
		if err != nil {
			return nil, err
		}
		cfg := dbConfigFromLibrenmsEnv(env)
		if cfg.Database == "" {
			return nil, fmt.Errorf("librenms env file %s has no MYSQL_DATABASE or DB_DATABASE", src)
		}
		return cfg, nil
	}
	return nil, errors.New("cannot find librenms MYSQL settings")
}

func (fl *FactumLibrenmsClient) RefreshLibrenmsDevices() error {
	var err error
	fl.LibrenmsDevices, err = fl.Librenms.DevicesGet()
	return err
}
func (fl *FactumLibrenmsClient) RefreshFactumDevices() error {
	var err error
	fl.FactumDevices, err = fl.Factum.GetDevices()
	return err
}

func (fl *FactumLibrenmsClient) findFactumInterface(name string, factumInterface *[]models.Interface) *models.Interface {
	for _, factumInterface := range *factumInterface {
		if name == factumInterface.Name {
			return &factumInterface
		}
	}
	return nil
}

func (fl *FactumLibrenmsClient) syncInterfaces(librenmsDevice *LibrenmsDevice, factumDevice *models.Device, reporter jobevent.Reporter) error {
	librenmsInterfaces, err := fl.Librenms.PortsGet(librenmsDevice.DeviceID)
	if err != nil {
		return err
	}

	if len(librenmsInterfaces) < 1 {
		// Not an error: some devices (e.g. UPSes with poor SNMP support)
		// legitimately have zero interfaces in Librenms but still report
		// other useful data (battery charge, etc).
		return nil
	}

	factumInterfaces := factumDevice.Interfaces

	// Default for all ports comes from field "alarm_interfaces"
	//
	// if alarm_interfaces is True, ALL ports by default generates alarms
	//   if a port should not send alarms, it needs to be configured
	// if alarm_interfaces is False, NO ports by default generates alarms
	//   if a port should send alarms, it needs to be configured
	//
	// If device is from netbox
	//    ignore = netbox custom field "alarm_interfaces"
	//    if interface has tag uplink or tag "librenms_alert_enable", ignore = 0
	//    if interface has tag "librenms_alert_disabled", ignore = 1
	//
	// If device is from BECS
	//    ignore = 1
	//    if interface has role uplink.* then ignore = 0

	for _, librenmsInterface := range librenmsInterfaces {
		// find the corresponding factum-device, based on name - factum does not know the port_id
		fi := fl.findFactumInterface(librenmsInterface.IfName, &factumInterfaces)
		if fi == nil {
			fi = fl.findFactumInterface(librenmsInterface.IfAlias, &factumInterfaces)
			if fi == nil {
				fi = fl.findFactumInterface(librenmsInterface.IfDescr, &factumInterfaces)
			}
		}

		var ignore int
		if factumDevice.CfAlarmInterfaces {
			ignore = 0
		} else {
			ignore = 1
		}
		role := ""

		if fi != nil {
			if fi.IsTag("uplink") {
				ignore = 0
			}
			if fi.IsTag("librenms_alarm_disable") {
				ignore = 1
			}
			if fi.IsTag("librenms_alarm_enable") {
				ignore = 0
			}

			role = fi.CfRole
			if role != "" {
				for _, r := range fl.RolesEnabledCompiled {
					if r.MatchString(role) {
						ignore = 0
					}
				}
				for _, r := range fl.InterfacesDisabledCompiled {
					if r.MatchString(fi.Name) {
						ignore = 1
					}
				}
			}
		}
		if librenmsInterface.Ignore != ignore {
			reporter.Emit(jobevent.Info, "%s/%s: updating librenms port %s: ignore=%d", librenmsDevice.Display, librenmsDevice.Hostname, librenmsInterface.IfName, ignore)
			fl.Librenms.PortsUpdateIgnore(librenmsInterface.PortID, ignore)
		}
	}
	return nil
}

// Get a device from Librenms. Try name as IP address first, then as string
// def get_device_from_librenms_device(devices, librenms_device):
func (fl *FactumLibrenmsClient) getDeviceFromLibrenmsDevice(factumDevices *models.Device, librenmsDevice *LibrenmsDevice) (*LibrenmsDevice, error) {
	// try:
	//     librenms_addr = ipaddress.ip_address(librenms_device.hostname)
	//     # librenms_device.hostname is an IP address
	//     for device in devices.values():
	//         if device.primary_ip4:
	//             netbox_addr = device.primary_ip4.address.split("/")[0]
	//             # print(f"{librenms_addr=} {netbox_addr=}")
	//             if librenms_device.hostname == netbox_addr:
	//                 return device, netbox_addr
	// except ValueError:
	//     # librenms_device.hostname is not an IP address, fall back to name
	//     for device in devices.values():
	//         if device.name == librenms_device.hostname:
	//             return device, None
	return nil, nil
}

// librenmsDisplayName is the display name written to LibreNMS: the Factum
// device name, with Settings.DefaultDomain appended when the name has no
// domain of its own. An address is left unchanged.
func (fl *FactumLibrenmsClient) librenmsDisplayName(name string) string {
	domain := ""
	if fl.Librenms != nil && fl.Librenms.P != nil {
		domain = fl.Librenms.P.DefaultDomain
	}
	return util.FormatName(domain, name)
}

// getFactumDeviceForLibrenmsDevice correlates a librenms device back to its
// factum device. The primary match is the LibreNMS id factum copied from
// the NetBox destination custom field (factumDevice.LibrenmsID), stamped
// on creation. It falls back to a hostname match for devices librenms
// already knows about but that have not been linked that way yet: librenms
// always stores an FQDN in hostname, while factum device names are often
// short, so the factum name is FQDN-ified with the default domain before
// comparing.
// The final fallback matches on primary IPv4: createDevice creates new
// librenms devices with hostname set to the IP, not the FQDN, so if the
// netbox update that stamps destination afterwards fails (logged but not
// retried until the next run's backfill), neither match above would see
// the device on the next run - Sync would then try to create it a second
// time. Matching on IP closes that gap, and also catches librenms devices
// added manually with an IP hostname.
// Those fallbacks only decide create, delete, and the destination backfill.
// Renames and field updates run only for the LibreNMS id stored in destination.
func (fl *FactumLibrenmsClient) getFactumDeviceForLibrenmsDevice(librenmsDevice *LibrenmsDevice) *models.Device {
	for _, factumDevice := range fl.FactumDevices {
		if factumDevice.LibrenmsID != 0 && factumDevice.LibrenmsID == uint(librenmsDevice.DeviceID) {
			return factumDevice
		}
	}
	for _, factumDevice := range fl.FactumDevices {
		fqdn := util.FormatName(fl.Librenms.P.DefaultDomain, factumDevice.Name)
		if strings.EqualFold(librenmsDevice.Hostname, fqdn) {
			return factumDevice
		}
	}
	for _, factumDevice := range fl.FactumDevices {
		if factumDevice.PrimaryIPv4 == "" {
			continue
		}
		addr := strings.Split(factumDevice.PrimaryIPv4, "/")[0]
		if librenmsDevice.Hostname == addr {
			return factumDevice
		}
	}
	return nil
}

// createDevice creates a device in Librenms, trying each configured SNMP
// community (Settings.LibrenmsSNMPCommunities) in turn until one succeeds.
// If none succeed, it force-adds the device with the first configured
// community, skipping Librenms's SNMP-reachability check, so a device is
// never left un-monitored just because automatic community detection
// failed.
func (fl *FactumLibrenmsClient) createDevice(addr, name string, reporter jobevent.Reporter) (int, error) {
	communities := splitLines(fl.Librenms.P.SNMPCommunities)
	if len(communities) == 0 {
		return 0, errors.New("no snmp communities configured")
	}
	version := fl.Librenms.P.SNMPVersion

	var lastErr error
	for _, community := range communities {
		deviceID, err := fl.Librenms.DeviceCreate(addr, name, false, version, community)
		if err == nil {
			return deviceID, nil
		}
		lastErr = err
		reporter.Emit(jobevent.Warning, "cannot create device %s with community %q: %s", name, community, err)
	}

	reporter.Emit(jobevent.Warning, "force-creating device %s with community %q", name, communities[0])
	deviceID, err := fl.Librenms.DeviceCreate(addr, name, true, version, communities[0])
	if err != nil {
		return 0, fmt.Errorf("force-create failed: %w (last non-forced error: %s)", err, lastErr)
	}
	return deviceID, nil
}

// locationCoordinates is the lat/lng sync will write for one LibreNMS
// location. Devices that share the location and disagree keep the
// coordinates of the lowest LibreNMS device id.
type locationCoordinates struct {
	Lat, Lng float64
	DeviceID int
	Hostname string
	Conflict bool
	Warned   bool
}

// librenmsLocationName is the LibreNMS location this device maps to.
// The Factum location custom field wins. Otherwise the device keeps the
// location LibreNMS already has.
func librenmsLocationName(factumDevice *models.Device, librenmsDevice *LibrenmsDevice) string {
	if factumDevice.CfLocation != "" {
		return factumDevice.CfLocation
	}
	return librenmsDevice.Location
}

func coordsClose(lat1, lng1, lat2, lng2 float64) bool {
	return math.Abs(lat1-lat2) < coordEpsilon && math.Abs(lng1-lng2) < coordEpsilon
}

// planLocationCoordinates picks one coordinate pair per location name.
// A device is included when this sync will update it: no destination id
// yet, or the destination id is this LibreNMS device. Quarantined devices
// are left out.
func planLocationCoordinates(devices []*LibrenmsDevice, skip map[int]struct{}, factumFor func(*LibrenmsDevice) *models.Device) map[string]*locationCoordinates {
	plan := map[string]*locationCoordinates{}
	for _, librenmsDevice := range devices {
		if _, skipped := skip[librenmsDevice.DeviceID]; skipped {
			continue
		}
		factumDevice := factumFor(librenmsDevice)
		if factumDevice == nil || factumDevice.Latitude == nil || factumDevice.Longitude == nil {
			continue
		}
		if factumDevice.LibrenmsID != 0 && factumDevice.LibrenmsID != uint(librenmsDevice.DeviceID) {
			continue
		}
		name := librenmsLocationName(factumDevice, librenmsDevice)
		if name == "" {
			continue
		}
		lat, lng := *factumDevice.Latitude, *factumDevice.Longitude
		cur := plan[name]
		if cur == nil {
			plan[name] = &locationCoordinates{Lat: lat, Lng: lng, DeviceID: librenmsDevice.DeviceID, Hostname: librenmsDevice.Hostname}
			continue
		}
		if !coordsClose(lat, lng, cur.Lat, cur.Lng) {
			cur.Conflict = true
			if librenmsDevice.DeviceID < cur.DeviceID {
				cur.Lat, cur.Lng = lat, lng
				cur.DeviceID = librenmsDevice.DeviceID
				cur.Hostname = librenmsDevice.Hostname
			}
		}
	}
	return plan
}

// syncDeviceCoordinates writes this device's Factum coordinates onto its
// LibreNMS location. LibreNMS stores the pin on the location, so devices
// that share a location share one pin. The plan's device performs the
// write. A device with coordinates and no location name is reported and
// skipped.
func (fl *FactumLibrenmsClient) syncDeviceCoordinates(librenmsDevice *LibrenmsDevice, factumDevice *models.Device, plan map[string]*locationCoordinates, reporter jobevent.Reporter) (bool, error) {
	if factumDevice.Latitude == nil || factumDevice.Longitude == nil {
		return false, nil
	}
	name := librenmsLocationName(factumDevice, librenmsDevice)
	if name == "" {
		reporter.Emit(jobevent.Warning, "%s %s: has coordinates but no location; not sent to librenms", librenmsDevice.Display, librenmsDevice.Hostname)
		return false, nil
	}
	choice := plan[name]
	if choice == nil || choice.DeviceID != librenmsDevice.DeviceID {
		return false, nil
	}
	if choice.Conflict && !choice.Warned {
		choice.Warned = true
		reporter.Emit(jobevent.Warning, "location %q has devices with different coordinates; using %s (%.6f, %.6f)", name, choice.Hostname, choice.Lat, choice.Lng)
	}
	wrote, err := fl.Librenms.LocationSetCoordinates(name, choice.Lat, choice.Lng)
	if err != nil {
		return false, err
	}
	if wrote {
		reporter.Emit(jobevent.Info, "%s %s: location %q coordinates %.6f, %.6f", librenmsDevice.Display, librenmsDevice.Hostname, name, choice.Lat, choice.Lng)
	}
	return wrote, nil
}

func (fl *FactumLibrenmsClient) Sync(reporter jobevent.Reporter) error {
	var err error
	var librenms_create []*models.Device
	var librenms_delete []*LibrenmsDevice

	reporter.Emit(jobevent.Info, "Librenms sync started")

	librenmsDevices, err := fl.Librenms.DevicesGet()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if err := requireIPHostnames(librenmsDevices, reporter); err != nil {
		return err
	}

	slog.Debug("Get list of factum-devices")
	fl.Factum = factum.NewFactumClient(fl.Config)
	err = fl.RefreshFactumDevices()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	// Netbox credentials are database-backed (Settings, admin-editable in
	// the web UI) rather than in the YAML config file - see
	// util.GetOrCreateSettings. This process runs on the LibreNMS server,
	// not the primary, and has no access to factum's Postgres DB, so fetch
	// them over REST instead (see web.ApiNetboxConfig).
	fl.Netbox, err = netbox.RemoteClient(fl.Config)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "fetched %d factum device(s), %d librenms device(s)", len(fl.FactumDevices), len(librenmsDevices))

	//
	// Compare Factum-devices with librenms-devices
	//
	// matchedFactumIDs tracks every factum device that already has a
	// corresponding librenms device (via LibrenmsID or the hostname
	// fallback in getFactumDeviceForLibrenmsDevice) - the create pass
	// below must skip these, otherwise every sync run tries to recreate
	// devices librenms already has, which librenms itself then rejects
	// once it notices the primary IP is already in use by that existing
	// device.
	matchedFactumIDs := make(map[uint]bool)
	noMatch, disabled, notMonitored := 0, 0, 0
	for _, librenmsDevice := range librenmsDevices {
		factumDevice := fl.getFactumDeviceForLibrenmsDevice(librenmsDevice)
		switch {
		case factumDevice == nil:
			noMatch++
			if noMatch <= 10 {
				fmt.Printf("DEBUG no factum match: librenms hostname=%q device_id=%d\n", librenmsDevice.Hostname, librenmsDevice.DeviceID)
			}
			librenms_delete = append(librenms_delete, librenmsDevice)
		case !factumDevice.Enabled:
			disabled++
			if disabled <= 10 {
				fmt.Printf("DEBUG factum disabled: librenms hostname=%q -> factum %q\n", librenmsDevice.Hostname, factumDevice.Name)
			}
			librenms_delete = append(librenms_delete, librenmsDevice)
		case !factumDevice.CfMonitorLibrenms:
			notMonitored++
			if notMonitored <= 10 {
				fmt.Printf("DEBUG factum not monitored: librenms hostname=%q -> factum %q\n", librenmsDevice.Hostname, factumDevice.Name)
			}
			librenms_delete = append(librenms_delete, librenmsDevice)
		default:
			matchedFactumIDs[factumDevice.ID] = true
		}
	}
	fmt.Printf("DEBUG delete reasons: no_match=%d disabled=%d not_monitored=%d\n", noMatch, disabled, notMonitored)

	for _, factumDevice := range fl.FactumDevices {
		name := strings.ToLower(factumDevice.Name)
		if !factumDevice.Enabled {
			continue
		}
		if !factumDevice.CfMonitorLibrenms {
			continue
		}
		if matchedFactumIDs[factumDevice.ID] {
			continue
		}
		if factumDevice.PrimaryIPv4 == "" {
			reporter.Emit(jobevent.Warning, "device %s missing primary_ip4, skipping", name)
			continue
		}
		librenms_create = append(librenms_create, factumDevice)
	}

	fmt.Printf("add %d delete %d\n", len(librenms_create), len(librenms_delete))

	//
	// Add/delete devices. Delete candidates are quarantined (polling and
	// alerts off, display name stamped with the scheduled date) and only
	// actually removed after Settings.LibrenmsDelayedDeleteDays, or sooner
	// if the user sets ForceDelete on the pending-delete row. Persistent
	// devices are never quarantined or deleted.
	//
	var candidates []pendingCandidate
	for _, librenmsDevice := range librenms_delete {
		if isPersistentDevice(librenmsDevice, fl.Librenms.P.PersistentDevices) {
			reporter.Emit(jobevent.Info, "skipping persistent device %s", librenmsDevice.Hostname)
			continue
		}
		reason := pendingReasonNoMatch
		if fd := fl.getFactumDeviceForLibrenmsDevice(librenmsDevice); fd != nil {
			if !fd.Enabled {
				reason = pendingReasonDisabled
			} else if !fd.CfMonitorLibrenms {
				reason = pendingReasonNotMonitored
			}
		}
		candidates = append(candidates, pendingCandidate{Device: librenmsDevice, Reason: reason})
	}

	existingPending, err := fl.Factum.GetLibrenmsPendingDeletes()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	presentIDs := make(map[int]bool, len(librenmsDevices))
	leaveAlone := make(map[int]bool)
	for _, d := range librenmsDevices {
		presentIDs[d.DeviceID] = true
		if isPersistentDevice(d, fl.Librenms.P.PersistentDevices) {
			leaveAlone[d.DeviceID] = true
		}
	}
	stillPending, deleted, err := applyDeletePolicy(
		fl.Librenms.P.DelayedDeleteEnabled,
		fl.Librenms.P.DelayedDeleteDays,
		candidates,
		existingPending,
		presentIDs,
		leaveAlone,
		time.Now(),
		fl.Librenms,
		factumPendingStore{client: fl.Factum},
		reporter,
	)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	created := 0
	for _, factumDevice := range librenms_create {
		addr := strings.Split(factumDevice.PrimaryIPv4, "/")[0]
		display := fl.librenmsDisplayName(factumDevice.Name)
		reporter.Emit(jobevent.Info, "creating device %s in Librenms, hostname %s, display %s", factumDevice.Name, addr, display)
		librenmsDeviceID, err := fl.createDevice(addr, display, reporter)
		if err != nil {
			reporter.Emit(jobevent.Error, "cannot create device %s: %s", factumDevice.Name, err)
			continue
		}
		created++

		// Store the librenms device id in the NetBox destination field
		// (librenms:<id>), leaving every other system's id in place, so
		// future syncs can match this device.
		if err := fl.Netbox.SetDestinationID(factumDevice.VM, factumDevice.NetboxID, netboxtool.DestinationLibrenms, strconv.Itoa(librenmsDeviceID)); err != nil {
			reporter.Emit(jobevent.Error, "cannot update netbox destination for %s: %s", factumDevice.Name, err)
		} else {
			factumDevice.LibrenmsID = uint(librenmsDeviceID)
		}
	}
	//
	// Update status on devices and ports in Librenms
	//
	librenmsDevices, err = fl.Librenms.DevicesGet()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	updated := 0
	coordPlan := planLocationCoordinates(librenmsDevices, stillPending, fl.getFactumDeviceForLibrenmsDevice)
	for _, librenmsDevice := range librenmsDevices {
		if _, quarantined := stillPending[librenmsDevice.DeviceID]; quarantined {
			continue
		}
		factumDevice := fl.getFactumDeviceForLibrenmsDevice(librenmsDevice)
		if factumDevice == nil {
			continue
		}

		// Renames and field updates address the LibreNMS device id in
		// the NetBox destination field. A row that already has a
		// different id is left alone. A row with no id is stamped
		// from this match, and that id is then the one updated below.
		// SetDestinationID reads the current value and rewrites only
		// the librenms pair.
		linked := factumDevice.LibrenmsID != 0 && factumDevice.LibrenmsID == uint(librenmsDevice.DeviceID)
		if factumDevice.LibrenmsID == 0 {
			reporter.Emit(jobevent.Info, "%s, %s: stamping netbox destination librenms=%d", librenmsDevice.Hostname, librenmsDevice.Display, librenmsDevice.DeviceID)
			if err := fl.Netbox.SetDestinationID(factumDevice.VM, factumDevice.NetboxID, netboxtool.DestinationLibrenms, strconv.Itoa(librenmsDevice.DeviceID)); err != nil {
				reporter.Emit(jobevent.Error, "%s: cannot update netbox destination: %s", librenmsDevice.Hostname, err)
			} else {
				factumDevice.LibrenmsID = uint(librenmsDevice.DeviceID)
				linked = true
			}
		}
		if !linked {
			continue
		}

		// Hostname is the SNMP polling target. LibreNMS renames it via
		// its own /rename/ endpoint rather than the field-update
		// endpoint used below. The destination id is the device identity,
		// so a changed primary IPv4 is renamed directly.
		if factumDevice.PrimaryIPv4 != "" {
			addr := strings.Split(factumDevice.PrimaryIPv4, "/")[0]
			if addr != "" && librenmsDevice.Hostname != addr {
				reporter.Emit(jobevent.Info, "%s: rename hostname to %s", librenmsDevice.Hostname, addr)
				if _, err := fl.Librenms.DeviceRename(librenmsDevice.DeviceID, addr); err != nil {
					reporter.EmitErr(err)
					return err
				}
			}
		}

		librenmsEnabled := librenmsDevice.Ignore == 0
		factumEnabled := factumDevice.Enabled
		updateData := make(map[string]string)
		if librenmsEnabled != factumEnabled {
			if factumEnabled {
				updateData["ignore"] = "0"
			} else {
				updateData["ignore"] = "1"
			}
			reporter.Emit(jobevent.Info, "%s: setting ignore to %s", librenmsDevice.Hostname, updateData["ignore"])
		}

		display := fl.librenmsDisplayName(factumDevice.Name)
		if librenmsDevice.Display != display {
			reporter.Emit(jobevent.Info, "%s: display %q, set to %q", librenmsDevice.Hostname, librenmsDevice.Display, display)
			updateData["display_template"] = display
		}

		if factumDevice.CfLocation != "" && factumDevice.CfLocation != librenmsDevice.Location {
			reporter.Emit(jobevent.Info, "%s %s: location change from %q to %q", librenmsDevice.Display, librenmsDevice.Hostname, librenmsDevice.Location, factumDevice.CfLocation)
			updateData["location"] = factumDevice.CfLocation
			updateData["override_sysLocation"] = "1"
		}

		changed := false
		if len(updateData) > 0 {
			if _, err := fl.Librenms.DeviceUpdate(librenmsDevice.DeviceID, updateData); err != nil {
				reporter.Emit(jobevent.Warning, err.Error())
				return err
			}
			changed = true
		}

		wrote, err := fl.syncDeviceCoordinates(librenmsDevice, factumDevice, coordPlan, reporter)
		if err != nil {
			reporter.Emit(jobevent.Warning, err.Error())
			return err
		}
		if wrote || changed {
			updated++
		}

		// Parent sync (LibrenmsClient.SetDeviceParent) isn't wired up
		// here: models.Device has no Parents field to sync from, and
		// SetDeviceParent's own DeviceParentList is still a stub - see
		// librenms.go.

		err = fl.syncInterfaces(librenmsDevice, factumDevice, reporter)
		if err != nil {
			reporter.Emit(jobevent.Warning, err.Error())
			return err
		}
	}

	reporter.Emit(jobevent.Info, "Librenms sync: %d new, %d updated, %d deleted", created, updated, deleted)
	return nil
}
