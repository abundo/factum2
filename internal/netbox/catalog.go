package netbox

import (
	"errors"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// upsertManufacturer finds or creates the shared catalog row for a NetBox
// manufacturer. Match netbox_id first, then slug, so a Factum-created row
// with the same slug is adopted rather than duplicated.
func upsertManufacturer(db *gorm.DB, name string, netboxID uint) (models.Manufacturer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Manufacturer{}, nil
	}
	slug := models.Slugify(name)
	var row models.Manufacturer
	err := db.Where("netbox_id = ? AND netbox_id != 0", netboxID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && slug != "" {
		err = db.Where("slug = ?", slug).Take(&row).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.Manufacturer{Name: name, Slug: slug, Source: "netbox", NetboxID: netboxID}
		if err := db.Create(&row).Error; err != nil {
			return models.Manufacturer{}, err
		}
		return row, nil
	}
	if err != nil {
		return models.Manufacturer{}, err
	}
	row.Name = name
	row.Slug = slug
	if netboxID != 0 {
		row.NetboxID = netboxID
		row.Source = "netbox"
	}
	if err := db.Save(&row).Error; err != nil {
		return models.Manufacturer{}, err
	}
	return row, nil
}

func upsertDeviceType(db *gorm.DB, manufacturerID uint, model string, netboxID uint) (models.DeviceType, error) {
	model = strings.TrimSpace(model)
	if model == "" || manufacturerID == 0 {
		return models.DeviceType{}, nil
	}
	slug := models.Slugify(model)
	var row models.DeviceType
	err := db.Where("netbox_id = ? AND netbox_id != 0", netboxID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = db.Where("manufacturer_id = ? AND model = ?", manufacturerID, model).Take(&row).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.DeviceType{
			ManufacturerID: manufacturerID,
			Model:          model,
			Slug:           slug,
			Source:         "netbox",
			NetboxID:       netboxID,
		}
		if err := db.Create(&row).Error; err != nil {
			return models.DeviceType{}, err
		}
		return row, nil
	}
	if err != nil {
		return models.DeviceType{}, err
	}
	row.ManufacturerID = manufacturerID
	row.Model = model
	row.Slug = slug
	if netboxID != 0 {
		row.NetboxID = netboxID
		row.Source = "netbox"
	}
	if err := db.Save(&row).Error; err != nil {
		return models.DeviceType{}, err
	}
	return row, nil
}

func upsertPlatform(db *gorm.DB, name string, netboxID uint) (models.Platform, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Platform{}, nil
	}
	slug := models.Slugify(name)
	var row models.Platform
	err := db.Where("netbox_id = ? AND netbox_id != 0", netboxID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && slug != "" {
		err = db.Where("slug = ?", slug).Take(&row).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.Platform{Name: name, Slug: slug, Source: "netbox", NetboxID: netboxID}
		if err := db.Create(&row).Error; err != nil {
			return models.Platform{}, err
		}
		return row, nil
	}
	if err != nil {
		return models.Platform{}, err
	}
	row.Name = name
	row.Slug = slug
	if netboxID != 0 {
		row.NetboxID = netboxID
		row.Source = "netbox"
	}
	if err := db.Save(&row).Error; err != nil {
		return models.Platform{}, err
	}
	return row, nil
}
