package certs

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func Enabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("certs_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.CertsEnabled != nil && *s.CertsEnabled
}
