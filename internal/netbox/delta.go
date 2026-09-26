package netbox

import (
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// A delta that must refetch this many devices, and at least this share of
// the NetBox devices already stored, is cheaper as one full inventory
// pass. Same floor as the old webhook burst switch.
const (
	deltaFullSyncPercent = 20
	deltaFullSyncMin     = 10
)

// preferFullSync reports whether pending device refetches should be one
// full SyncDB. known is the number of NetBox-sourced devices already in
// factum; zero means the names are not in the local inventory yet, so a
// large enough batch is still one full sync.
func preferFullSync(pending, known int) bool {
	if pending < deltaFullSyncMin {
		return false
	}
	if known <= 0 {
		return true
	}
	return int64(pending)*100 >= int64(known)*int64(deltaFullSyncPercent)
}

// SyncDelta applies NetBox object-changes since the cursor stored by the
// last successful full or delta sync. With no cursor it runs a full sync,
// which records one. A wide device set also falls back to a full sync.
func SyncDelta(c *util.ConfigRoot, reporter jobevent.Reporter) error {
	db, err := util.ConnectDatabase(&c.DB)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	return SyncDeltaDB(db, reporter)
}

// SyncDeltaDB is SyncDelta against an already-connected database.
func SyncDeltaDB(db *gorm.DB, reporter jobevent.Reporter) error {
	return withNetboxSyncLock(db, func() error {
		return syncDeltaDB(db, reporter)
	})
}

func syncDeltaDB(db *gorm.DB, reporter jobevent.Reporter) error {
	reporter.Emit(jobevent.Info, "Netbox delta sync started")

	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if settings.NetboxChangelogAt == nil {
		reporter.Emit(jobevent.Info, "Netbox delta sync: no cursor yet, running full sync")
		return syncDB(db, "", reporter)
	}

	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   settings.NetboxApiURL,
		Token: settings.NetboxApiToken,
	})
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	cursorAt := settings.NetboxChangelogAt.UTC().Truncate(time.Microsecond)
	listed, err := nb.ObjectChangesSince(cursorAt)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	fresh := make([]netboxtool.ObjectChange, 0, len(listed))
	for _, ch := range listed {
		if afterCursor(ch, cursorAt, settings.NetboxChangelogID) {
			fresh = append(fresh, ch)
		}
	}
	if len(fresh) == 0 {
		reporter.Emit(jobevent.Info, "Netbox delta sync: no changes")
		return nil
	}

	plan, err := newDeltaPlan(fresh, func(vmIface bool, ifaceID uint) (uint, bool, error) {
		parent, _, ok, err := parentDevice(db, nb, ifaceID, vmIface)
		return parent, ok, err
	})
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	var known int64
	if err := db.Model(&models.Device{}).Where("cf_source = ?", "netbox").Count(&known).Error; err != nil {
		reporter.EmitErr(err)
		return err
	}
	pending := len(plan.devices) + len(plan.vms)
	if preferFullSync(pending, int(known)) {
		reporter.Emit(jobevent.Info, "Netbox delta sync: %d devices changed, running full sync", pending)
		return syncDB(db, "", reporter)
	}

	if err := plan.apply(db, nb, reporter); err != nil {
		reporter.EmitErr(err)
		return err
	}

	mark := newestChange(fresh)
	if err := saveNetboxChangelogCursor(db, settings.ID, mark); err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Netbox delta sync: cursor %s", mark.At.Format(time.RFC3339))
	return nil
}

func afterCursor(ch netboxtool.ObjectChange, cursorAt time.Time, cursorID uint) bool {
	if ch.Time.After(cursorAt) {
		return true
	}
	if ch.Time.Before(cursorAt) {
		return false
	}
	return ch.ID > cursorID
}

func newestChange(changes []netboxtool.ObjectChange) netboxtool.ChangelogMark {
	var mark netboxtool.ChangelogMark
	for _, ch := range changes {
		if ch.Time.After(mark.At) || (ch.Time.Equal(mark.At) && ch.ID >= mark.ID) {
			mark.At = ch.Time
			mark.ID = ch.ID
		}
	}
	return mark
}

func saveNetboxChangelogCursor(db *gorm.DB, settingsID uint, mark netboxtool.ChangelogMark) error {
	if mark.At.IsZero() {
		return nil
	}
	return db.Model(&models.Settings{}).Where("id = ?", settingsID).Updates(map[string]any{
		"netbox_changelog_at": mark.At.UTC().Truncate(time.Microsecond),
		"netbox_changelog_id": mark.ID,
	}).Error
}

// deltaPlan is the set of factum objects to refresh from one changelog
// window. Device and VM ids are NetBox ids. Cable and site maps hold the
// last action for that id ("create", "update", or "delete").
type deltaPlan struct {
	devices    map[uint]struct{}
	vms        map[uint]struct{}
	delDevices map[uint]struct{}
	delVMs     map[uint]struct{}
	cables     map[uint]string
	sites      map[uint]string
	regions    map[uint]string
	locations  map[uint]string
	racks      bool
	vrfs       bool
	l2vpn      bool
	devTypes   bool
}

// parentLookup resolves an interface netbox id to its device netbox id.
// ok is false when the interface is already gone.
type parentLookup func(vmIface bool, ifaceID uint) (deviceID uint, ok bool, err error)

func newDeltaPlan(changes []netboxtool.ObjectChange, lookup parentLookup) (*deltaPlan, error) {
	p := &deltaPlan{
		devices:    map[uint]struct{}{},
		vms:        map[uint]struct{}{},
		delDevices: map[uint]struct{}{},
		delVMs:     map[uint]struct{}{},
		cables:     map[uint]string{},
		sites:      map[uint]string{},
		regions:    map[uint]string{},
		locations:  map[uint]string{},
	}
	for _, ch := range changes {
		if err := p.note(ch, lookup); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *deltaPlan) note(ch netboxtool.ObjectChange, lookup parentLookup) error {
	switch ch.ChangedType {
	case "dcim.device":
		p.noteDevice(ch.ChangedID, false, ch.Action)
	case "virtualization.virtualmachine":
		p.noteDevice(ch.ChangedID, true, ch.Action)
	case "dcim.interface":
		if ch.RelatedType == "dcim.device" {
			p.noteDevice(ch.RelatedID, false, "update")
		}
	case "virtualization.vminterface":
		if ch.RelatedType == "virtualization.virtualmachine" {
			p.noteDevice(ch.RelatedID, true, "update")
		}
	case "ipam.ipaddress":
		switch ch.RelatedType {
		case "dcim.interface", "virtualization.vminterface":
			if lookup == nil || ch.RelatedID == 0 {
				return nil
			}
			parent, ok, err := lookup(ch.RelatedType == "virtualization.vminterface", ch.RelatedID)
			if err != nil {
				return err
			}
			if ok {
				p.noteDevice(parent, ch.RelatedType == "virtualization.vminterface", "update")
			}
		case "dcim.device":
			p.noteDevice(ch.RelatedID, false, "update")
		case "virtualization.virtualmachine":
			p.noteDevice(ch.RelatedID, true, "update")
		}
	case "dcim.cable":
		if ch.ChangedID != 0 {
			p.cables[ch.ChangedID] = ch.Action
		}
	case "dcim.site":
		if ch.ChangedID != 0 {
			p.sites[ch.ChangedID] = ch.Action
		}
	case "dcim.region":
		if ch.ChangedID != 0 {
			p.regions[ch.ChangedID] = ch.Action
		}
	case "dcim.location":
		if ch.ChangedID != 0 {
			p.locations[ch.ChangedID] = ch.Action
		}
	case "dcim.rack":
		p.racks = true
	case "ipam.vrf":
		p.vrfs = true
	case "vpn.l2vpn", "vpn.l2vpntermination":
		p.l2vpn = true
	case "dcim.devicetype":
		p.devTypes = true
	}
	return nil
}

// noteDevice records the latest action for one device or VM. A later
// create/update after a delete refetches; a later delete drops the refetch.
func (p *deltaPlan) noteDevice(id uint, vm bool, action string) {
	if id == 0 {
		return
	}
	refetch, del := p.devices, p.delDevices
	if vm {
		refetch, del = p.vms, p.delVMs
	}
	if action == "delete" {
		delete(refetch, id)
		del[id] = struct{}{}
		return
	}
	delete(del, id)
	refetch[id] = struct{}{}
}

type parentRow struct {
	NetboxID uint
	VM       bool
}

func parentDevice(db *gorm.DB, nb *netboxtool.NetboxClient, ifaceID uint, vmIface bool) (uint, bool, bool, error) {
	var rows []parentRow
	err := db.Model(&models.Interface{}).
		Select("devices.netbox_id as netbox_id, devices.vm as vm").
		Joins("JOIN devices ON devices.id = interfaces.device_id").
		Where("interfaces.netbox_id = ? AND devices.vm = ?", ifaceID, vmIface).
		Scan(&rows).Error
	if err != nil {
		return 0, false, false, err
	}
	if len(rows) == 1 && rows[0].NetboxID != 0 {
		return rows[0].NetboxID, rows[0].VM, true, nil
	}
	parent, ok, err := nb.InterfaceParent(vmIface, ifaceID)
	if err != nil || !ok {
		return 0, false, false, err
	}
	return parent, vmIface, true, nil
}

func (p *deltaPlan) apply(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	var deleted int
	for id := range p.delDevices {
		n, err := DeleteDeviceByNetboxID(db, id, false)
		if err != nil {
			return err
		}
		deleted += n
	}
	for id := range p.delVMs {
		n, err := DeleteDeviceByNetboxID(db, id, true)
		if err != nil {
			return err
		}
		deleted += n
	}

	var devices []*netboxtool.NBDevice
	var created, updated int
	if len(p.devices)+len(p.vms) > 0 {
		dnsNames, err := fetchAddressDNSNames(nb, reporter)
		if err != nil {
			return err
		}
		for id := range p.devices {
			n, u, d, dev, err := refetchDevice(db, nb, id, false, dnsNames)
			if err != nil {
				return err
			}
			created += n
			updated += u
			deleted += d
			if dev != nil {
				devices = append(devices, dev)
			}
		}
		for id := range p.vms {
			n, u, d, dev, err := refetchDevice(db, nb, id, true, dnsNames)
			if err != nil {
				return err
			}
			created += n
			updated += u
			deleted += d
			if dev != nil {
				devices = append(devices, dev)
			}
		}
	}
	reporter.Emit(jobevent.Info, "Netbox delta sync: %d new, %d updated, %d deleted", created, updated, deleted)

	if len(devices) > 0 {
		if err := syncDeviceTypeTemplates(db, nb, devices); err != nil {
			return err
		}
	}

	if err := p.applyCables(db, nb, reporter); err != nil {
		return err
	}
	if err := p.applyTree(db, p.sites, models.SiteNetboxKindSite, reporter); err != nil {
		return err
	}
	if err := p.applyTree(db, p.regions, models.SiteNetboxKindRegion, reporter); err != nil {
		return err
	}
	if err := p.applyTree(db, p.locations, models.SiteNetboxKindLocation, reporter); err != nil {
		return err
	}
	if p.racks || p.devTypes || len(p.devices) > 0 {
		if err := syncRacksAndPlacements(db, nb, reporter); err != nil {
			return err
		}
	}
	if p.vrfs {
		if err := syncVRFs(db, nb, reporter); err != nil {
			return err
		}
	}
	if p.l2vpn {
		if err := syncServiceEndpointsFromL2VPNs(db, nb, reporter); err != nil {
			return err
		}
	}
	return nil
}

func refetchDevice(db *gorm.DB, nb *netboxtool.NetboxClient, id uint, vm bool, dnsNames map[uint]string) (created, updated, deleted int, dev *netboxtool.NBDevice, err error) {
	if vm {
		dev, err = nb.GetVM("", int(id))
	} else {
		dev, err = nb.GetDevice("", int(id))
	}
	if err != nil {
		return 0, 0, 0, nil, err
	}
	if dev == nil {
		n, err := DeleteDeviceByNetboxID(db, id, vm)
		return 0, 0, n, nil, err
	}
	isNew, err := syncDevice(db, dev, dnsNames)
	if err != nil {
		return 0, 0, 0, nil, err
	}
	if isNew {
		return 1, 0, 0, dev, nil
	}
	return 0, 1, 0, dev, nil
}

func (p *deltaPlan) applyCables(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	if len(p.cables) == 0 {
		return nil
	}
	var created, updated, deleted, skipped int
	for id, action := range p.cables {
		if action == "delete" {
			n, err := DeleteConnectionByNetboxID(db, id)
			if err != nil {
				return err
			}
			deleted += n
			continue
		}
		cable, err := nb.GetInterfaceCable(id)
		if err != nil {
			return err
		}
		c, u, d, s, err := ApplyCable(db, id, cable)
		if err != nil {
			return err
		}
		created += c
		updated += u
		deleted += d
		skipped += s
	}
	reporter.Emit(jobevent.Info, "Netbox cable sync: %d new, %d updated, %d deleted, %d skipped (unresolved endpoint)",
		created, updated, deleted, skipped)
	if err := optical.RebuildStale(db); err != nil {
		reporter.Emit(jobevent.Warning, "optical retrace after cable sync: %v", err)
	}
	return nil
}

func (p *deltaPlan) applyTree(db *gorm.DB, actions map[uint]string, kind string, reporter jobevent.Reporter) error {
	for id, action := range actions {
		if action == "delete" {
			if _, err := DeleteSyncedSiteNode(db, kind, id); err != nil {
				return err
			}
			continue
		}
		if err := SyncDCIMTreeItem(db, kind, id, reporter); err != nil {
			return err
		}
	}
	return nil
}
