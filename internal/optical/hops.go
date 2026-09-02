package optical

import (
	"time"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// RebuildPath traces and replaces hops for one service.
func RebuildPath(db *gorm.DB, g *Graph, serviceID uint) error {
	var path models.ServicePath
	if err := db.Where("service_id = ?", serviceID).First(&path).Error; err != nil {
		return err
	}
	res := Walk(g, path.EndpointAInterfaceID, path.EndpointZInterfaceID, path.Mode)
	now := time.Now()
	updates := map[string]any{
		"status":           res.Status,
		"last_traced_at":   now,
		"last_trace_error": res.Error,
		"freq_hz":          res.FreqHz,
		"start_kind_a":     res.StartKind,
	}
	if res.Status == models.PathConflict {
		return db.Model(&path).Updates(updates).Error
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&path).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("service_id = ?", serviceID).Delete(&models.ServiceHop{}).Error; err != nil {
			return err
		}
		for i, h := range res.Hops {
			row := models.ServiceHop{
				ServiceID:    serviceID,
				Seq:          i + 1,
				Kind:         h.Kind,
				InterfaceID:  h.InterfaceID,
				ConnectionID: h.ConnectionID,
				XConnectID:   h.XConnectID,
				DeviceID:     h.DeviceID,
				FreqHz:       h.FreqHz,
				Label:        h.Label,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// RebuildStale loads the graph once and rebuilds every stale/incomplete path.
func RebuildStale(db *gorm.DB) error {
	g, err := LoadGraph(db)
	if err != nil {
		return err
	}
	var paths []models.ServicePath
	if err := db.Where("status IN ?", []string{models.PathStale, models.PathIncomplete}).
		Find(&paths).Error; err != nil {
		return err
	}
	for _, p := range paths {
		if err := RebuildPath(db, g, p.ServiceID); err != nil {
			return err
		}
	}
	return nil
}

// MarkStaleByInterface marks paths whose hops reference the interface.
func MarkStaleByInterface(db *gorm.DB, interfaceID uint) error {
	return db.Exec(`
		UPDATE service_paths SET status = ?
		WHERE service_id IN (
			SELECT DISTINCT service_id FROM service_hops
			WHERE interface_id = ? OR service_id IN (
				SELECT service_id FROM service_paths
				WHERE endpoint_a_interface_id = ? OR endpoint_z_interface_id = ?
			)
		)`, models.PathStale, interfaceID, interfaceID, interfaceID).Error
}

// MarkStaleByDevice marks paths that hop through the chassis or its ports.
func MarkStaleByDevice(db *gorm.DB, deviceID uint) error {
	return db.Exec(`
		UPDATE service_paths SET status = ?
		WHERE service_id IN (
			SELECT DISTINCT service_id FROM service_hops
			WHERE device_id = ? OR interface_id IN (
				SELECT id FROM interfaces WHERE device_id = ?
			)
		)`, models.PathStale, deviceID, deviceID).Error
}

// MarkStaleByConnection marks paths whose hops reference the connection.
func MarkStaleByConnection(db *gorm.DB, connectionID uint) error {
	return db.Exec(`
		UPDATE service_paths SET status = ?
		WHERE service_id IN (
			SELECT DISTINCT service_id FROM service_hops WHERE connection_id = ?
		)`, models.PathStale, connectionID).Error
}

// DeletePathForService removes path + hops for a service (used on service delete).
func DeletePathForService(tx *gorm.DB, serviceID uint) error {
	if err := tx.Where("service_id = ?", serviceID).Delete(&models.ServiceHop{}).Error; err != nil {
		return err
	}
	return tx.Where("service_id = ?", serviceID).Delete(&models.ServicePath{}).Error
}
