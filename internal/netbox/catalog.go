package netbox

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/abundo/factum2/models"
	"github.com/abundo/factum2/internal/netboxtool"
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

type deviceTypeTemplateSource interface {
	GetDeviceType(manufacturer, model string) (*netboxtool.NetboxDeviceTypeDetail, error)
}

// syncDeviceTypeTemplates pulls NetBox interface templates for each distinct
// manufacturer+model in nbDevices and upserts them onto the matching
// Factum DeviceType. A missing/unreadable device type is skipped so a
// GraphQL failure cannot abort the rest of the sync.
func syncDeviceTypeTemplates(db *gorm.DB, src deviceTypeTemplateSource, nbDevices []*netboxtool.NBDevice) error {
	if src == nil {
		return nil
	}
	seen := make(map[string][2]string)
	for _, d := range nbDevices {
		if d == nil {
			continue
		}
		mfr := strings.TrimSpace(d.Manufacturer)
		model := strings.TrimSpace(d.ModelName)
		if mfr == "" || model == "" {
			continue
		}
		seen[mfr+"\x00"+model] = [2]string{mfr, model}
	}
	for _, pair := range seen {
		detail, err := src.GetDeviceType(pair[0], pair[1])
		if err != nil || detail == nil {
			slog.Warn("netbox: device type templates", "manufacturer", pair[0], "model", pair[1], "err", err)
			continue
		}
		var mfr models.Manufacturer
		if err := db.Where("name = ?", pair[0]).Take(&mfr).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		var dt models.DeviceType
		if err := db.Where("manufacturer_id = ? AND model = ?", mfr.ID, pair[1]).Take(&dt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		if err := upsertInterfaceTemplates(db, dt.ID, detail.Interfaces); err != nil {
			return err
		}
	}
	return nil
}

func upsertInterfaceTemplates(db *gorm.DB, deviceTypeID uint, nb []netboxtool.NetboxInterfaceTemplate) error {
	if deviceTypeID == 0 {
		return nil
	}
	var existing []models.InterfaceTemplate
	if err := db.Where("device_type_id = ?", deviceTypeID).Find(&existing).Error; err != nil {
		return err
	}
	byNetbox := make(map[uint]models.InterfaceTemplate, len(existing))
	byName := make(map[string]models.InterfaceTemplate, len(existing))
	for _, row := range existing {
		if row.NetboxID != 0 {
			byNetbox[row.NetboxID] = row
		}
		byName[row.Name] = row
	}

	seenNetbox := make(map[uint]bool, len(nb))
	seenName := make(map[string]bool, len(nb))
	for _, tmpl := range nb {
		name := strings.TrimSpace(tmpl.Name)
		if name == "" {
			continue
		}
		row, ok := byNetbox[tmpl.ID]
		if !ok || tmpl.ID == 0 {
			row, ok = byName[name]
		}
		if !ok {
			row = models.InterfaceTemplate{DeviceTypeID: deviceTypeID, Source: "netbox"}
		}
		row.DeviceTypeID = deviceTypeID
		row.Name = name
		row.Type = tmpl.Type
		row.Description = tmpl.Description
		if tmpl.ID != 0 {
			row.NetboxID = tmpl.ID
		}
		row.Source = "netbox"
		if err := db.Save(&row).Error; err != nil {
			return err
		}
		if tmpl.ID != 0 {
			seenNetbox[tmpl.ID] = true
		}
		seenName[name] = true
	}

	for _, row := range existing {
		if row.Source != "netbox" {
			continue
		}
		if seenNetbox[row.NetboxID] || seenName[row.Name] {
			continue
		}
		if err := db.Delete(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
