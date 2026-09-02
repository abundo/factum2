package optical

import (
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// Inventory is one device's optical ports and intra-device xconnects, as
// produced by a read-only driver (Open ROADM GetOpticalInventory) and
// applied onto Factum tables.
type Inventory struct {
	Kind      string              `json:"optical_kind"`
	Source    string              `json:"source"`
	Ports     []InventoryPort     `json:"ports"`
	XConnects []InventoryXConnect `json:"xconnects"`
}

type InventoryPort struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	FreqHz uint64 `json:"freq_hz"`
}

type InventoryXConnect struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	PortA  string `json:"port_a"`
	PortB  string `json:"port_b"`
	FreqHz uint64 `json:"freq_hz"`
}

// ApplyResult is what ApplyInventory changed (and skipped).
type ApplyResult struct {
	Kind              string   `json:"kind"`
	PortsUpserted     int      `json:"ports_upserted"`
	PortsSkipped      []string `json:"ports_skipped,omitempty"`
	XConnectsUpserted int      `json:"xconnects_upserted"`
	XConnectsDeleted  int      `json:"xconnects_deleted"`
	XConnectsSkipped  []string `json:"xconnects_skipped,omitempty"`
}

type ifaceIndex struct {
	byName   map[string]models.Interface
	byLower  map[string]models.Interface
	bySuffix map[string][]models.Interface
}

func indexInterfaces(ifaces []models.Interface) ifaceIndex {
	idx := ifaceIndex{
		byName:   make(map[string]models.Interface, len(ifaces)),
		byLower:  make(map[string]models.Interface, len(ifaces)),
		bySuffix: map[string][]models.Interface{},
	}
	for _, iface := range ifaces {
		idx.byName[iface.Name] = iface
		idx.byLower[strings.ToLower(iface.Name)] = iface
		suf := iface.Name
		if n := strings.LastIndex(iface.Name, "/"); n >= 0 && n < len(iface.Name)-1 {
			suf = iface.Name[n+1:]
		}
		key := strings.ToLower(suf)
		idx.bySuffix[key] = append(idx.bySuffix[key], iface)
	}
	return idx
}

func (idx ifaceIndex) match(name string) (models.Interface, bool) {
	if name == "" {
		return models.Interface{}, false
	}
	if iface, ok := idx.byName[name]; ok {
		return iface, true
	}
	if iface, ok := idx.byLower[strings.ToLower(name)]; ok {
		return iface, true
	}
	suf := name
	if n := strings.LastIndex(name, "/"); n >= 0 && n < len(name)-1 {
		suf = name[n+1:]
	}
	cands := idx.bySuffix[strings.ToLower(suf)]
	if len(cands) == 1 {
		return cands[0], true
	}
	return models.Interface{}, false
}

// ApplyInventory upserts Device.OpticalKind, OpticalPort rows, and
// driver-sourced OpticalXConnects for one chassis. Operator-created
// xconnects (Source empty) are left alone. Passthrough xconnects from
// inventory are skipped: ILA/passive walk uses OpticalKind, and ROADM
// internal-links are not λ adjacencies.
func ApplyInventory(db *gorm.DB, deviceID uint, inv Inventory) (*ApplyResult, error) {
	source := inv.Source
	if source == "" {
		source = models.XCSourceOpenROADM
	}
	res := &ApplyResult{}
	err := db.Transaction(func(tx *gorm.DB) error {
		var dev models.Device
		if err := tx.First(&dev, deviceID).Error; err != nil {
			return err
		}
		kind := NormalizeOpticalKindCF(inv.Kind)
		if kind != "" && dev.OpticalKind != kind {
			if err := tx.Model(&dev).Update("optical_kind", kind).Error; err != nil {
				return err
			}
			dev.OpticalKind = kind
		}
		res.Kind = dev.OpticalKind

		var ifaces []models.Interface
		if err := tx.Where("device_id = ?", deviceID).Find(&ifaces).Error; err != nil {
			return err
		}
		idx := indexInterfaces(ifaces)

		for _, p := range inv.Ports {
			if p.Role == "" || !models.AllowedOpticalPortRoles[p.Role] {
				if p.Name != "" {
					res.PortsSkipped = append(res.PortsSkipped, p.Name+": invalid role")
				}
				continue
			}
			iface, ok := idx.match(p.Name)
			if !ok {
				res.PortsSkipped = append(res.PortsSkipped, p.Name+": no matching interface")
				continue
			}
			if err := upsertInventoryPort(tx, iface.ID, p); err != nil {
				return err
			}
			res.PortsUpserted++
		}

		keep := map[uint]bool{}
		for _, xc := range inv.XConnects {
			id, skip := applyInventoryXConnect(tx, deviceID, source, idx, xc)
			if skip != "" {
				label := xc.Name
				if label == "" {
					label = xc.PortA + "↔" + xc.PortB
				}
				res.XConnectsSkipped = append(res.XConnectsSkipped, label+": "+skip)
				continue
			}
			keep[id] = true
			res.XConnectsUpserted++
		}

		var existing []models.OpticalXConnect
		if err := tx.Where("device_id = ? AND source = ?", deviceID, source).Find(&existing).Error; err != nil {
			return err
		}
		for _, row := range existing {
			if keep[row.ID] {
				continue
			}
			if err := tx.Delete(&models.OpticalXConnect{}, row.ID).Error; err != nil {
				return err
			}
			res.XConnectsDeleted++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = MarkStaleByDevice(db, deviceID)
	_ = RebuildStale(db)
	return res, nil
}

func upsertInventoryPort(tx *gorm.DB, interfaceID uint, p InventoryPort) error {
	var port models.OpticalPort
	err := tx.Where("interface_id = ?", interfaceID).First(&port).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		port = models.OpticalPort{InterfaceID: interfaceID, Role: p.Role, FreqHz: p.FreqHz}
		return tx.Create(&port).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{"role": p.Role}
	if p.FreqHz != 0 {
		updates["freq_hz"] = p.FreqHz
	}
	return tx.Model(&port).Updates(updates).Error
}

func applyInventoryXConnect(tx *gorm.DB, deviceID uint, source string, idx ifaceIndex, xc InventoryXConnect) (uint, string) {
	switch xc.Kind {
	case models.XCAddDrop, models.XCExpress, models.XCTributary:
	default:
		return 0, "kind " + xc.Kind + " is not applied from inventory"
	}
	a, okA := idx.match(xc.PortA)
	b, okB := idx.match(xc.PortB)
	if !okA || !okB {
		return 0, "port not in factum"
	}
	if a.ID == b.ID {
		return 0, "same interface both ends"
	}

	var ports []models.OpticalPort
	if err := tx.Where("interface_id IN ?", []uint{a.ID, b.ID}).Find(&ports).Error; err != nil {
		return 0, err.Error()
	}
	byIface := map[uint]models.OpticalPort{}
	for _, p := range ports {
		byIface[p.InterfaceID] = p
	}
	pa, pb := byIface[a.ID], byIface[b.ID]
	oa, ob, err := orderInventoryXConnect(xc.Kind, a, b, pa, pb)
	if err != nil {
		return 0, err.Error()
	}
	pa, pb = byIface[oa.ID], byIface[ob.ID]
	freq := xc.FreqHz
	if freq == 0 {
		if pa.FreqHz != 0 {
			freq = pa.FreqHz
		} else {
			freq = pb.FreqHz
		}
	}
	if xc.Kind == models.XCAddDrop && pa.FreqHz == 0 && freq != 0 {
		if err := tx.Model(&models.OpticalPort{}).Where("interface_id = ?", oa.ID).Update("freq_hz", freq).Error; err != nil {
			return 0, err.Error()
		}
		pa.FreqHz = freq
	}

	row := models.OpticalXConnect{
		DeviceID:     deviceID,
		Kind:         xc.Kind,
		InterfaceAID: oa.ID,
		InterfaceBID: ob.ID,
		FreqHz:       freq,
		Source:       source,
	}
	var existing models.OpticalXConnect
	find := tx.Where("device_id = ? AND kind = ? AND freq_hz = ? AND ((interface_a_id = ? AND interface_b_id = ?) OR (interface_a_id = ? AND interface_b_id = ?))",
		deviceID, xc.Kind, freq, oa.ID, ob.ID, ob.ID, oa.ID).First(&existing)
	if find.Error == nil {
		row.ID = existing.ID
	} else if !errors.Is(find.Error, gorm.ErrRecordNotFound) {
		return 0, find.Error.Error()
	}
	if err := ValidateXConnect(tx, &row); err != nil {
		return 0, err.Error()
	}
	if row.ID != 0 {
		if existing.Source != "" && existing.Source != source {
			if err := tx.Model(&existing).Update("source", source).Error; err != nil {
				return 0, err.Error()
			}
		}
		if existing.InterfaceAID != oa.ID || existing.InterfaceBID != ob.ID {
			if err := tx.Model(&existing).Updates(map[string]any{
				"interface_a_id": oa.ID, "interface_b_id": ob.ID,
			}).Error; err != nil {
				return 0, err.Error()
			}
		}
		return existing.ID, ""
	}
	if err := tx.Create(&row).Error; err != nil {
		return 0, err.Error()
	}
	return row.ID, ""
}

func orderInventoryXConnect(kind string, a, b models.Interface, pa, pb models.OpticalPort) (models.Interface, models.Interface, error) {
	switch kind {
	case models.XCTributary:
		if pa.Role == models.PortTXPClient && pb.Role == models.PortTXPLine {
			return a, b, nil
		}
		if pb.Role == models.PortTXPClient && pa.Role == models.PortTXPLine {
			return b, a, nil
		}
		return a, b, fmt.Errorf("tributary requires txp_client and txp_line")
	case models.XCAddDrop:
		if pa.Role == models.PortROADMAddDrop && pb.Role == models.PortROADMDegree {
			return a, b, nil
		}
		if pb.Role == models.PortROADMAddDrop && pa.Role == models.PortROADMDegree {
			return b, a, nil
		}
		return a, b, fmt.Errorf("add/drop requires roadm_adddrop and roadm_degree")
	default:
		return a, b, nil
	}
}
