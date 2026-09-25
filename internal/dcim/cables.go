package dcim

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func CableSource(netboxID uint) string {
	if netboxID != 0 {
		return "netbox"
	}
	return "factum"
}

// ConnectionPair loads two devices, every interface, and cables that
// terminate on those ports. aID and bID may be the same device.
func ConnectionPair(db *gorm.DB, aID, bID uint) (*PairDTO, error) {
	if aID == 0 || bID == 0 {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "device_a and device_b are required")
	}
	devA, err := loadPairDevice(db, aID)
	if err != nil {
		return nil, err
	}
	var devB PairDevice
	if bID == aID {
		devB = clonePairDevice(devA)
	} else {
		devB, err = loadPairDevice(db, bID)
		if err != nil {
			return nil, err
		}
	}

	ifaceIDs := make([]uint, 0, len(devA.Interfaces)+len(devB.Interfaces))
	for _, p := range devA.Interfaces {
		ifaceIDs = append(ifaceIDs, p.ID)
	}
	if bID != aID {
		for _, p := range devB.Interfaces {
			ifaceIDs = append(ifaceIDs, p.ID)
		}
	}
	conns, err := connectionsTouchingInterfaces(db, ifaceIDs)
	if err != nil {
		return nil, err
	}

	peerIDs := map[uint]bool{}
	peerIfaceIDs := map[uint]bool{}
	for _, c := range conns {
		peerIDs[c.DeviceAID] = true
		peerIDs[c.DeviceBID] = true
		peerIfaceIDs[c.InterfaceAID] = true
		peerIfaceIDs[c.InterfaceBID] = true
	}
	devNames, err := deviceNamesByID(db, keys(peerIDs))
	if err != nil {
		return nil, err
	}
	ifaceNames, err := interfaceNamesByID(db, keys(peerIfaceIDs))
	if err != nil {
		return nil, err
	}

	byIface := map[uint]models.Connection{}
	for _, c := range conns {
		byIface[c.InterfaceAID] = c
		byIface[c.InterfaceBID] = c
	}
	attachLinks := func(dev *PairDevice) {
		for i, p := range dev.Interfaces {
			c, ok := byIface[p.ID]
			if !ok {
				continue
			}
			peerDev, peerIface := c.DeviceBID, c.InterfaceBID
			if peerIface == p.ID {
				peerDev, peerIface = c.DeviceAID, c.InterfaceAID
			}
			link := PairLink{
				ID: c.ID, Label: c.Label, NetboxID: c.NetboxID, Source: CableSource(c.NetboxID),
				PeerDeviceID: peerDev, PeerDeviceName: devNames[peerDev],
				PeerInterfaceID: peerIface, PeerInterfaceName: ifaceNames[peerIface],
			}
			dev.Interfaces[i].Connection = &link
		}
	}
	attachLinks(&devA)
	attachLinks(&devB)

	cables := make([]PairCable, 0)
	seen := map[uint]bool{}
	aIfaces := map[uint]bool{}
	for _, p := range devA.Interfaces {
		aIfaces[p.ID] = true
	}
	bIfaces := map[uint]bool{}
	for _, p := range devB.Interfaces {
		bIfaces[p.ID] = true
	}
	for _, c := range conns {
		if seen[c.ID] {
			continue
		}
		aOnA := aIfaces[c.InterfaceAID]
		aOnB := bIfaces[c.InterfaceAID]
		bOnA := aIfaces[c.InterfaceBID]
		bOnB := bIfaces[c.InterfaceBID]
		between := (aOnA && bOnB) || (aOnB && bOnA)
		if !between {
			continue
		}
		seen[c.ID] = true
		cables = append(cables, PairCable{
			ID: c.ID, Label: c.Label, NetboxID: c.NetboxID, Source: CableSource(c.NetboxID),
			DeviceAID: c.DeviceAID, InterfaceAID: c.InterfaceAID,
			DeviceBID: c.DeviceBID, InterfaceBID: c.InterfaceBID,
		})
	}
	sort.Slice(cables, func(i, j int) bool { return cables[i].ID < cables[j].ID })

	return &PairDTO{DeviceA: devA, DeviceB: devB, Cables: cables}, nil
}

func CreateCable(db *gorm.DB, ifaceAID, ifaceBID uint, label string) (*models.Connection, error) {
	return createCable(db, ifaceAID, ifaceBID, label, 0)
}

// CreateLinkedCable stores a cable that already exists in NetBox. A webhook
// often inserts that netbox_id while this save is in flight; the existing
// row is kept instead of inserting a second one.
func CreateLinkedCable(db *gorm.DB, ifaceAID, ifaceBID uint, label string, netboxID uint) (*models.Connection, error) {
	if netboxID == 0 {
		return CreateCable(db, ifaceAID, ifaceBID, label)
	}
	return createCable(db, ifaceAID, ifaceBID, label, netboxID)
}

func createCable(db *gorm.DB, ifaceAID, ifaceBID uint, label string, netboxID uint) (*models.Connection, error) {
	a, b, err := loadCableEnds(db, ifaceAID, ifaceBID)
	if err != nil {
		return nil, err
	}
	existing, err := ExistingPairCable(db, a.ID, b.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return bindExistingPair(db, existing, a.ID, b.ID, netboxID)
	}
	conn := models.Connection{
		NetboxID:     netboxID,
		DeviceAID:    a.DeviceID,
		InterfaceAID: a.ID,
		DeviceBID:    b.DeviceID,
		InterfaceBID: b.ID,
		Label:        strings.TrimSpace(label),
	}
	if err := db.Create(&conn).Error; err != nil {
		if netboxID != 0 && isUniqueViolation(err) {
			if row, gerr := GetCableByNetboxID(db, netboxID); gerr == nil && CableJoins(row, a.ID, b.ID) {
				return row, nil
			}
		}
		return nil, err
	}
	return &conn, nil
}

// ExistingPairCable returns the cable that already joins ifaceAID and ifaceBID.
// Duplicate rows of that same pair collapse to one. A cable from either
// port to a different interface is a conflict.
func ExistingPairCable(db *gorm.DB, ifaceAID, ifaceBID uint) (*models.Connection, error) {
	conns, err := connectionsTouchingInterfaces(db, []uint{ifaceAID, ifaceBID})
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return nil, nil
	}
	same := make([]models.Connection, 0, len(conns))
	for _, c := range conns {
		if !CableJoins(&c, ifaceAID, ifaceBID) {
			return nil, interfacesBusy(db, ifaceAID, ifaceBID, conns)
		}
		same = append(same, c)
	}
	return keepOnePair(db, same)
}

func keepOnePair(db *gorm.DB, same []models.Connection) (*models.Connection, error) {
	sort.Slice(same, func(i, j int) bool {
		if (same[i].NetboxID != 0) != (same[j].NetboxID != 0) {
			return same[i].NetboxID != 0
		}
		return same[i].ID < same[j].ID
	})
	keeper := same[0]
	for _, extra := range same[1:] {
		_ = optical.MarkStaleByConnection(db, extra.ID)
		if err := db.Delete(&models.Connection{}, extra.ID).Error; err != nil {
			return nil, err
		}
	}
	return &keeper, nil
}

func bindExistingPair(db *gorm.DB, existing *models.Connection, ifaceAID, ifaceBID, netboxID uint) (*models.Connection, error) {
	if existing == nil {
		return nil, nil
	}
	if netboxID == 0 || existing.NetboxID == netboxID {
		return existing, nil
	}
	if existing.NetboxID != 0 {
		return nil, interfacesBusy(db, ifaceAID, ifaceBID, []models.Connection{*existing})
	}
	if err := SetCableNetboxID(db, existing.ID, netboxID); err != nil {
		if !isUniqueViolation(err) {
			return nil, err
		}
		winner, gerr := GetCableByNetboxID(db, netboxID)
		if gerr != nil || !CableJoins(winner, ifaceAID, ifaceBID) {
			return nil, err
		}
		if winner.ID != existing.ID {
			_ = optical.MarkStaleByConnection(db, existing.ID)
			if derr := db.Delete(&models.Connection{}, existing.ID).Error; derr != nil {
				return nil, derr
			}
		}
		return winner, nil
	}
	existing.NetboxID = netboxID
	return existing, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint") || strings.Contains(s, "duplicate key")
}

func UpdateCable(db *gorm.DB, id, ifaceAID, ifaceBID uint, label string) (*models.Connection, error) {
	var existing models.Connection
	if err := db.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errf(http.StatusNotFound, ReasonNotFound, "cable not found")
		}
		return nil, err
	}
	a, b, err := loadCableEnds(db, ifaceAID, ifaceBID)
	if err != nil {
		return nil, err
	}
	if err := assertInterfacesFree(db, a.ID, b.ID, existing.ID); err != nil {
		return nil, err
	}
	existing.DeviceAID = a.DeviceID
	existing.InterfaceAID = a.ID
	existing.DeviceBID = b.DeviceID
	existing.InterfaceBID = b.ID
	existing.Label = strings.TrimSpace(label)
	if err := db.Save(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func DeleteCable(db *gorm.DB, id uint) error {
	var existing models.Connection
	if err := db.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errf(http.StatusNotFound, ReasonNotFound, "cable not found")
		}
		return err
	}
	return db.Delete(&existing).Error
}

func SetCableNetboxID(db *gorm.DB, id, netboxID uint) error {
	return db.Model(&models.Connection{}).Where("id = ?", id).Update("netbox_id", netboxID).Error
}

// StampCableNetboxID sets netbox_id on localID. When a webhook row already
// holds that id and joins the same interfaces, that row is kept and localID
// is removed.
func StampCableNetboxID(db *gorm.DB, localID, netboxID, ifaceAID, ifaceBID uint) (uint, error) {
	if err := SetCableNetboxID(db, localID, netboxID); err != nil {
		if !isUniqueViolation(err) {
			return 0, err
		}
		winner, gerr := GetCableByNetboxID(db, netboxID)
		if gerr != nil || !CableJoins(winner, ifaceAID, ifaceBID) {
			return 0, err
		}
		if winner.ID != localID {
			_ = optical.MarkStaleByConnection(db, localID)
			if derr := db.Delete(&models.Connection{}, localID).Error; derr != nil {
				return 0, derr
			}
		}
		return winner.ID, nil
	}
	return localID, nil
}

func GetCable(db *gorm.DB, id uint) (*models.Connection, error) {
	var existing models.Connection
	if err := db.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errf(http.StatusNotFound, ReasonNotFound, "cable not found")
		}
		return nil, err
	}
	return &existing, nil
}

func loadPairDevice(db *gorm.DB, id uint) (PairDevice, error) {
	var d models.Device
	if err := db.First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PairDevice{}, errf(http.StatusNotFound, ReasonNotFound, "device not found")
		}
		return PairDevice{}, err
	}
	var ifaces []models.Interface
	if err := db.Where("device_id = ?", d.ID).Order("name").Find(&ifaces).Error; err != nil {
		return PairDevice{}, err
	}
	ports := make([]PairPort, 0, len(ifaces))
	for _, i := range ifaces {
		ports = append(ports, PairPort{
			ID: i.ID, Name: i.Name, Description: i.Description, Type: i.Type, Enabled: i.Enabled,
		})
	}
	return PairDevice{ID: d.ID, Name: d.Name, Site: d.Site, Interfaces: ports}, nil
}

func clonePairDevice(d PairDevice) PairDevice {
	out := d
	out.Interfaces = append([]PairPort(nil), d.Interfaces...)
	return out
}

func BothNetboxInterfaces(a, b models.Interface) bool {
	return a.NetboxID != 0 && b.NetboxID != 0
}

func LoadCableEnds(db *gorm.DB, ifaceAID, ifaceBID uint) (models.Interface, models.Interface, error) {
	return loadCableEnds(db, ifaceAID, ifaceBID)
}

func loadCableEnds(db *gorm.DB, ifaceAID, ifaceBID uint) (models.Interface, models.Interface, error) {
	if ifaceAID == 0 || ifaceBID == 0 {
		return models.Interface{}, models.Interface{}, errf(http.StatusBadRequest, ReasonInvalid, "interface_a_id and interface_b_id are required")
	}
	if ifaceAID == ifaceBID {
		return models.Interface{}, models.Interface{}, errf(http.StatusBadRequest, ReasonInvalid, "a cable cannot terminate on the same interface twice")
	}
	var a, b models.Interface
	if err := db.First(&a, ifaceAID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Interface{}, models.Interface{}, errf(http.StatusNotFound, ReasonNotFound, "interface a not found")
		}
		return models.Interface{}, models.Interface{}, err
	}
	if err := db.First(&b, ifaceBID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Interface{}, models.Interface{}, errf(http.StatusNotFound, ReasonNotFound, "interface b not found")
		}
		return models.Interface{}, models.Interface{}, err
	}
	return a, b, nil
}

func AssertInterfacesFree(db *gorm.DB, ifaceAID, ifaceBID, exceptID uint) error {
	return assertInterfacesFree(db, ifaceAID, ifaceBID, exceptID)
}

func assertInterfacesFree(db *gorm.DB, ifaceAID, ifaceBID, exceptID uint) error {
	q := db.Model(&models.Connection{}).Where(
		"interface_a_id IN ? OR interface_b_id IN ?",
		[]uint{ifaceAID, ifaceBID}, []uint{ifaceAID, ifaceBID},
	)
	if exceptID != 0 {
		q = q.Where("id <> ?", exceptID)
	}
	var conns []models.Connection
	if err := q.Find(&conns).Error; err != nil {
		return err
	}
	if len(conns) > 0 {
		return interfacesBusy(db, ifaceAID, ifaceBID, conns)
	}
	return nil
}

// CableJoins reports whether conn terminates on ifaceAID and ifaceBID in either order.
func CableJoins(conn *models.Connection, ifaceAID, ifaceBID uint) bool {
	if conn == nil {
		return false
	}
	return (conn.InterfaceAID == ifaceAID && conn.InterfaceBID == ifaceBID) ||
		(conn.InterfaceAID == ifaceBID && conn.InterfaceBID == ifaceAID)
}

// GetCableByNetboxID loads the Connection stored for a NetBox cable id.
func GetCableByNetboxID(db *gorm.DB, netboxID uint) (*models.Connection, error) {
	if netboxID == 0 {
		return nil, errf(http.StatusNotFound, ReasonNotFound, "cable not found")
	}
	var existing models.Connection
	if err := db.Where("netbox_id = ?", netboxID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errf(http.StatusNotFound, ReasonNotFound, "cable not found")
		}
		return nil, err
	}
	return &existing, nil
}

func interfacesBusy(db *gorm.DB, wantA, wantB uint, conns []models.Connection) error {
	sort.Slice(conns, func(i, j int) bool { return conns[i].ID < conns[j].ID })
	msg, err := describeBusyCable(db, wantA, wantB, conns)
	if err != nil || msg == "" {
		return errf(http.StatusConflict, ReasonConflict, "one of the interfaces already has a cable")
	}
	return errf(http.StatusConflict, ReasonConflict, "%s", msg)
}

func describeBusyCable(db *gorm.DB, wantA, wantB uint, conns []models.Connection) (string, error) {
	ids := map[uint]bool{wantA: true, wantB: true}
	for _, c := range conns {
		ids[c.InterfaceAID] = true
		ids[c.InterfaceBID] = true
	}
	var rows []models.Interface
	if err := db.Select("id", "name", "device_id").Where("id IN ?", keys(ids)).Find(&rows).Error; err != nil {
		return "", err
	}
	ifaces := make(map[uint]models.Interface, len(rows))
	devIDs := map[uint]bool{}
	for _, row := range rows {
		ifaces[row.ID] = row
		devIDs[row.DeviceID] = true
	}
	names, err := deviceNamesByID(db, keys(devIDs))
	if err != nil {
		return "", err
	}
	occupied := make([]string, 0, len(conns))
	for _, c := range conns {
		occupied = append(occupied, fmt.Sprintf("cable %d is %s to %s",
			c.ID, interfaceRef(names, ifaces, c.InterfaceAID), interfaceRef(names, ifaces, c.InterfaceBID)))
	}
	return fmt.Sprintf("one of the interfaces already has a cable: trying %s to %s; %s",
		interfaceRef(names, ifaces, wantA), interfaceRef(names, ifaces, wantB), strings.Join(occupied, "; ")), nil
}

func interfaceRef(deviceNames map[uint]string, ifaces map[uint]models.Interface, id uint) string {
	iface, ok := ifaces[id]
	name := "interface"
	device := ""
	if ok {
		if iface.Name != "" {
			name = iface.Name
		}
		device = deviceNames[iface.DeviceID]
	}
	if device == "" {
		return fmt.Sprintf("%s (interface %d)", name, id)
	}
	return fmt.Sprintf("%s %s (interface %d)", device, name, id)
}

func connectionsTouchingInterfaces(db *gorm.DB, ifaceIDs []uint) ([]models.Connection, error) {
	if len(ifaceIDs) == 0 {
		return nil, nil
	}
	var conns []models.Connection
	if err := db.Where("interface_a_id IN ? OR interface_b_id IN ?", ifaceIDs, ifaceIDs).Find(&conns).Error; err != nil {
		return nil, err
	}
	return conns, nil
}

func deviceNamesByID(db *gorm.DB, ids []uint) (map[uint]string, error) {
	out := map[uint]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []models.Device
	if err := db.Select("id", "name").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

func interfaceNamesByID(db *gorm.DB, ids []uint) (map[uint]string, error) {
	out := map[uint]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []models.Interface
	if err := db.Select("id", "name").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

func keys(m map[uint]bool) []uint {
	out := make([]uint, 0, len(m))
	for id := range m {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
}
