package ipam

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// Enabled reports Settings.IpamEnabled (nil/false = off). Disabling the
// flag only hides the UI and 404s /api/ipam/* — it does not touch rows.
func Enabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("ipam_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.IpamEnabled != nil && *s.IpamEnabled
}
