package dcim

import (
	"errors"
	"net/http"
	"sort"
	"strings"

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
	a, b, err := loadCableEnds(db, ifaceAID, ifaceBID)
	if err != nil {
		return nil, err
	}
	if err := assertInterfacesFree(db, a.ID, b.ID, 0); err != nil {
		return nil, err
	}
	conn := models.Connection{
		DeviceAID:    a.DeviceID,
		InterfaceAID: a.ID,
		DeviceBID:    b.DeviceID,
		InterfaceBID: b.ID,
		Label:        strings.TrimSpace(label),
	}
	if err := db.Create(&conn).Error; err != nil {
		return nil, err
	}
	return &conn, nil
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
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errf(http.StatusConflict, ReasonConflict, "one of the interfaces already has a cable")
	}
	return nil
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
