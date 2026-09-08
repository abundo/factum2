package dns

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// ZonesEnabled reports Settings.DnsZonesEnabled (nil/false = off).
// Disabling the flag only hides the UI and 404s /api/dns/* zone routes —
// it does not touch rows.
func ZonesEnabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("dns_zones_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.DnsZonesEnabled != nil && *s.DnsZonesEnabled
}

// DhcpEnabled reports Settings.DhcpEnabled (nil/false = off).
func DhcpEnabled(db *gorm.DB) bool {
	var s models.Settings
	if err := db.Select("dhcp_enabled").First(&s, 1).Error; err != nil {
		return false
	}
	return s.DhcpEnabled != nil && *s.DhcpEnabled
}
