package netbox

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Fetch all devices (with their interfaces, addresses and tags) from
// Netbox and update the factum database.
//
// If name is specified, only that device is synced. Otherwise every
// device is synced and any device previously imported from Netbox
// (cf_source == "netbox") that no longer exists there is removed,
// together with its interfaces, addresses and tags.
//
// Interfaces, addresses and tags belonging to a synced device are always
// reconciled against Netbox: anything no longer present there is removed
// from factum, whether the sync is for a single device or all of them.
func Sync(c *util.ConfigRoot, name string, reporter jobevent.Reporter) error {
	db, err := util.ConnectDatabase(&c.DB)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	return SyncDB(db, name, reporter)
}

// SyncDB is Sync against an already-connected database, for callers that
// already hold a shared *gorm.DB (currently just the Netbox webhook
// handler, web.ApiNetboxWebhook) - opening a brand new, unbounded connection
// pool on every single webhook call is what exhausted Postgres's
// max_connections under a burst of Netbox webhooks.
func SyncDB(db *gorm.DB, name string, reporter jobevent.Reporter) error {
	var err error
	reporter.Emit(jobevent.Info, "Netbox sync started")

	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	nb, err := netboxtool.NewNetboxClient(
		netboxtool.ConfigNetbox{
			URL:   settings.NetboxApiURL,
			Token: settings.NetboxApiToken,
		})
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	fullSync := name == ""
	var nb_devices []*netboxtool.NBDevice
	if fullSync {
		slog.Debug("Fetch devices from netbox")
		nb_devices, err = nb.GetDevices()
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
		slog.Debug("Fetch virtual machines from netbox")
		nb_vms, err := nb.GetVMs()
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
		nb_devices = append(nb_devices, nb_vms...)
	} else {
		// Create/update (and interface/IP delete) path: re-fetch the named
		// object. Device-level deletes are handled by the webhook itself
		// via DeleteDeviceByNetboxID — GetDevice would return nil here
		// because Netbox has already removed the row.
		//
		// Device and VM names are globally unique in Netbox, so try a
		// physical device first and only fall back to a VM lookup if that
		// finds nothing - avoids a second API call for the common case.
		device, err := nb.GetDevice(name, 0)
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
		if device == nil {
			device, err = nb.GetVM(name, 0)
			if err != nil {
				reporter.EmitErr(err)
				return err
			}
		}
		if device == nil {
			err := fmt.Errorf("netbox: no device or VM named %q", name)
			reporter.EmitErr(err)
			return err
		}
		nb_devices = append(nb_devices, device)
	}
	slog.Debug("Netbox devices", "loaded", len(nb_devices))

	var count_new int
	var count_updated int
	var count_deleted int

	// Netbox's dcim.Device and virtualization.VirtualMachine tables have
	// independent ID sequences, so a physical device and a VM can share the
	// same NetboxID - synced IDs are tracked separately per the VM flag so
	// deleteMissingDevices below can't confuse one for the other.
	syncedDeviceIDs := make([]uint, 0, len(nb_devices))
	syncedVMIDs := make([]uint, 0, len(nb_devices))
	for _, nb_device := range nb_devices {
		if nb_device.VM {
			syncedVMIDs = append(syncedVMIDs, nb_device.NetboxID)
		} else {
			syncedDeviceIDs = append(syncedDeviceIDs, nb_device.NetboxID)
		}

		isNew, err := syncDevice(db, nb_device)
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
		if isNew {
			count_new++
		} else {
			count_updated++
		}
	}

	// Only a full sync has the complete list of devices that exist in
	// Netbox, so device-level cleanup only runs then. Guard against an
	// empty result wiping every netbox-sourced device in factum.
	if fullSync && len(nb_devices) > 0 {
		count_deleted, err = deleteMissingDevices(db, syncedDeviceIDs, syncedVMIDs)
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
	}

	reporter.Emit(jobevent.Info, "Netbox sync: %d new, %d updated, %d deleted", count_new, count_updated, count_deleted)

	// Cable reconciliation needs Netbox's complete cable inventory to tell
	// "removed" apart from "just not in this device's slice", so - like
	// customer-to-tenant sync below - it only runs on a full sync.
	if fullSync {
		if err := syncCables(db, nb, reporter); err != nil {
			reporter.EmitErr(err)
			return err
		}
		if err := syncSites(db, nb, reporter); err != nil {
			reporter.EmitErr(err)
			return err
		}
	}

	// Customer-to-tenant sync is one-directional (factum -> netbox) and
	// unrelated to the device name filter above, so it only runs on a
	// full sync - a single-device sync (e.g. the netbox webhook) has no
	// reason to also push every customer.
	if fullSync && settings.NetboxSyncCustomersEnabled != nil && *settings.NetboxSyncCustomersEnabled {
		if err := syncCustomersToNetbox(db, nb, reporter); err != nil {
			reporter.EmitErr(err)
			return err
		}
	}

	// Reverse-import Netbox L2VPNs (created by device-sync or the
	// service GUI; type from cfgmgmt NetboxType, e.g. evpl/vpls) onto
	// matching factum Service rows so the device interface table can
	// show Service buttons. Runs on single-device syncs too: interfaces
	// for the just-synced device are fresh, and peer ends resolve from
	// whatever is already in factum.
	if err := syncServiceEndpointsFromL2VPNs(db, nb, reporter); err != nil {
		reporter.EmitErr(err)
		return err
	}

	return nil
}

// syncCables mirrors Netbox's full cable inventory into the local
// Connection table, keyed by Netbox cable id - not just the LLDP-derived
// cables internal/device-sync creates, so manually-created Netbox cables
// show up too (e.g. for a device connectivity map). Each cable's two
// Netbox interface ids are resolved to factum's own (device_id,
// interface_id) via the interfaces table, which by this point in Sync has
// already been fully reconciled for every synced device.
func syncCables(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	nb_cables, err := nb.GetCables()
	if err != nil {
		return err
	}

	var interfaces []models.Interface
	if err := db.Select("id", "device_id", "netbox_id").Find(&interfaces).Error; err != nil {
		return err
	}
	byNetboxID := make(map[uint]models.Interface, len(interfaces))
	for _, intf := range interfaces {
		byNetboxID[intf.NetboxID] = intf
	}

	var existing []models.Connection
	if err := db.Select("id", "netbox_id").Find(&existing).Error; err != nil {
		return err
	}
	existingIDs := make(map[uint]bool, len(existing))
	for _, c := range existing {
		existingIDs[c.NetboxID] = true
	}

	syncedIDs := make([]uint, 0, len(nb_cables))
	var count_new, count_updated, count_skipped int
	for _, nb_cable := range nb_cables {
		aIntf, aok := byNetboxID[nb_cable.AInterface]
		bIntf, bok := byNetboxID[nb_cable.BInterface]
		if !aok || !bok {
			// One (or both) endpoints belong to a device/interface factum
			// doesn't have synced (filtered out, disabled sync, ...) -
			// skip rather than persist a connection with a dangling side.
			count_skipped++
			continue
		}
		syncedIDs = append(syncedIDs, nb_cable.NetboxID)

		conn := models.Connection{
			NetboxID:     nb_cable.NetboxID,
			DeviceAID:    aIntf.DeviceID,
			InterfaceAID: aIntf.ID,
			DeviceBID:    bIntf.DeviceID,
			InterfaceBID: bIntf.ID,
			Label:        nb_cable.Label,
		}
		err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "netbox_id"}},
			UpdateAll: true,
		}).Create(&conn).Error
		if err != nil {
			return err
		}
		if existingIDs[nb_cable.NetboxID] {
			count_updated++
		} else {
			count_new++
		}
	}

	// Guard against an empty/failed cable fetch wiping every local
	// connection, matching deleteMissingDevices' guard for devices - if
	// Netbox genuinely returned zero interface-to-interface cables, this
	// leaves stale rows in place rather than assuming that.
	var count_deleted int
	if len(nb_cables) > 0 {
		result := db.Where("netbox_id NOT IN ?", syncedIDs).Delete(&models.Connection{})
		if result.Error != nil {
			return result.Error
		}
		count_deleted = int(result.RowsAffected)
	}

	reporter.Emit(jobevent.Info, "Netbox cable sync: %d new, %d updated, %d deleted, %d skipped (unresolved endpoint)",
		count_new, count_updated, count_deleted, count_skipped)

	if err := db.Exec(`
		UPDATE service_paths SET status = ?
		WHERE service_id IN (
			SELECT DISTINCT service_id FROM service_hops h
			LEFT JOIN connections c ON c.id = h.connection_id
			WHERE h.connection_id IS NOT NULL AND c.id IS NULL
		)`, models.PathStale).Error; err != nil {
		return err
	}
	if err := optical.RebuildStale(db); err != nil {
		reporter.Emit(jobevent.Warning, "optical retrace after cable sync: %v", err)
	}
	return nil
}

// syncSites mirrors Netbox's full site inventory into the local Site
// table, keyed by Netbox site id - unlike Device.Site/SiteID, which only
// ever reference sites that have at least one device, this is what lets
// the network map plot a site's location even when it has none.
// nb.GetSites already filters out sites with no coordinates and the
// placeholder "Default" site.
func syncSites(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	nb_sites, err := nb.GetSites()
	if err != nil {
		return err
	}

	var existing []models.Site
	if err := db.Select("id", "netbox_id").Find(&existing).Error; err != nil {
		return err
	}
	existingIDs := make(map[uint]bool, len(existing))
	for _, s := range existing {
		existingIDs[s.NetboxID] = true
	}

	syncedIDs := make([]uint, 0, len(nb_sites))
	var count_new, count_updated int
	for _, nb_site := range nb_sites {
		syncedIDs = append(syncedIDs, nb_site.ID)

		site := models.Site{
			NetboxID:  nb_site.ID,
			Name:      nb_site.Name,
			Latitude:  float64(*nb_site.Latitude),
			Longitude: float64(*nb_site.Longitude),
		}
		err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "netbox_id"}},
			UpdateAll: true,
		}).Create(&site).Error
		if err != nil {
			return err
		}
		if existingIDs[nb_site.ID] {
			count_updated++
		} else {
			count_new++
		}
	}

	// Guard against an empty/failed site fetch wiping every local site,
	// matching syncCables'/deleteMissingDevices' guard for the same reason.
	var count_deleted int
	if len(nb_sites) > 0 {
		result := db.Where("netbox_id NOT IN ?", syncedIDs).Delete(&models.Site{})
		if result.Error != nil {
			return result.Error
		}
		count_deleted = int(result.RowsAffected)
	}

	reporter.Emit(jobevent.Info, "Netbox site sync: %d new, %d updated, %d deleted", count_new, count_updated, count_deleted)
	return nil
}

// slugInvalidChars matches runs of characters not valid in a Netbox slug.
var slugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns a customer name into a Netbox-safe tenant slug.
func slugify(s string) string {
	s = slugInvalidChars.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if len(s) > 100 {
		s = strings.Trim(s[:100], "-")
	}
	if s == "" {
		s = "tenant"
	}
	return s
}

// uniqueTenantSlug returns base if it is free, otherwise base-<disambiguator>
// (and -2, -3, ... if that is taken too). Netbox slugs are unique and cap
// at 100 characters; two customers whose names slugify the same way (e.g.
// "Acme AB" and "Acme-AB") would otherwise collide on create.
func uniqueTenantSlug(base, disambiguator string, taken map[string]*netboxtool.NBTenant) string {
	if _, ok := taken[base]; !ok {
		return base
	}
	for i := 0; ; i++ {
		var suffix string
		if i == 0 {
			suffix = "-" + disambiguator
		} else {
			suffix = fmt.Sprintf("-%s-%d", disambiguator, i+1)
		}
		stem := base
		if len(stem)+len(suffix) > 100 {
			keep := 100 - len(suffix)
			if keep < 1 {
				keep = 1
			}
			stem = strings.Trim(stem[:keep], "-")
		}
		candidate := stem + suffix
		if len(candidate) > 100 {
			candidate = strings.Trim(candidate[:100], "-")
		}
		if _, ok := taken[candidate]; !ok {
			return candidate
		}
	}
}

// tenantAPI is the Netbox surface customer→tenant sync needs, narrowed so
// tests can substitute a fake without a live Netbox. *netboxtool.NetboxClient
// implements it.
type tenantAPI interface {
	GetTenants() ([]*netboxtool.NBTenant, error)
	GetTenant(source, sourceID string) (*netboxtool.NBTenant, error)
	CreateTenant(name, slug string, changes map[string]any) (*netboxtool.NetboxTenantREST, error)
	UpdateTenant(tenantID uint, changes map[string]any) error
}

// tenantNameTakenError is a same-name Netbox tenant that already belongs to
// a different live factum customer - creating another with that name would
// violate tenancy_tenant_unique_name, and adopting it would steal the
// tenant out from under the owner.
type tenantNameTakenError struct {
	Name    string
	OwnerID string
}

func (e *tenantNameTakenError) Error() string {
	return fmt.Sprintf("netbox tenant %q already claimed by factum customer id %s", e.Name, e.OwnerID)
}

func isTenantConflict(err error) bool {
	if err == nil {
		return false
	}
	var taken *tenantNameTakenError
	if errors.As(err, &taken) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "tenancy_tenant_unique_name") ||
		strings.Contains(s, "tenancy_tenant_unique_slug") ||
		strings.Contains(s, "already exists")
}

type tenantSyncAction int

const (
	tenantUnchanged tenantSyncAction = iota
	tenantCreated
	tenantUpdated
)

// tenantIndex is the in-memory view of Netbox tenants for one sync run,
// keyed so a customer can be matched by custom-field source_id first and
// fall back to name (Netbox's unique constraint) without a second API
// round-trip. liveIDs is the set of factum customer IDs in this run;
// a tenant tagged for a source_id that is not in it is treated as orphaned
// and can be adopted by name. nil liveIDs (FindOrCreateTenant) never
// treats another factum source_id as orphaned.
type tenantIndex struct {
	bySourceID map[string]*netboxtool.NBTenant
	byName     map[string]*netboxtool.NBTenant
	bySlug     map[string]*netboxtool.NBTenant
	liveIDs    map[string]struct{}
}

func newTenantIndex(tenants []*netboxtool.NBTenant, liveIDs map[string]struct{}) *tenantIndex {
	idx := &tenantIndex{
		bySourceID: make(map[string]*netboxtool.NBTenant, len(tenants)),
		byName:     make(map[string]*netboxtool.NBTenant, len(tenants)),
		bySlug:     make(map[string]*netboxtool.NBTenant, len(tenants)),
		liveIDs:    liveIDs,
	}
	for _, t := range tenants {
		idx.add(t)
	}
	return idx
}

func (idx *tenantIndex) add(t *netboxtool.NBTenant) {
	if t.CfSource == "factum" && t.CfSourceID != "" {
		idx.bySourceID[t.CfSourceID] = t
	}
	idx.byName[strings.ToLower(t.Name)] = t
	if t.Slug != "" {
		idx.bySlug[t.Slug] = t
	}
}

func (idx *tenantIndex) claim(t *netboxtool.NBTenant, sourceID string) {
	if t.CfSourceID != "" && t.CfSourceID != sourceID {
		delete(idx.bySourceID, t.CfSourceID)
	}
	t.CfSource = "factum"
	t.CfSourceID = sourceID
	idx.bySourceID[sourceID] = t
}

func (idx *tenantIndex) rename(t *netboxtool.NBTenant, name string) {
	oldKey := strings.ToLower(t.Name)
	newKey := strings.ToLower(name)
	if oldKey != newKey {
		delete(idx.byName, oldKey)
		idx.byName[newKey] = t
	}
	t.Name = name
}

// tenantClaimable reports whether an existing Netbox tenant can be tagged
// as this factum customer. Unclaimed tenants (no factum source_id) and
// tenants already tagged for this customer are claimable. A tenant tagged
// for another live factum customer is not. When liveIDs is set, a tenant
// tagged for a source_id that is no longer a factum customer is treated
// as orphaned and claimable (customers re-imported with new IDs).
func tenantClaimable(t *netboxtool.NBTenant, sourceID string, liveIDs map[string]struct{}) bool {
	if t.CfSource != "factum" || t.CfSourceID == "" || t.CfSourceID == sourceID {
		return true
	}
	if liveIDs == nil {
		return false
	}
	_, live := liveIDs[t.CfSourceID]
	return !live
}

func tenantCustomFields(sourceID string) map[string]any {
	return map[string]any{
		"source":    "factum",
		"source_id": sourceID,
	}
}

// ensureTenant creates or updates the Netbox tenant for customer against
// idx, which is mutated so later customers in the same run see the claim.
// Matching order: custom-field source_id, then case-insensitive name
// (adopt if claimable), then create with a unique slug.
func ensureTenant(nb tenantAPI, customer models.Customer, idx *tenantIndex) (*netboxtool.NBTenant, tenantSyncAction, error) {
	sourceID := strconv.FormatUint(uint64(customer.ID), 10)
	customFields := tenantCustomFields(sourceID)

	if tenant, ok := idx.bySourceID[sourceID]; ok {
		if tenant.Name == customer.Name {
			return tenant, tenantUnchanged, nil
		}
		if err := nb.UpdateTenant(tenant.NetboxID, map[string]any{
			"name":          customer.Name,
			"custom_fields": customFields,
		}); err != nil {
			return nil, tenantUnchanged, err
		}
		idx.rename(tenant, customer.Name)
		return tenant, tenantUpdated, nil
	}

	if tenant, ok := idx.byName[strings.ToLower(customer.Name)]; ok {
		if !tenantClaimable(tenant, sourceID, idx.liveIDs) {
			return nil, tenantUnchanged, &tenantNameTakenError{Name: customer.Name, OwnerID: tenant.CfSourceID}
		}
		if err := nb.UpdateTenant(tenant.NetboxID, map[string]any{
			"name":          customer.Name,
			"custom_fields": customFields,
		}); err != nil {
			return nil, tenantUnchanged, err
		}
		idx.claim(tenant, sourceID)
		return tenant, tenantUpdated, nil
	}

	slug := uniqueTenantSlug(slugify(customer.Name), sourceID, idx.bySlug)
	created, err := nb.CreateTenant(customer.Name, slug, map[string]any{
		"custom_fields": customFields,
	})
	if err != nil {
		return nil, tenantUnchanged, err
	}
	t := &netboxtool.NBTenant{
		NetboxID:   created.ID,
		Name:       created.Name,
		Slug:       created.Slug,
		CfSource:   "factum",
		CfSourceID: sourceID,
	}
	idx.add(t)
	return t, tenantCreated, nil
}

// syncCustomersToNetbox creates/updates a Netbox tenant for every factum
// customer, matched to its tenant via the custom fields "source"="factum"
// and "source_id"=<customer.ID> (customer.ID is factum's own autogenerated
// primary key - a Lime-sourced customer's Lime company ID lives in
// customer.SourceID instead, see internal/lime.SyncCustomers). Keying on
// the factum ID rather than a source-specific one means every customer -
// regardless of which upstream source it came from, or whether it only
// exists in factum - syncs to Netbox the same way.
//
// Tenants that already exist in Netbox under the same name but without
// those custom fields (created by hand, or before this sync was enabled)
// are adopted rather than POSTed - Netbox unique-constraints name and
// slug, so creating a second tenant with the same name 400s and used to
// abort the rest of the job. A name already claimed by a different live
// factum customer is skipped with a warning so one collision cannot
// block L2VPN import / the rest of the tenant list. Tenants are never
// deleted here: a customer removed from factum leaves its tenant untouched
// in Netbox.
func syncCustomersToNetbox(db *gorm.DB, nb tenantAPI, reporter jobevent.Reporter) error {
	var customers []models.Customer
	if err := db.Find(&customers).Error; err != nil {
		return err
	}

	nb_tenants, err := nb.GetTenants()
	if err != nil {
		return err
	}
	liveIDs := make(map[string]struct{}, len(customers))
	for _, c := range customers {
		liveIDs[strconv.FormatUint(uint64(c.ID), 10)] = struct{}{}
	}
	idx := newTenantIndex(nb_tenants, liveIDs)

	var count_new, count_updated, count_skipped int
	for _, customer := range customers {
		_, action, err := ensureTenant(nb, customer, idx)
		if err != nil {
			if isTenantConflict(err) {
				reporter.Emit(jobevent.Warning, "Netbox tenant sync: skipping customer %q (id=%d): %v", customer.Name, customer.ID, err)
				count_skipped++
				continue
			}
			return err
		}
		switch action {
		case tenantCreated:
			count_new++
		case tenantUpdated:
			count_updated++
		}
	}

	reporter.Emit(jobevent.Info, "Netbox tenant sync: %d new, %d updated, %d skipped", count_new, count_updated, count_skipped)
	return nil
}

// FindOrCreateTenant returns the Netbox tenant for customer, matched via the
// same "source"="factum"/"source_id"=<customer.ID> custom fields
// syncCustomersToNetbox uses. Provisioning paths (currently
// web.ApiServiceElineUpdate, when it assigns a service's L2VPN to its
// customer's tenant) call this directly instead of waiting for the next
// scheduled Netbox sync, so a customer's first service still gets a tenant
// even if Netbox sync hasn't run since the customer was created.
//
// This runs synchronously in a user-facing request, so it looks the tenant
// up via nb.GetTenant's filtered REST call rather than nb.GetTenants' full
// tenant-table fetch - the latter used to mean every service update paid
// for pulling every Netbox tenant just to find one. GetTenants is only
// consulted on a miss, to adopt an existing same-name tenant (or pick a
// unique slug) instead of POSTing a name that already exists.
func FindOrCreateTenant(nb *netboxtool.NetboxClient, customer models.Customer) (*netboxtool.NBTenant, error) {
	return findOrCreateTenant(nb, customer)
}

func findOrCreateTenant(nb tenantAPI, customer models.Customer) (*netboxtool.NBTenant, error) {
	sourceID := strconv.FormatUint(uint64(customer.ID), 10)

	if tenant, err := nb.GetTenant("factum", sourceID); err != nil {
		return nil, err
	} else if tenant != nil {
		return tenant, nil
	}

	tenants, err := nb.GetTenants()
	if err != nil {
		return nil, err
	}
	tenant, _, err := ensureTenant(nb, customer, newTenantIndex(tenants, nil))
	return tenant, err
}

// syncDevice creates or updates a single device and reconciles its
// interfaces, addresses and tags against Netbox. Returns true if the
// device was newly created.
func syncDevice(db *gorm.DB, nb_device *netboxtool.NBDevice) (bool, error) {
	// Scoped by vm as well as netbox_id: Netbox's dcim.Device and
	// virtualization.VirtualMachine tables have independent ID sequences, so
	// a physical device and a VM can share the same NetboxID.
	//
	// device.ID is deliberately left unset (rather than looked up first and
	// assigned here) so the upsert below is a genuine INSERT ... ON CONFLICT
	// keyed on the idx_devices_netbox_id_vm unique index instead of a
	// check-then-act find-then-save - two overlapping syncs of the same
	// device (e.g. a webhook firing while a full sync is in progress) used
	// to be able to both see "no existing row" and both insert, producing
	// duplicate devices for the same Netbox object.
	//
	// isNew is only used for the new/updated counts in the sync summary
	// log, so this pre-check being racy against a concurrent syncDevice
	// call (unlike the upsert itself, which is atomic) only risks a
	// slightly wrong count, never a duplicate row.
	var existing models.Device
	lookupErr := db.Select("id").Where("netbox_id = ? AND vm = ?", nb_device.NetboxID, nb_device.VM).Take(&existing).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return false, lookupErr
	}
	isNew := errors.Is(lookupErr, gorm.ErrRecordNotFound)

	var device models.Device
	device.VM = nb_device.VM
	device.NetboxID = nb_device.NetboxID
	device.Name = nb_device.Name
	device.Comments = nb_device.Comments
	device.Enabled = nb_device.Enabled
	device.Manufacturer = nb_device.Manufacturer
	device.ManufacturerID = nb_device.ManufacturerID
	device.ModelName = nb_device.ModelName
	device.ModelID = nb_device.ModelID
	device.Platform = nb_device.Platform
	device.PlatformID = nb_device.PlatformID
	device.PrimaryIPv4 = nb_device.PrimaryIPv4
	device.PrimaryIPv4ID = nb_device.PrimaryIPv4ID
	device.PrimaryIPv6 = nb_device.PrimaryIPv6
	device.PrimaryIPv6ID = nb_device.PrimaryIPv6ID
	device.Role = nb_device.Role
	device.RoleID = nb_device.RoleID
	device.Site = nb_device.Site
	device.SiteID = nb_device.SiteID
	device.Status = nb_device.Status
	device.Latitude = nb_device.Latitude
	device.Longitude = nb_device.Longitude
	device.CfAlarmTimeperiod = nb_device.CfAlarmTimeperiod
	device.CfAlarmDestination = nb_device.CfAlarmDestination
	device.CfAlarmInterfaces = nb_device.CfAlarmInterfaces
	device.CfBackupOxidized = nb_device.CfBackupOxidized
	device.CfConnectionMethod = nb_device.CfConnectionMethod
	device.CfLocation = nb_device.CfLocation
	device.CfMonitorGrafana = nb_device.CfMonitorGrafana
	device.CfMonitorIcinga = nb_device.CfMonitorIcinga
	device.CfMonitorLibrenms = nb_device.CfMonitorLibrenms
	device.CfSource = nb_device.CfSource
	device.CfSourceID = nb_device.CfSourceID
	device.LibrenmsID = nb_device.LibrenmsID

	kindMaps, mapErr := optical.LoadKindMaps(db)
	if mapErr != nil {
		return false, mapErr
	}
	device.OpticalKindCF = optical.NormalizeOpticalKindCF(optical.CustomFieldValue(nb_device.CustomFields, "optical_role", "optical_kind"))
	device.OpticalKind = optical.ResolveOpticalKind(device.OpticalKindCF, device.Role, kindMaps)

	// Interfaces and tags are reconciled explicitly below; clear them
	// here so the upsert does not try to auto-persist the netbox-shaped
	// copies (which carry no factum IDs) as associations.
	device.Interfaces = nil
	device.Tags = nil

	// UpdateAll rewrites every column except the primary key and
	// CreatedAt on a conflicting row (see gorm's onConflict handling in
	// callbacks/create.go), and still bumps UpdatedAt - equivalent to the
	// old find-then-Save's "update every field" behaviour, but as a single
	// atomic statement keyed on idx_devices_netbox_id_vm rather than a
	// separate check.
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "netbox_id"}, {Name: "vm"}},
		UpdateAll: true,
	}).Create(&device).Error
	if err != nil {
		return false, err
	}

	if err := syncTags(db, device.ID, 0, nb_device.Tags); err != nil {
		return false, err
	}
	if err := syncInterfaces(db, device.ID, nb_device.Interfaces); err != nil {
		return false, err
	}

	return isNew, nil
}

// syncInterfaces creates/updates the interfaces of a device, matched by
// netbox_id, and removes any factum interface no longer present in
// nb_interfaces (along with its addresses and tags).
func syncInterfaces(db *gorm.DB, deviceID uint, nb_interfaces []netboxtool.NBInterface) error {
	var existing []models.Interface
	if err := db.Where("device_id = ?", deviceID).Find(&existing).Error; err != nil {
		return err
	}
	seen := make(map[uint]bool, len(nb_interfaces))
	for _, nb_intf := range nb_interfaces {
		seen[nb_intf.NetboxID] = true

		// Built fresh (no factum ID) and upserted on (device_id, netbox_id)
		// rather than read-then-Save, same reasoning and pattern as
		// syncDevice above - see MigrateDatabase's dedupeInterfaces
		// (internal/util/db.go) for what the old read-then-Save race let
		// happen.
		var iface models.Interface
		iface.DeviceID = deviceID
		iface.NetboxID = nb_intf.NetboxID
		iface.Name = nb_intf.Name
		iface.Description = nb_intf.Description
		iface.Enabled = nb_intf.Enabled
		iface.CfRole = nb_intf.CfRole
		iface.VRF = nb_intf.VRF
		iface.Type = nb_intf.Type
		iface.UntaggedVLAN = nb_intf.UntaggedVLAN
		iface.TaggedVLANs = nb_intf.TaggedVLANs
		iface.SwitchportMode = models.NetboxModeToSwitchportMode(nb_intf.Mode)
		iface.CableID = nb_intf.CableID
		iface.Label = nb_intf.Label
		iface.ParentID = nb_intf.ParentID
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "device_id"}, {Name: "netbox_id"}},
			UpdateAll: true,
		}).Create(&iface).Error; err != nil {
			return err
		}

		if err := syncAddresses(db, iface.ID, nb_intf.Addresses); err != nil {
			return err
		}
		if err := syncTags(db, 0, iface.ID, nb_intf.Tags); err != nil {
			return err
		}
		if role := optical.NormalizeOpticalPortRole(optical.CustomFieldValue(nb_intf.CustomFields, "optical_role")); role != "" {
			if err := upsertOpticalPortRole(db, iface.ID, role); err != nil {
				return err
			}
		}
	}

	for _, intf := range existing {
		if !seen[intf.NetboxID] {
			if err := deleteInterface(db, intf.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncAddresses creates/updates the addresses of an interface, matched
// by netbox_id, and removes any factum address no longer present in
// nb_addresses.
func syncAddresses(db *gorm.DB, interfaceID uint, nb_addresses []netboxtool.NBAddress) error {
	var existing []models.Address
	if err := db.Where("interface_id = ?", interfaceID).Find(&existing).Error; err != nil {
		return err
	}
	byNetboxID := make(map[uint]models.Address, len(existing))
	for _, addr := range existing {
		byNetboxID[addr.NetboxID] = addr
	}

	seen := make(map[uint]bool, len(nb_addresses))
	for _, nb_addr := range nb_addresses {
		seen[nb_addr.NetboxID] = true

		addr := byNetboxID[nb_addr.NetboxID]
		addr.InterfaceID = interfaceID
		addr.NetboxID = nb_addr.NetboxID
		addr.Address = nb_addr.Address
		addr.VRF = nb_addr.VRF
		addr.Role = nb_addr.Role
		if err := db.Save(&addr).Error; err != nil {
			return err
		}
	}

	for _, addr := range existing {
		if !seen[addr.NetboxID] {
			if err := db.Delete(&models.Address{}, addr.ID).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// syncTags creates/updates tags for a device (interfaceID == 0) or an
// interface (deviceID == 0), matched by netbox_id, and removes any
// factum tag no longer present in nb_tags. Exactly one of
// deviceID/interfaceID is non-zero; the unused side is stored as SQL
// NULL (see models.Tag), so it must be queried/set as NULL, not 0.
func syncTags(db *gorm.DB, deviceID, interfaceID uint, nb_tags []netboxtool.NBTag) error {
	var existing []models.Tag
	q := db
	if deviceID != 0 {
		q = q.Where("device_id = ? AND interface_id IS NULL", deviceID)
	} else {
		q = q.Where("interface_id = ? AND device_id IS NULL", interfaceID)
	}
	if err := q.Find(&existing).Error; err != nil {
		return err
	}
	byNetboxID := make(map[uint]models.Tag, len(existing))
	for _, tag := range existing {
		byNetboxID[tag.NetboxID] = tag
	}

	seen := make(map[uint]bool, len(nb_tags))
	for _, nb_tag := range nb_tags {
		seen[nb_tag.NetboxID] = true

		tag := byNetboxID[nb_tag.NetboxID]
		if deviceID != 0 {
			tag.DeviceID = &deviceID
			tag.InterfaceID = nil
		} else {
			tag.InterfaceID = &interfaceID
			tag.DeviceID = nil
		}
		tag.NetboxID = nb_tag.NetboxID
		tag.Name = nb_tag.Name
		if err := db.Save(&tag).Error; err != nil {
			return err
		}
	}

	for _, tag := range existing {
		if !seen[tag.NetboxID] {
			if err := db.Delete(&models.Tag{}, tag.ID).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// upsertOpticalPortRole writes OpticalPort.Role from the NetBox
// optical_role custom field without touching freq_hz / notes the GUI set.
func upsertOpticalPortRole(db *gorm.DB, interfaceID uint, role string) error {
	var port models.OpticalPort
	err := db.Where("interface_id = ?", interfaceID).First(&port).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(&models.OpticalPort{InterfaceID: interfaceID, Role: role}).Error
	}
	if err != nil {
		return err
	}
	if port.Role == role {
		return nil
	}
	return db.Model(&port).Update("role", role).Error
}

// deleteInterface removes an interface together with its addresses, tags
// and any Connection referencing it as either end - a single-device sync
// (e.g. the Netbox webhook) never runs syncCables (that's fullSync-gated),
// so a stale Connection row pointing at this interface would otherwise
// outlive it until the next full sync.
func deleteInterface(db *gorm.DB, interfaceID uint) error {
	if err := db.Where("interface_id = ?", interfaceID).Delete(&models.Address{}).Error; err != nil {
		return err
	}
	if err := db.Where("interface_id = ?", interfaceID).Delete(&models.Tag{}).Error; err != nil {
		return err
	}
	if err := db.Where("interface_a_id = ? OR interface_b_id = ?", interfaceID, interfaceID).Delete(&models.Connection{}).Error; err != nil {
		return err
	}
	if err := db.Where("interface_id = ?", interfaceID).Delete(&models.OpticalPort{}).Error; err != nil {
		return err
	}
	if err := db.Where("interface_a_id = ? OR interface_b_id = ?", interfaceID, interfaceID).Delete(&models.OpticalXConnect{}).Error; err != nil {
		return err
	}
	_ = optical.MarkStaleByInterface(db, interfaceID)
	return db.Delete(&models.Interface{}, interfaceID).Error
}

// deleteDevice removes a device together with its interfaces (and their
// addresses, tags and connections — see deleteInterface) and device-level
// tags. Shared by full-sync cleanup and the webhook's single-object delete.
func deleteDevice(db *gorm.DB, device models.Device) error {
	var interfaces []models.Interface
	if err := db.Where("device_id = ?", device.ID).Find(&interfaces).Error; err != nil {
		return err
	}
	for _, intf := range interfaces {
		if err := deleteInterface(db, intf.ID); err != nil {
			return err
		}
	}
	if err := db.Where("device_id = ?", device.ID).Delete(&models.Tag{}).Error; err != nil {
		return err
	}
	return db.Delete(&models.Device{}, device.ID).Error
}

// DeleteDeviceByNetboxID removes one netbox-sourced device (or VM) by its
// Netbox primary key. Used by the webhook delete path: Netbox has already
// removed the object, so it cannot be re-fetched and upserted. Returns 1 if
// a row was deleted, 0 if none matched (already gone, or cf_source !=
// "netbox" — matching deleteMissingDevices' guard). vm must match the
// object's table: Netbox's dcim.Device and virtualization.VirtualMachine
// IDs are independent sequences, so a device and a VM can share a NetboxID.
func DeleteDeviceByNetboxID(db *gorm.DB, netboxID uint, vm bool) (int, error) {
	var device models.Device
	err := db.Where("cf_source = ? AND vm = ? AND netbox_id = ?", "netbox", vm, netboxID).
		First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := deleteDevice(db, device); err != nil {
		return 0, err
	}
	return 1, nil
}

// deleteMissingDevices removes netbox-sourced devices/VMs no longer present
// in Netbox. Physical devices and VMs are checked against their own synced-ID
// list (rather than one combined list) since Netbox's dcim.Device and
// virtualization.VirtualMachine tables have independent ID sequences, and a
// device and a VM can share the same NetboxID.
func deleteMissingDevices(db *gorm.DB, syncedDeviceIDs, syncedVMIDs []uint) (int, error) {
	var stale []models.Device
	err := db.Where("cf_source = ? AND vm = ? AND netbox_id NOT IN ?", "netbox", false, syncedDeviceIDs).
		Or("cf_source = ? AND vm = ? AND netbox_id NOT IN ?", "netbox", true, syncedVMIDs).
		Find(&stale).Error
	if err != nil {
		return 0, err
	}

	for _, device := range stale {
		if err := deleteDevice(db, device); err != nil {
			return 0, err
		}
	}

	return len(stale), nil
}
