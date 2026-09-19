package storage

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// Enabled reports Settings.StorageEnabled (nil/false = off).
// Disabling the flag only hides the Software GUI and /api/software/*
// routes — it does not delete files on the storage host.
func Enabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("storage_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.StorageEnabled != nil && *s.StorageEnabled
}
