package optical

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// ImpactRow is one affected service.
type ImpactRow struct {
	ServiceID  uint   `json:"id"`
	ServiceRef string `json:"service_id"`
	Category   string `json:"category"`
	CustomerID uint   `json:"customer_id"`
	Customer   string `json:"customer"`
	Source     string `json:"source"`
}

// DeviceImpact is the payload for GET /api/device/:id/impact.
type DeviceImpact struct {
	DeviceID      uint        `json:"device_id"`
	Status        string      `json:"status"`
	ServiceCount  int         `json:"service_count"`
	CustomerCount int         `json:"customer_count"`
	Services      []ImpactRow `json:"services"`
}

// DeviceDownImpact unions ELINE endpoints and optical device hops.
func DeviceDownImpact(db *gorm.DB, deviceID uint) (DeviceImpact, error) {
	out := DeviceImpact{DeviceID: deviceID, Services: []ImpactRow{}}
	var dev models.Device
	if err := db.First(&dev, deviceID).Error; err != nil {
		return out, err
	}
	out.Status = dev.Status

	seen := map[uint]bool{}
	customers := map[uint]bool{}

	add := func(s models.Service, source string) {
		if seen[s.ID] {
			return
		}
		cat := models.CategoryFromServiceID(s.ServiceID)
		if source == "optical_hop" {
			if cat == "CN" || cat == "CI" {
				return
			}
			if cat == "" {
				// Lime free-text: only if a path exists (already implied by hop)
			} else if !models.OpticalServiceCategories[cat] {
				return
			}
		}
		seen[s.ID] = true
		customers[s.CustomerID] = true
		name := ""
		var c models.Customer
		if s.CustomerID != 0 && db.First(&c, s.CustomerID).Error == nil {
			name = c.Name
		}
		out.Services = append(out.Services, ImpactRow{
			ServiceID:  s.ID,
			ServiceRef: s.ServiceID,
			Category:   cat,
			CustomerID: s.CustomerID,
			Customer:   name,
			Source:     source,
		})
	}

	var elines []models.Service
	if err := db.Where("endpoint_a_device_id = ? OR endpoint_b_device_id = ?", deviceID, deviceID).
		Find(&elines).Error; err != nil {
		return out, err
	}
	for _, s := range elines {
		add(s, "eline")
	}

	var hops []models.ServiceHop
	if err := db.Where("kind = ? AND device_id = ?", models.HopDevice, deviceID).
		Find(&hops).Error; err != nil {
		return out, err
	}
	if len(hops) > 0 {
		ids := make([]uint, 0, len(hops))
		for _, h := range hops {
			ids = append(ids, h.ServiceID)
		}
		var svcs []models.Service
		if err := db.Where("id IN ?", ids).Find(&svcs).Error; err != nil {
			return out, err
		}
		for _, s := range svcs {
			add(s, "optical_hop")
		}
	}

	out.ServiceCount = len(out.Services)
	out.CustomerCount = len(customers)
	return out, nil
}

// ResourceImpact lists VL/VI/LF/LI services whose hops touch the resource.
func ResourceImpact(db *gorm.DB, resourceType string, resourceID uint) ([]ImpactRow, error) {
	q := db.Model(&models.ServiceHop{})
	switch resourceType {
	case models.MaintResourceConnection:
		q = q.Where("connection_id = ?", resourceID)
	case models.MaintResourceDevice:
		q = q.Where("kind = ? AND device_id = ?", models.HopDevice, resourceID)
	case models.MaintResourceInterface:
		q = q.Where("interface_id = ?", resourceID)
	default:
		return nil, nil
	}
	var hops []models.ServiceHop
	if err := q.Find(&hops).Error; err != nil {
		return nil, err
	}
	if len(hops) == 0 {
		return []ImpactRow{}, nil
	}
	ids := make([]uint, 0, len(hops))
	seenHop := map[uint]bool{}
	for _, h := range hops {
		if !seenHop[h.ServiceID] {
			seenHop[h.ServiceID] = true
			ids = append(ids, h.ServiceID)
		}
	}
	var svcs []models.Service
	if err := db.Where("id IN ?", ids).Find(&svcs).Error; err != nil {
		return nil, err
	}
	var out []ImpactRow
	for _, s := range svcs {
		cat := models.CategoryFromServiceID(s.ServiceID)
		if cat == "CN" || cat == "CI" {
			continue
		}
		if cat != "" && !models.OpticalServiceCategories[cat] {
			continue
		}
		if cat == "" {
			var n int64
			db.Model(&models.ServicePath{}).Where("service_id = ?", s.ID).Count(&n)
			if n == 0 {
				continue
			}
		}
		name := ""
		var c models.Customer
		if s.CustomerID != 0 && db.First(&c, s.CustomerID).Error == nil {
			name = c.Name
		}
		out = append(out, ImpactRow{
			ServiceID:  s.ID,
			ServiceRef: s.ServiceID,
			Category:   cat,
			CustomerID: s.CustomerID,
			Customer:   name,
			Source:     "optical_hop",
		})
	}
	return out, nil
}
