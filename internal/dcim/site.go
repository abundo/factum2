package dcim

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// LocalSiteIDsForDevice returns local sites.id values that this device
// belongs to. Device.SiteID is a local sites.id for Factum-created devices
// and a NetBox site id for imported devices — the two sequences are not
// interchangeable.
func LocalSiteIDsForDevice(db *gorm.DB, device models.Device) ([]uint, error) {
	ids := make([]uint, 0, 2)
	seen := map[uint]bool{}
	add := func(id uint) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	if device.SiteID != 0 {
		var local models.Site
		if err := db.First(&local, device.SiteID).Error; err == nil {
			add(local.ID)
		}
		if device.NetboxID != 0 {
			var nbSite models.Site
			err := db.Where("netbox_kind = ? AND netbox_id = ?", models.SiteNetboxKindSite, device.SiteID).
				Take(&nbSite).Error
			if err == nil {
				add(nbSite.ID)
			}
		}
	}

	var p models.DevicePlacement
	if err := db.Where("device_id = ?", device.ID).Take(&p).Error; err == nil {
		var rack models.Rack
		if err := db.First(&rack, p.RackID).Error; err == nil {
			add(rack.SiteID)
		}
	}
	return ids, nil
}

func DeviceAtLocalSite(db *gorm.DB, device models.Device, siteID uint) (bool, error) {
	ids, err := LocalSiteIDsForDevice(db, device)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == siteID {
			return true, nil
		}
	}
	return false, nil
}

// DeviceIDsAtLocalSite returns devices associated with a local site:
// local SiteID match, imported NetBox site mapping, or a placement in a
// rack at that site.
func DeviceIDsAtLocalSite(db *gorm.DB, siteID uint) ([]uint, error) {
	seen := map[uint]bool{}
	add := func(id uint) {
		if id != 0 {
			seen[id] = true
		}
	}

	var local []models.Device
	if err := db.Select("id").Where("site_id = ?", siteID).Find(&local).Error; err != nil {
		return nil, err
	}
	for _, d := range local {
		add(d.ID)
	}

	var site models.Site
	if err := db.First(&site, siteID).Error; err == nil && site.NetboxKind == models.SiteNetboxKindSite && site.NetboxID != 0 {
		var imported []models.Device
		if err := db.Select("id").Where("netbox_id != 0 AND site_id = ?", site.NetboxID).Find(&imported).Error; err != nil {
			return nil, err
		}
		for _, d := range imported {
			add(d.ID)
		}
	}

	var racks []models.Rack
	if err := db.Select("id").Where("site_id = ?", siteID).Find(&racks).Error; err != nil {
		return nil, err
	}
	if len(racks) > 0 {
		rackIDs := make([]uint, len(racks))
		for i, r := range racks {
			rackIDs[i] = r.ID
		}
		var placed []models.DevicePlacement
		if err := db.Select("device_id").Where("rack_id IN ?", rackIDs).Find(&placed).Error; err != nil {
			return nil, err
		}
		for _, p := range placed {
			add(p.DeviceID)
		}
	}

	out := make([]uint, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}
