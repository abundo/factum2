package optical

import (
	"fmt"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// ValidateXConnect checks one xconnect against the design rules.
func ValidateXConnect(db *gorm.DB, xc *models.OpticalXConnect) error {
	if xc.InterfaceAID == 0 || xc.InterfaceBID == 0 || xc.InterfaceAID == xc.InterfaceBID {
		return fmt.Errorf("xconnect needs two distinct interfaces")
	}
	var a, b models.Interface
	if err := db.First(&a, xc.InterfaceAID).Error; err != nil {
		return fmt.Errorf("interface a: %w", err)
	}
	if err := db.First(&b, xc.InterfaceBID).Error; err != nil {
		return fmt.Errorf("interface b: %w", err)
	}
	if a.DeviceID != b.DeviceID || a.DeviceID != xc.DeviceID {
		return fmt.Errorf("both interfaces must belong to device %d", xc.DeviceID)
	}
	var dev models.Device
	if err := db.First(&dev, xc.DeviceID).Error; err != nil {
		return fmt.Errorf("device: %w", err)
	}

	ports, err := portsByInterface(db, []uint{a.ID, b.ID})
	if err != nil {
		return err
	}
	pa, pb := ports[a.ID], ports[b.ID]

	switch xc.Kind {
	case models.XCTributary:
		if pa.Role != models.PortTXPClient || pb.Role != models.PortTXPLine {
			return fmt.Errorf("tributary requires txp_client → txp_line")
		}
		var n int64
		if err := db.Model(&models.OpticalXConnect{}).
			Where("kind = ? AND interface_a_id = ? AND id <> ?", models.XCTributary, a.ID, xc.ID).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("client already has a tributary xconnect")
		}
	case models.XCAddDrop:
		if pa.Role != models.PortROADMAddDrop || pb.Role != models.PortROADMDegree {
			return fmt.Errorf("add/drop requires roadm_adddrop → roadm_degree")
		}
		if xc.FreqHz == 0 || pa.FreqHz == 0 || xc.FreqHz != pa.FreqHz {
			return fmt.Errorf("add/drop frequency must match the add/drop port")
		}
		if err := degreeFreqFree(db, xc.DeviceID, b.ID, xc.FreqHz, xc.ID); err != nil {
			return err
		}
	case models.XCExpress:
		if pa.Role != models.PortROADMDegree || pb.Role != models.PortROADMDegree {
			return fmt.Errorf("express requires two roadm_degree ports")
		}
		if xc.FreqHz == 0 {
			return fmt.Errorf("express requires a frequency")
		}
		if err := degreeFreqFree(db, xc.DeviceID, a.ID, xc.FreqHz, xc.ID); err != nil {
			return err
		}
		if err := degreeFreqFree(db, xc.DeviceID, b.ID, xc.FreqHz, xc.ID); err != nil {
			return err
		}
	case models.XCPassthrough:
		if dev.OpticalKind != models.OpticalKindILA && dev.OpticalKind != models.OpticalKindPassive {
			return fmt.Errorf("passthrough only on ila/passive devices")
		}
		if pa.Role != "" || pb.Role != "" {
			return fmt.Errorf("passthrough ports must not have an optical role")
		}
		if xc.FreqHz != 0 {
			return fmt.Errorf("passthrough has no frequency")
		}
	default:
		return fmt.Errorf("unknown xconnect kind %q", xc.Kind)
	}

	if err := checkLineAddDropMatch(db, a.ID, b.ID); err != nil {
		return err
	}
	return nil
}

func degreeFreqFree(db *gorm.DB, deviceID, degreeID uint, freq uint64, ignoreID uint) error {
	var n int64
	if err := db.Model(&models.OpticalXConnect{}).
		Where("device_id = ? AND freq_hz = ? AND id <> ? AND (interface_a_id = ? OR interface_b_id = ?)",
			deviceID, freq, ignoreID, degreeID, degreeID).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("frequency already in use on this degree")
	}
	return nil
}

func checkLineAddDropMatch(db *gorm.DB, aID, bID uint) error {
	// When a cable joins a txp_line to a roadm_adddrop, frequencies must match
	// if both are set. Checked here so an xconnect write notices a bad patch.
	var conns []models.Connection
	if err := db.Where(
		"(interface_a_id IN ? OR interface_b_id IN ?)",
		[]uint{aID, bID}, []uint{aID, bID},
	).Find(&conns).Error; err != nil {
		return err
	}
	ports, err := portsByInterface(db, connectionIfaceIDs(conns))
	if err != nil {
		return err
	}
	for _, c := range conns {
		pa, pb := ports[c.InterfaceAID], ports[c.InterfaceBID]
		if (pa.Role == models.PortTXPLine && pb.Role == models.PortROADMAddDrop) ||
			(pb.Role == models.PortTXPLine && pa.Role == models.PortROADMAddDrop) {
			if pa.FreqHz != 0 && pb.FreqHz != 0 && pa.FreqHz != pb.FreqHz {
				return fmt.Errorf("txp line and ROADM add/drop frequencies do not match")
			}
		}
	}
	return nil
}

func portsByInterface(db *gorm.DB, ids []uint) (map[uint]models.OpticalPort, error) {
	out := make(map[uint]models.OpticalPort, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []models.OpticalPort
	if err := db.Where("interface_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.InterfaceID] = r
	}
	return out, nil
}

func connectionIfaceIDs(conns []models.Connection) []uint {
	ids := make([]uint, 0, len(conns)*2)
	for _, c := range conns {
		ids = append(ids, c.InterfaceAID, c.InterfaceBID)
	}
	return ids
}
