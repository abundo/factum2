package util

import (
	"errors"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// GetOrCreateSettings returns the single Settings row (id=1), creating it
// with zero-valued fields on first use so callers always have a row to read
// - most settings (integration URLs/tokens like Netbox's, previously only
// configurable via the YAML config file) are now database-backed and
// editable from the admin UI instead, so both the web API and any CLI/sync
// code that needs them goes through here.
func GetOrCreateSettings(db *gorm.DB) (*models.Settings, error) {
	var settings models.Settings
	err := db.First(&settings, 1).Error
	if err == nil {
		return &settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	settings = models.Settings{FactumModel: models.FactumModel{ID: 1}}
	if err := db.Create(&settings).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

// OrganizationEnabled reports Settings.OrganizationEnabled (nil/false = off).
// Disabling the flag only hides the Customers/Contacts menu — it does not
// touch customer or contact rows.
func OrganizationEnabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("organization_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.OrganizationEnabled != nil && *s.OrganizationEnabled
}

// OxidizedEnabled reports Settings.OxidizedEnabled (nil/false = off).
// Disabling the flag only hides the Oxidized menu and GUI browser — it does
// not touch device backup flags or oxidized's own router.db.
func OxidizedEnabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("oxidized_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.OxidizedEnabled != nil && *s.OxidizedEnabled
}
