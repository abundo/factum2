package netbox

import (
	"errors"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// nbPrefixVRF is the nested VRF NetBox returns on an ipam.prefix. A null
// VRF is the global table, which Factum stores on the default VRF.
type nbPrefixVRF struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type nbPrefixREST struct {
	ID          uint         `json:"id"`
	Prefix      string       `json:"prefix"`
	Description string       `json:"description"`
	VRF         *nbPrefixVRF `json:"vrf"`
}

func (p nbPrefixREST) vrfID() uint {
	if p.VRF == nil {
		return 0
	}
	return p.VRF.ID
}

// slotKey is one VRF plus one CIDR. The same CIDR may exist in another
// VRF, and more than once in a VRF whose Unique IP Space is off.
func slotKey(vrfID uint, cidr string) string {
	return strconv.FormatUint(uint64(vrfID), 10) + " " + cidr
}

// prefixSpaceUnique reports whether an exact CIDR may appear only once
// in this VRF. The global table (default VRF) follows NetBox
// ENFORCE_GLOBAL_UNIQUE, which defaults to on. A NetBox VRF follows
// Unique IP Space.
func prefixSpaceUnique(v models.IpamVRF) bool {
	if v.IsDefault {
		return true
	}
	return v.EnforceUnique
}

// syncPrefixes mirrors NetBox ipam.prefix rows into the Netbox namespace.
// VRFs must already have been synced. A prefix with no VRF is stored on
// that namespace's default VRF. The same CIDR may exist in the global
// table and in any VRF. Factum-created prefixes are left alone. An empty
// fetch does not wipe already-synced rows.
func syncPrefixes(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbPrefixREST](nb, "/api/ipam/prefixes/")
	if err != nil {
		return err
	}

	nsID, err := netboxNamespaceID(db)
	if err != nil {
		return err
	}
	var vrfs []models.IpamVRF
	if err := db.Find(&vrfs).Error; err != nil {
		return err
	}
	var defaultVRF models.IpamVRF
	vrfByNetbox := map[uint]models.IpamVRF{}
	for _, v := range vrfs {
		if v.NamespaceID == nsID && v.IsDefault {
			defaultVRF = v
		}
		if v.NetboxID != 0 {
			vrfByNetbox[v.NetboxID] = v
		}
	}
	if defaultVRF.ID == 0 {
		return errors.New("Netbox namespace default VRF is missing")
	}

	var existing []models.IpamPrefix
	if err := db.Find(&existing).Error; err != nil {
		return err
	}
	byNetbox := map[uint]models.IpamPrefix{}
	bySlot := map[string][]models.IpamPrefix{}
	for _, row := range existing {
		if row.NetboxID != 0 {
			byNetbox[row.NetboxID] = row
		}
		k := slotKey(row.VRFID, row.Prefix)
		bySlot[k] = append(bySlot[k], row)
	}

	inPayload := map[uint]bool{}
	for _, row := range rows {
		if row.ID != 0 {
			inPayload[row.ID] = true
		}
	}

	state := &prefixSyncState{
		db:          db,
		defaultVRF:  defaultVRF,
		vrfByNetbox: vrfByNetbox,
		byNetbox:    byNetbox,
		bySlot:      bySlot,
		inPayload:   inPayload,
		reporter:    reporter,
	}

	seen := map[uint]bool{}
	var countNew, countUpdated, countSkipped int
	for _, row := range rows {
		// A row we already mirror stays, even when this pass cannot apply
		// the new CIDR. Otherwise the delete pass would drop it.
		if row.ID != 0 {
			if _, ok := state.byNetbox[row.ID]; ok {
				seen[row.ID] = true
			}
		}
		created, updated, skipped, err := state.upsert(row)
		if err != nil {
			return err
		}
		if row.ID != 0 && skipped == 0 {
			seen[row.ID] = true
		}
		countNew += created
		countUpdated += updated
		countSkipped += skipped
	}

	var countDeleted int
	if len(rows) > 0 {
		var synced []models.IpamPrefix
		if err := db.Where("source = ? AND netbox_id != 0", models.VRFSourceNetbox).Find(&synced).Error; err != nil {
			return err
		}
		for _, row := range synced {
			if seen[row.NetboxID] {
				continue
			}
			n, err := deleteNetboxPrefix(db, row, reporter)
			if err != nil {
				return err
			}
			countDeleted += n
		}
	}

	reporter.Emit(jobevent.Info, "Netbox prefix sync: %d new, %d updated, %d deleted, %d skipped",
		countNew, countUpdated, countDeleted, countSkipped)
	return nil
}

type prefixSyncState struct {
	db          *gorm.DB
	defaultVRF  models.IpamVRF
	vrfByNetbox map[uint]models.IpamVRF
	byNetbox    map[uint]models.IpamPrefix
	bySlot      map[string][]models.IpamPrefix
	inPayload   map[uint]bool
	reporter    jobevent.Reporter
}

func (s *prefixSyncState) remember(row models.IpamPrefix) {
	if row.NetboxID != 0 {
		s.byNetbox[row.NetboxID] = row
	}
	k := slotKey(row.VRFID, row.Prefix)
	rows := s.bySlot[k]
	for i, got := range rows {
		if got.ID == row.ID {
			rows[i] = row
			s.bySlot[k] = rows
			return
		}
	}
	s.bySlot[k] = append(rows, row)
}

func (s *prefixSyncState) forget(row models.IpamPrefix) {
	k := slotKey(row.VRFID, row.Prefix)
	rows := s.bySlot[k]
	kept := rows[:0]
	for _, got := range rows {
		if got.ID != row.ID {
			kept = append(kept, got)
		}
	}
	if len(kept) == 0 {
		delete(s.bySlot, k)
		return
	}
	s.bySlot[k] = kept
}

// occupant returns another row that already holds this exact CIDR in the
// VRF, when that VRF does not allow a duplicate.
func (s *prefixSyncState) occupant(vrf models.IpamVRF, cidr string, selfID uint) (models.IpamPrefix, bool) {
	if !prefixSpaceUnique(vrf) {
		return models.IpamPrefix{}, false
	}
	for _, got := range s.bySlot[slotKey(vrf.ID, cidr)] {
		if got.ID != selfID {
			return got, true
		}
	}
	return models.IpamPrefix{}, false
}

func (s *prefixSyncState) resolveVRF(row nbPrefixREST) (models.IpamVRF, bool) {
	if row.vrfID() == 0 {
		return s.defaultVRF, true
	}
	v, ok := s.vrfByNetbox[row.vrfID()]
	if !ok || v.NamespaceID == 0 {
		return models.IpamVRF{}, false
	}
	return v, true
}

func (s *prefixSyncState) upsert(row nbPrefixREST) (created, updated, skipped int, err error) {
	raw := strings.TrimSpace(row.Prefix)
	if row.ID == 0 || raw == "" {
		s.reporter.Emit(jobevent.Warning, "Netbox prefix sync: skipping prefix with no id or CIDR")
		return 0, 0, 1, nil
	}
	parsed, err := ipam.ParsePrefix(raw)
	if err != nil {
		s.reporter.Emit(jobevent.Warning, "Netbox prefix sync: skipping %q (netbox_id=%d); %s", raw, row.ID, err.Error())
		return 0, 0, 1, nil
	}
	cidr := parsed.String()
	vrf, ok := s.resolveVRF(row)
	if !ok {
		name := ""
		if row.VRF != nil {
			name = row.VRF.Name
		}
		s.reporter.Emit(jobevent.Warning, "Netbox prefix sync: skipping %s (netbox_id=%d); VRF %q (netbox_id=%d) is not in Factum", cidr, row.ID, name, row.vrfID())
		return 0, 0, 1, nil
	}
	desc := clipDescription(row.Description)
	family := 4
	if parsed.Addr().Is6() {
		family = 6
	}
	fields := map[string]any{
		"namespace_id": vrf.NamespaceID,
		"vrf_id":       vrf.ID,
		"prefix":       cidr,
		"family":       family,
		"description":  desc,
		"source":       models.VRFSourceNetbox,
		"netbox_id":    row.ID,
	}
	if existing, ok := s.byNetbox[row.ID]; ok {
		if _, taken := s.occupant(vrf, cidr, existing.ID); taken {
			s.reporter.Emit(jobevent.Warning, "Netbox prefix sync: skipping %s (netbox_id=%d); that CIDR is already allocated in this VRF", cidr, row.ID)
			return 0, 0, 1, nil
		}
		if err := s.db.Model(&models.IpamPrefix{}).Where("id = ?", existing.ID).Updates(fields).Error; err != nil {
			return 0, 0, 0, err
		}
		s.forget(existing)
		existing.Prefix = cidr
		existing.Description = desc
		existing.VRFID = vrf.ID
		existing.Family = family
		existing.NamespaceID = vrf.NamespaceID
		existing.Source = models.VRFSourceNetbox
		s.remember(existing)
		return 0, 1, 0, nil
	}

	if other, taken := s.occupant(vrf, cidr, 0); taken {
		if other.Source == models.VRFSourceNetbox && other.NetboxID != 0 && !s.inPayload[other.NetboxID] {
			adopted, err := s.adopt(other, row.ID, cidr, fields)
			if err != nil {
				return 0, 0, 0, err
			}
			if !adopted {
				return 0, 0, 1, nil
			}
			return 0, 1, 0, nil
		}
		if other.Source == models.VRFSourceNetbox {
			s.reporter.Emit(jobevent.Warning, "Netbox prefix sync: skipping %s (netbox_id=%d); NetBox prefix %d already has that CIDR in this VRF", cidr, row.ID, other.NetboxID)
		} else {
			s.reporter.Emit(jobevent.Warning, "Netbox prefix sync: skipping %s (netbox_id=%d); a Factum prefix already has that CIDR in this VRF", cidr, row.ID)
		}
		return 0, 0, 1, nil
	}

	createdRow := models.IpamPrefix{
		NamespaceID: vrf.NamespaceID,
		VRFID:       vrf.ID,
		Prefix:      cidr,
		Family:      family,
		Description: desc,
		Source:      models.VRFSourceNetbox,
		NetboxID:    row.ID,
	}
	if err := s.db.Create(&createdRow).Error; err != nil {
		return 0, 0, 0, err
	}
	s.remember(createdRow)
	return 1, 0, 0, nil
}

// adopt moves a NetBox prefix row whose id disappeared onto the new NetBox
// id that now owns the same CIDR, so addresses keep their prefix_id.
// A row that still has addresses and cannot be retargeted is left in place.
func (s *prefixSyncState) adopt(other models.IpamPrefix, newID uint, cidr string, fields map[string]any) (bool, error) {
	if err := s.db.Model(&models.IpamPrefix{}).Where("id = ?", other.ID).Updates(fields).Error; err != nil {
		return false, err
	}
	oldID := other.NetboxID
	delete(s.byNetbox, oldID)
	s.forget(other)
	other.NetboxID = newID
	other.Prefix = cidr
	other.Source = models.VRFSourceNetbox
	if v, ok := fields["vrf_id"].(uint); ok {
		other.VRFID = v
	}
	if ns, ok := fields["namespace_id"].(uint); ok {
		other.NamespaceID = ns
	}
	if d, ok := fields["description"].(string); ok {
		other.Description = d
	}
	s.remember(other)
	return true, nil
}

func clipDescription(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= 255 {
		return s
	}
	return string(r[:255])
}

func deleteNetboxPrefix(db *gorm.DB, row models.IpamPrefix, reporter jobevent.Reporter) (int, error) {
	var n int64
	if err := db.Model(&models.Address{}).Where("prefix_id = ?", row.ID).Count(&n).Error; err != nil {
		return 0, err
	}
	if n > 0 {
		reporter.Emit(jobevent.Warning, "Netbox prefix sync: keeping %s (netbox_id=%d); still has IP addresses but is missing from NetBox", row.Prefix, row.NetboxID)
		return 0, nil
	}
	if err := db.Delete(&row).Error; err != nil {
		return 0, err
	}
	return 1, nil
}
