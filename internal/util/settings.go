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
