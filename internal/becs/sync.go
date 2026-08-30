package becs

//
// Add, update and delete devices in Netbox based on BECS elements, then
// reuse internal/netbox.Sync to pull the result into factum.
//
// The helpers in internal/netbox (syncDevice, syncInterfaces, ...) write
// to factum's Postgres keyed by netbox_id — they cannot write to Netbox,
// so they are not shared. After this package has reconciled Netbox,
// netbox.Sync is the factum-side half.
//

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

const becsOIDField = "becs_oid"

// customFieldUint reads a Netbox custom field that may be a JSON number
// or a numeric string. Unknown/empty values are 0.
func customFieldUint(fields map[string]any, key string) uint {
	if fields == nil {
		return 0
	}
	switch v := fields[key].(type) {
	case float64:
		if v < 0 {
			return 0
		}
		return uint(v)
	case int:
		if v < 0 {
			return 0
		}
		return uint(v)
	case uint:
		return v
	case string:
		if v == "" {
			return 0
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0
		}
		return uint(n)
	default:
		return 0
	}
}

func deviceBecsOID(d *netboxtool.NBDevice) uint {
	return customFieldUint(d.CustomFields, becsOIDField)
}

func interfaceBecsOID(iface *netboxtool.NBInterface) uint {
	return customFieldUint(iface.CustomFields, becsOIDField)
}

const (
	manufacturerName = "Waystream"
	defaultSiteName  = "Default"
	defaultRoleSlug  = "access-nod"
	defaultPlatform  = "ibos"
)

var ignoreInterfaces = map[string]bool{
	"ethernet0": true,
}

// netboxAPI is the subset of *netboxtool.NetboxClient this sync uses.
type netboxAPI interface {
	GetDevices() ([]*netboxtool.NBDevice, error)
	GetDevice(name string, id int) (*netboxtool.NBDevice, error)
	GetDeviceType(manufacturer, model string) (*netboxtool.NetboxDeviceTypeDetail, error)
	CreateDevice(name string, extra map[string]any) (*netboxtool.NetboxDeviceREST, error)
	UpdateDevice(deviceID uint, changes map[string]any) error
	DeleteDevice(id int) error
	CreateInterfaceWithOptions(deviceID uint, name string, extra map[string]any) (*netboxtool.NetboxInterfaceREST, error)
	InterfaceUpdate(interfaceID int, changes map[string]any) error
	InterfaceDelete(interfaceID int) error
	CreateInterfaceAddress(interfaceID uint, address string, extra map[string]any) (*netboxtool.NBAddress, error)
	AddressDelete(addressID int) error
	GetSiteByName(name string) (*netboxtool.NetboxNamedRef, error)
	GetDeviceRoleBySlug(slug string) (*netboxtool.NetboxNamedRef, error)
	GetPlatformBySlug(slug string) (*netboxtool.NetboxNamedRef, error)
	RequireCustomField(name string, objectTypes ...string) error
}

type Syncer struct {
	becs     *Client
	nb       netboxAPI
	settings *models.Settings
	reporter jobevent.Reporter
	domain   string

	siteID     uint
	roleID     uint
	platformID uint
	types      map[string]*netboxtool.NetboxDeviceTypeDetail

	nbByName map[string]*netboxtool.NBDevice
	nbByOID  map[uint]*netboxtool.NBDevice

	errors  []error
	changed bool
	created int
	updated int
	deleted int
}

func (s *Syncer) note(err error) {
	if err == nil {
		return
	}
	s.reporter.EmitErr(err)
	s.errors = append(s.errors, err)
}

func joinParents(parents []string) string {
	return strings.Join(parents, ",")
}

func ifaceType(dt *netboxtool.NetboxDeviceTypeDetail, ifname string) string {
	if dt != nil {
		for _, tmpl := range dt.Interfaces {
			if tmpl.Name == ifname && tmpl.Type != "" {
				return tmpl.Type
			}
		}
	}
	if strings.Contains(strings.ToLower(ifname), "ethernet") {
		return "1000base-t"
	}
	return "virtual"
}

func firstAddr(addrs []netboxtool.NBAddress) *netboxtool.NBAddress {
	if len(addrs) == 0 {
		return nil
	}
	return &addrs[0]
}

func firstPrefix(p []Prefix) *Prefix {
	if len(p) == 0 {
		return nil
	}
	return &p[0]
}

func ifaceByOID(d *netboxtool.NBDevice) map[uint]*netboxtool.NBInterface {
	out := make(map[uint]*netboxtool.NBInterface, len(d.Interfaces))
	for i := range d.Interfaces {
		iface := &d.Interfaces[i]
		if oid := interfaceBecsOID(iface); oid != 0 {
			out[oid] = iface
		}
	}
	return out
}

func ifaceByName(d *netboxtool.NBDevice) map[string]*netboxtool.NBInterface {
	out := make(map[string]*netboxtool.NBInterface, len(d.Interfaces))
	for i := range d.Interfaces {
		out[d.Interfaces[i].Name] = &d.Interfaces[i]
	}
	return out
}

// Sync fetches ibos elements from BECS, reconciles Netbox devices that
// have a non-zero becs_oid custom field against them, then runs
// netbox.Sync so factum sees the result. If name is set, only that
// device is created/updated/deleted (the full BECS tree is still loaded
// — parent/opaque lookup walks it).
func Sync(c *util.ConfigRoot, name string, reporter jobevent.Reporter) error {
	reporter.Emit(jobevent.Info, "BECS sync started")

	db, err := util.ConnectDatabase(&c.DB)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	becsClient, err := NewClientFromSettings(settings)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if settings.NetboxApiURL == "" || settings.NetboxApiToken == "" {
		err := fmt.Errorf("becs: netbox api url/token is not configured")
		reporter.EmitErr(err)
		return err
	}
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   settings.NetboxApiURL,
		Token: settings.NetboxApiToken,
	})
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	s := &Syncer{
		becs:     becsClient,
		nb:       nb,
		settings: settings,
		reporter: reporter,
		domain:   settings.DefaultDomain,
		types:    map[string]*netboxtool.NetboxDeviceTypeDetail{},
		nbByName: map[string]*netboxtool.NBDevice{},
		nbByOID:  map[uint]*netboxtool.NBDevice{},
	}
	if err := s.run(name); err != nil {
		return err
	}

	reporter.Emit(jobevent.Info, "BECS→Netbox: %d created, %d updated, %d deleted", s.created, s.updated, s.deleted)

	if !s.changed {
		return nil
	}

	// Reuse the existing Netbox→factum reconciler rather than writing
	// factum rows from BECS directly. A single-device BECS delete cannot
	// be expressed as netbox.Sync(name) (that errors when the device is
	// gone), so factum cleanup of a deleted device waits for a full
	// netbox sync.
	factumName := ""
	if name != "" {
		short := strings.ToLower(strings.TrimSpace(name))
		if s.domain != "" {
			short = util.ShortName(short, s.domain)
		}
		if _, ok := s.nbByName[short]; !ok {
			reporter.Emit(jobevent.Warning, "device %q removed from Netbox; run a full netbox sync to delete it from factum", name)
			return s.result()
		}
		factumName = short
	}
	if err := netbox.SyncDB(db, factumName, reporter); err != nil {
		reporter.EmitErr(err)
		return err
	}
	return s.result()
}

func (s *Syncer) result() error {
	if len(s.errors) == 0 {
		return nil
	}
	return fmt.Errorf("becs sync finished with %d error(s)", len(s.errors))
}

func (s *Syncer) run(name string) error {
	rootOID := int(s.settings.BecsEapiOID)
	s.reporter.Emit(jobevent.Info, "Fetching BECS elements")
	if err := s.becs.LoadTree(rootOID); err != nil {
		s.reporter.EmitErr(err)
		return err
	}
	becsDevices, err := s.becs.Devices(s.domain)
	if err != nil {
		s.reporter.EmitErr(err)
		return err
	}

	s.reporter.Emit(jobevent.Info, "Fetching Netbox devices")
	nbDevices, err := s.nb.GetDevices()
	if err != nil {
		s.reporter.EmitErr(err)
		return err
	}
	for _, d := range nbDevices {
		s.nbByName[strings.ToLower(d.Name)] = d
		if oid := deviceBecsOID(d); oid != 0 {
			s.nbByOID[oid] = d
		}
	}

	becsByOID := make(map[int]*Device, len(becsDevices))
	becsByName := make(map[string]*Device, len(becsDevices))
	for _, d := range becsDevices {
		becsByOID[d.OID] = d
		becsByName[d.ShortName] = d
		becsByName[d.Name] = d
	}

	if name != "" {
		want := strings.ToLower(strings.TrimSpace(name))
		if s.domain != "" {
			want = util.ShortName(want, s.domain)
		}
		filtered := map[int]*Device{}
		if d, ok := becsByName[want]; ok {
			filtered[d.OID] = d
		}
		becsByOID = filtered
		becsByName = map[string]*Device{}
		for _, d := range filtered {
			becsByName[d.ShortName] = d
			becsByName[d.Name] = d
		}
		// Restrict Netbox maps to the named device so we don't delete others.
		nbKeep := map[string]*netboxtool.NBDevice{}
		nbOID := map[uint]*netboxtool.NBDevice{}
		if d, ok := s.nbByName[want]; ok {
			nbKeep[strings.ToLower(d.Name)] = d
			if oid := deviceBecsOID(d); oid != 0 {
				nbOID[oid] = d
			}
		}
		if d, ok := becsByName[want]; ok {
			if nb, ok := s.nbByOID[uint(d.OID)]; ok {
				nbKeep[strings.ToLower(nb.Name)] = nb
				nbOID[uint(d.OID)] = nb
			}
		}
		s.nbByName = nbKeep
		s.nbByOID = nbOID
	}

	s.reporter.Emit(jobevent.Info, "Got %d BECS elements, %d Netbox devices with becs_oid",
		len(becsByOID), len(s.nbByOID))

	if err := s.ensureRefs(); err != nil {
		s.reporter.EmitErr(err)
		return err
	}

	s.reporter.Emit(jobevent.Info, "Syncing devices")
	s.syncDevices(becsByOID, becsByName)

	s.reporter.Emit(jobevent.Info, "Syncing device settings")
	for oid, becsDev := range becsByOID {
		nbDev := s.nbByOID[uint(oid)]
		if nbDev == nil {
			continue
		}
		s.syncDeviceSettings(nbDev, becsDev)
	}

	s.reporter.Emit(jobevent.Info, "Syncing interfaces")
	for oid, becsDev := range becsByOID {
		nbDev := s.nbByOID[uint(oid)]
		if nbDev == nil {
			continue
		}
		s.syncInterfaces(nbDev, becsDev)
	}

	s.reporter.Emit(jobevent.Info, "Syncing interface settings")
	for oid, becsDev := range becsByOID {
		nbDev := s.nbByOID[uint(oid)]
		if nbDev == nil {
			continue
		}
		s.syncInterfaceSettings(nbDev, becsDev)
	}

	s.reporter.Emit(jobevent.Info, "Syncing interface addresses (delete)")
	for oid, becsDev := range becsByOID {
		nbDev := s.nbByOID[uint(oid)]
		if nbDev == nil {
			continue
		}
		s.syncAddressesDelete(nbDev, becsDev)
	}

	s.reporter.Emit(jobevent.Info, "Syncing interface addresses (create)")
	for oid, becsDev := range becsByOID {
		nbDev := s.nbByOID[uint(oid)]
		if nbDev == nil {
			continue
		}
		s.syncAddressesCreate(nbDev, becsDev)
	}

	return nil
}

func (s *Syncer) ensureRefs() error {
	if err := s.nb.RequireCustomField(becsOIDField, "dcim.device", "dcim.interface", "ipam.ipaddress"); err != nil {
		return err
	}

	site, err := s.nb.GetSiteByName(defaultSiteName)
	if err != nil {
		return fmt.Errorf("site %s: %w", defaultSiteName, err)
	}
	if site == nil {
		return fmt.Errorf("netbox site %q not found", defaultSiteName)
	}
	s.siteID = site.ID

	role, err := s.nb.GetDeviceRoleBySlug(defaultRoleSlug)
	if err != nil {
		return fmt.Errorf("device role %s: %w", defaultRoleSlug, err)
	}
	if role == nil {
		return fmt.Errorf("netbox device role %q not found", defaultRoleSlug)
	}
	s.roleID = role.ID

	plat, err := s.nb.GetPlatformBySlug(defaultPlatform)
	if err != nil {
		return fmt.Errorf("platform %s: %w", defaultPlatform, err)
	}
	if plat == nil {
		return fmt.Errorf("netbox platform %q not found", defaultPlatform)
	}
	s.platformID = plat.ID
	return nil
}

func (s *Syncer) deviceType(model string) *netboxtool.NetboxDeviceTypeDetail {
	if model == "" {
		return nil
	}
	if dt, ok := s.types[model]; ok {
		return dt
	}
	dt, err := s.nb.GetDeviceType(manufacturerName, model)
	if err != nil {
		s.note(fmt.Errorf("device type %s/%s: %w", manufacturerName, model, err))
		s.types[model] = nil
		return nil
	}
	s.types[model] = dt
	return dt
}

func (s *Syncer) remember(d *netboxtool.NBDevice) {
	if d == nil {
		return
	}
	s.nbByName[strings.ToLower(d.Name)] = d
	if oid := deviceBecsOID(d); oid != 0 {
		s.nbByOID[oid] = d
	}
}

func (s *Syncer) forget(d *netboxtool.NBDevice) {
	if d == nil {
		return
	}
	delete(s.nbByName, strings.ToLower(d.Name))
	if oid := deviceBecsOID(d); oid != 0 {
		delete(s.nbByOID, oid)
	}
}

func (s *Syncer) refresh(name string, id uint) *netboxtool.NBDevice {
	d, err := s.nb.GetDevice(name, int(id))
	if err != nil {
		s.note(fmt.Errorf("refresh device %s: %w", name, err))
		return nil
	}
	if d == nil {
		return nil
	}
	s.remember(d)
	return d
}

func (s *Syncer) syncDevices(becsByOID map[int]*Device, becsByName map[string]*Device) {
	var toDelete []*netboxtool.NBDevice
	type oidFix struct {
		device *netboxtool.NBDevice
		oid    uint
	}
	var toSetOID []oidFix

	seen := map[*netboxtool.NBDevice]bool{}
	for oid, d := range s.nbByOID {
		if seen[d] {
			continue
		}
		seen[d] = true
		if _, ok := becsByOID[int(oid)]; !ok {
			toDelete = append(toDelete, d)
		}
	}

	for _, d := range toDelete {
		s.reporter.Emit(jobevent.Info, "Deleting Netbox device %s", d.Name)
		if err := s.nb.DeleteDevice(int(d.NetboxID)); err != nil {
			s.note(fmt.Errorf("delete device %s: %w", d.Name, err))
			continue
		}
		s.forget(d)
		s.changed = true
		s.deleted++
	}

	for _, b := range becsByOID {
		if _, ok := s.nbByOID[uint(b.OID)]; ok {
			continue
		}
		if d, ok := s.nbByName[b.ShortName]; ok {
			toSetOID = append(toSetOID, oidFix{device: d, oid: uint(b.OID)})
			continue
		}
		if err := s.createDevice(b); err != nil {
			s.note(fmt.Errorf("create device %s: %w", b.ShortName, err))
		}
	}

	for _, fix := range toSetOID {
		s.reporter.Emit(jobevent.Info, "Setting becs_oid on %s to %d", fix.device.Name, fix.oid)
		if err := s.nb.UpdateDevice(fix.device.NetboxID, map[string]any{
			"custom_fields": map[string]any{"becs_oid": fix.oid},
		}); err != nil {
			s.note(fmt.Errorf("set becs_oid on %s: %w", fix.device.Name, err))
			continue
		}
		if d := s.refresh(fix.device.Name, fix.device.NetboxID); d != nil {
			if d.CustomFields == nil {
				d.CustomFields = map[string]any{}
			}
			d.CustomFields[becsOIDField] = fix.oid
			s.remember(d)
		}
		s.changed = true
		s.updated++
	}
}

func (s *Syncer) createDevice(b *Device) error {
	dt := s.deviceType(b.Model)
	if dt == nil {
		return fmt.Errorf("no device type for model %q", b.Model)
	}
	status := "offline"
	if b.Enabled {
		status = "active"
	}
	cf := map[string]any{
		"becs_oid":          b.OID,
		"parents":           joinParents(b.Parents),
		"connection_method": b.ConnectionMethod,
	}
	if b.AlarmDestination != "" {
		cf["alarm_destination"] = b.AlarmDestination
	} else if dt.CF.AlarmDestination != "" {
		cf["alarm_destination"] = dt.CF.AlarmDestination
	}
	if b.AlarmTimeperiod != "" {
		cf["alarm_timeperiod"] = b.AlarmTimeperiod
	} else if dt.CF.AlarmTimeperiod != "" {
		cf["alarm_timeperiod"] = dt.CF.AlarmTimeperiod
	}
	cf["alarm_interfaces"] = dt.CF.AlarmInterfaces
	cf["backup_oxidized"] = dt.CF.BackupOxidized
	cf["monitor_grafana"] = dt.CF.MonitorGrafana
	cf["monitor_icinga"] = dt.CF.MonitorIcinga
	if dt.CF.MonitorLibrenms != nil {
		cf["monitor_librenms"] = *dt.CF.MonitorLibrenms
	} else {
		cf["monitor_librenms"] = true
	}

	s.reporter.Emit(jobevent.Info, "Creating Netbox device %s", b.ShortName)
	created, err := s.nb.CreateDevice(b.ShortName, map[string]any{
		"site":          s.siteID,
		"device_type":   dt.ID,
		"role":          s.roleID,
		"platform":      s.platformID,
		"status":        status,
		"custom_fields": cf,
	})
	if err != nil {
		return err
	}
	d := s.refresh(created.Name, created.ID)
	if d != nil {
		if d.CustomFields == nil {
			d.CustomFields = map[string]any{}
		}
		d.CustomFields[becsOIDField] = uint(b.OID)
		s.remember(d)
	}
	s.changed = true
	s.created++
	return nil
}

func (s *Syncer) saveDevice(d *netboxtool.NBDevice, update map[string]any, cf map[string]any) {
	if len(update) == 0 && len(cf) == 0 {
		return
	}
	if len(cf) > 0 {
		update["custom_fields"] = cf
	}
	if enabled, ok := update["enabled"]; ok {
		delete(update, "enabled")
		if en, _ := enabled.(bool); en {
			update["status"] = "active"
		} else {
			update["status"] = "offline"
		}
	}
	oldName := d.Name
	s.reporter.Emit(jobevent.Info, "Updating device %s", d.Name)
	if err := s.nb.UpdateDevice(d.NetboxID, update); err != nil {
		s.note(fmt.Errorf("update device %s: %w", d.Name, err))
		return
	}
	lookup := oldName
	if name, ok := update["name"].(string); ok && name != "" {
		lookup = name
	}
	if refreshed := s.refresh(lookup, d.NetboxID); refreshed != nil {
		if strings.EqualFold(oldName, refreshed.Name) {
			delete(s.nbByName, strings.ToLower(oldName))
		}
	}
	s.changed = true
	s.updated++
}

func (s *Syncer) syncDeviceSettings(d *netboxtool.NBDevice, b *Device) {
	update := map[string]any{}
	cf := map[string]any{}
	dt := s.deviceType(b.Model)

	if !strings.EqualFold(d.Name, b.ShortName) {
		update["name"] = b.ShortName
	}
	if d.Enabled != b.Enabled {
		update["enabled"] = b.Enabled
	}
	if d.CfParents != joinParents(b.Parents) {
		cf["parents"] = joinParents(b.Parents)
	}
	if d.CfAlarmDestination == "" && b.AlarmDestination != "" {
		cf["alarm_destination"] = b.AlarmDestination
	} else if d.CfAlarmDestination == "" && dt != nil && dt.CF.AlarmDestination != "" {
		cf["alarm_destination"] = dt.CF.AlarmDestination
	}
	if d.CfAlarmTimeperiod == "" && b.AlarmTimeperiod != "" {
		cf["alarm_timeperiod"] = b.AlarmTimeperiod
	} else if d.CfAlarmTimeperiod == "" && dt != nil && dt.CF.AlarmTimeperiod != "" {
		cf["alarm_timeperiod"] = dt.CF.AlarmTimeperiod
	}
	if d.CfConnectionMethod == "" {
		cf["connection_method"] = b.ConnectionMethod
	}
	if dt != nil && d.CfBackupOxidized != dt.CF.BackupOxidized {
		cf["backup_oxidized"] = dt.CF.BackupOxidized
	}
	if d.ModelName != b.Model && dt != nil {
		s.reporter.Emit(jobevent.Info, "Device %s: model %q -> %q", d.Name, d.ModelName, b.Model)
		update["device_type"] = dt.ID
	}
	s.saveDevice(d, update, cf)
}

func (s *Syncer) syncInterfaces(d *netboxtool.NBDevice, b *Device) {
	byOID := ifaceByOID(d)
	byName := ifaceByName(d)

	deleted := false
	for oid, iface := range byOID {
		if _, ok := b.InterfacesOID[int(oid)]; ok {
			continue
		}
		s.reporter.Emit(jobevent.Info, "Netbox %s: delete interface %s", d.Name, iface.Name)
		if err := s.nb.InterfaceDelete(int(iface.NetboxID)); err != nil {
			s.note(fmt.Errorf("delete interface %s/%s: %w", d.Name, iface.Name, err))
			continue
		}
		deleted = true
		s.changed = true
		s.updated++
	}

	if deleted {
		if refreshed := s.refresh(d.Name, d.NetboxID); refreshed != nil {
			d = refreshed
			byOID = ifaceByOID(d)
			byName = ifaceByName(d)
		}
	}

	dt := s.deviceType(b.Model)
	created := false
	for oid, bIface := range b.InterfacesOID {
		if ignoreInterfaces[strings.ToLower(bIface.Name)] {
			continue
		}
		if _, ok := byOID[uint(oid)]; ok {
			continue
		}
		if iface, ok := byName[bIface.Name]; ok {
			s.reporter.Emit(jobevent.Info, "Netbox %s: set becs_oid on interface %s", d.Name, bIface.Name)
			if err := s.nb.InterfaceUpdate(int(iface.NetboxID), map[string]any{
				"custom_fields": map[string]any{"becs_oid": bIface.OID},
			}); err != nil {
				s.note(fmt.Errorf("set interface becs_oid %s/%s: %w", d.Name, bIface.Name, err))
				continue
			}
			created = true
			continue
		}
		s.reporter.Emit(jobevent.Info, "Netbox %s: create interface %s", d.Name, bIface.Name)
		if _, err := s.nb.CreateInterfaceWithOptions(d.NetboxID, bIface.Name, map[string]any{
			"type":    ifaceType(dt, bIface.Name),
			"enabled": bIface.Enabled,
			"custom_fields": map[string]any{
				"becs_oid": bIface.OID,
			},
		}); err != nil {
			s.note(fmt.Errorf("create interface %s/%s: %w", d.Name, bIface.Name, err))
			continue
		}
		created = true
	}
	if created {
		s.refresh(d.Name, d.NetboxID)
		s.changed = true
		s.updated++
	}
}

func (s *Syncer) syncInterfaceSettings(d *netboxtool.NBDevice, b *Device) {
	dt := s.deviceType(b.Model)
	byOID := ifaceByOID(d)

	type pending struct {
		id      uint
		oldName string
		changes map[string]any
	}
	updates := map[string]pending{}
	for oid, iface := range byOID {
		bIface, ok := b.InterfacesOID[int(oid)]
		if !ok {
			continue
		}
		changes := map[string]any{}
		if iface.Name != bIface.Name {
			changes["name"] = bIface.Name
		}
		wantType := ifaceType(dt, bIface.Name)
		if wantType != "" && iface.Type != wantType {
			changes["type"] = wantType
		}
		if iface.Enabled != bIface.Enabled {
			changes["enabled"] = bIface.Enabled
		}
		if len(changes) == 0 {
			continue
		}
		updates[iface.Name] = pending{id: iface.NetboxID, oldName: iface.Name, changes: changes}
	}
	if len(updates) == 0 {
		return
	}

	// Apply renames in an order that avoids colliding with a name that
	// is itself about to be renamed away.
	for len(updates) > 0 {
		progress := false
		for oldName, u := range updates {
			if newName, ok := u.changes["name"].(string); ok && newName != oldName {
				if _, taken := updates[newName]; taken {
					continue
				}
			}
			s.reporter.Emit(jobevent.Info, "Updating interface %s/%s", d.Name, oldName)
			if err := s.nb.InterfaceUpdate(int(u.id), u.changes); err != nil {
				s.note(fmt.Errorf("update interface %s/%s: %w", d.Name, oldName, err))
			}
			delete(updates, oldName)
			progress = true
			break
		}
		if !progress {
			// Cycle of renames — apply the rest anyway and let Netbox error.
			for oldName, u := range updates {
				s.reporter.Emit(jobevent.Info, "Updating interface %s/%s", d.Name, oldName)
				if err := s.nb.InterfaceUpdate(int(u.id), u.changes); err != nil {
					s.note(fmt.Errorf("update interface %s/%s: %w", d.Name, oldName, err))
				}
				delete(updates, oldName)
			}
		}
	}
	s.refresh(d.Name, d.NetboxID)
	s.changed = true
	s.updated++
}

func (s *Syncer) syncAddressesDelete(d *netboxtool.NBDevice, b *Device) {
	byOID := ifaceByOID(d)
	clearedPrimary := false
	removed := false
	for oid, iface := range byOID {
		bIface, ok := b.InterfacesOID[int(oid)]
		if !ok {
			continue
		}
		have := firstAddr(iface.Addresses)
		want := firstPrefix(bIface.Prefix4)
		if have == nil {
			continue
		}
		del := want == nil
		if !del && have.Address != want.Address {
			del = true
		}
		if !del {
			continue
		}
		s.reporter.Emit(jobevent.Info, "Netbox %s/%s: delete address %s", d.Name, iface.Name, have.Address)
		if err := s.nb.AddressDelete(int(have.NetboxID)); err != nil {
			s.note(fmt.Errorf("delete address %s/%s %s: %w", d.Name, iface.Name, have.Address, err))
			continue
		}
		removed = true
		s.changed = true
		if strings.EqualFold(iface.Name, "loopback0") && d.PrimaryIPv4ID == have.NetboxID {
			clearedPrimary = true
		}
	}
	if clearedPrimary {
		s.saveDevice(d, map[string]any{"primary_ip4": nil}, nil)
	} else if removed {
		s.refresh(d.Name, d.NetboxID)
	}
}

func (s *Syncer) syncAddressesCreate(d *netboxtool.NBDevice, b *Device) {
	d = s.refresh(d.Name, d.NetboxID)
	if d == nil {
		return
	}
	byOID := ifaceByOID(d)
	deviceUpdate := map[string]any{}
	for oid, iface := range byOID {
		bIface, ok := b.InterfacesOID[int(oid)]
		if !ok {
			continue
		}
		have := firstAddr(iface.Addresses)
		want := firstPrefix(bIface.Prefix4)
		if want == nil {
			continue
		}
		if have == nil {
			s.reporter.Emit(jobevent.Info, "Netbox %s/%s: create address %s", d.Name, iface.Name, want.Address)
			created, err := s.nb.CreateInterfaceAddress(iface.NetboxID, want.Address, map[string]any{
				"custom_fields": map[string]any{"becs_oid": want.OID},
			})
			if err != nil {
				s.note(fmt.Errorf("create address %s/%s %s: %w", d.Name, iface.Name, want.Address, err))
				continue
			}
			have = created
			s.changed = true
		}
		if strings.EqualFold(iface.Name, "loopback0") && have != nil {
			if d.PrimaryIPv4ID == 0 || d.PrimaryIPv4ID != have.NetboxID {
				s.reporter.Emit(jobevent.Info, "Netbox %s: set primary_ip4 to %s", d.Name, have.Address)
				deviceUpdate["primary_ip4"] = have.NetboxID
			}
		}
	}
	if len(deviceUpdate) > 0 {
		s.saveDevice(d, deviceUpdate, nil)
	} else if s.changed {
		s.refresh(d.Name, d.NetboxID)
	}
}
