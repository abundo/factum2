package netbox

import (
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

// ErrInvalidLocation is a caller-input problem (missing coords, placeholder
// site name, device not in Netbox) rather than a Netbox/DB failure.
var ErrInvalidLocation = errors.New("invalid location")

// AssignLocationInput is the GUI's "put this device here" request.
// SiteName is optional: empty writes GPS onto the device itself (one
// chassis, no site to share). When set, a Netbox site is created/updated
// and the device is assigned to it. Latitude/Longitude are required when
// creating a site, when the named site has no coordinates yet, or when
// pinning the device with no site; omitted they keep an already-plotted
// site's position.
type AssignLocationInput struct {
	SiteName        string
	Latitude        *float64
	Longitude       *float64
	PhysicalAddress string
}

// AssignLocationResult is the local Device (and Site, when a site was
// assigned) after a successful Netbox write and factum mirror.
type AssignLocationResult struct {
	Device models.Device
	Site   *models.Site
}

func invalidLocation(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidLocation, msg)
}

// AssignDeviceLocation writes GPS to Netbox and mirrors it into factum.
// With a site name it creates/updates that site, assigns the device, and
// the device inherits the site's coordinates the same way netbox sync
// does. With no site name it PATCHes latitude/longitude on the device
// itself and leaves its site alone — for a lone chassis a site is
// overhead.
func AssignDeviceLocation(db *gorm.DB, nb *netboxtool.NetboxClient, device models.Device, in AssignLocationInput) (*AssignLocationResult, error) {
	name := strings.TrimSpace(in.SiteName)
	if strings.EqualFold(name, "Default") {
		return nil, invalidLocation("Default is a Netbox placeholder and cannot be used as a site")
	}
	if device.VM {
		return nil, invalidLocation("virtual machines have no coordinates of their own")
	}
	if device.NetboxID == 0 {
		return nil, invalidLocation("device is not synced from netbox")
	}
	if err := validateCoords(in.Latitude, in.Longitude); err != nil {
		return nil, err
	}
	if name == "" {
		return assignDeviceCoordinates(db, nb, device, in)
	}

	siteID, siteName, lat, lng, err := ensureNetboxSite(nb, name, in.Latitude, in.Longitude, strings.TrimSpace(in.PhysicalAddress))
	if err != nil {
		return nil, err
	}

	var previous models.Site
	hasPrevious := db.Where("netbox_id = ?", siteID).First(&previous).Error == nil

	plotted := netboxSiteWithCoords(siteID, siteName, lat, lng)
	if _, _, _, err := ApplySite(db, siteID, plotted); err != nil {
		return nil, err
	}

	if err := patchNetboxDeviceSite(nb, device, siteID); err != nil {
		return nil, err
	}

	updates := map[string]any{
		"site":      siteName,
		"site_id":   siteID,
		"latitude":  lat,
		"longitude": lng,
	}
	if err := db.Model(&models.Device{}).Where("id = ?", device.ID).Updates(updates).Error; err != nil {
		return nil, err
	}

	coordsChanged := !hasPrevious || previous.Latitude != lat || previous.Longitude != lng
	if coordsChanged {
		q := db.Model(&models.Device{}).Where("site_id = ? AND id <> ?", siteID, device.ID)
		if hasPrevious {
			q = q.Where(
				"(latitude IS NULL AND longitude IS NULL) OR (latitude = ? AND longitude = ?)",
				previous.Latitude, previous.Longitude,
			)
		} else {
			q = q.Where("latitude IS NULL AND longitude IS NULL")
		}
		if err := q.Updates(map[string]any{
			"site":      siteName,
			"latitude":  lat,
			"longitude": lng,
		}).Error; err != nil {
			return nil, err
		}
	}

	var outDevice models.Device
	if err := db.First(&outDevice, device.ID).Error; err != nil {
		return nil, err
	}
	var outSite models.Site
	if err := db.Where("netbox_id = ?", siteID).First(&outSite).Error; err != nil {
		return nil, err
	}
	return &AssignLocationResult{Device: outDevice, Site: &outSite}, nil
}

func assignDeviceCoordinates(db *gorm.DB, nb *netboxtool.NetboxClient, device models.Device, in AssignLocationInput) (*AssignLocationResult, error) {
	if in.Latitude == nil || in.Longitude == nil {
		return nil, invalidLocation("latitude and longitude are required")
	}
	lat, lng := *in.Latitude, *in.Longitude
	if err := nb.UpdateDevice(device.NetboxID, map[string]any{
		"latitude":  lat,
		"longitude": lng,
	}); err != nil {
		return nil, err
	}
	if err := db.Model(&models.Device{}).Where("id = ?", device.ID).Updates(map[string]any{
		"latitude":  lat,
		"longitude": lng,
	}).Error; err != nil {
		return nil, err
	}
	var outDevice models.Device
	if err := db.First(&outDevice, device.ID).Error; err != nil {
		return nil, err
	}
	return &AssignLocationResult{Device: outDevice}, nil
}

func validateCoords(lat, lng *float64) error {
	if lat == nil && lng == nil {
		return nil
	}
	if lat == nil || lng == nil {
		return invalidLocation("latitude and longitude must both be set")
	}
	if *lat < -90 || *lat > 90 {
		return invalidLocation("latitude must be between -90 and 90")
	}
	if *lng < -180 || *lng > 180 {
		return invalidLocation("longitude must be between -180 and 180")
	}
	return nil
}

func siteCoordFields(lat, lng float64, address string) map[string]any {
	fields := map[string]any{
		"latitude":  lat,
		"longitude": lng,
	}
	if address != "" {
		fields["physical_address"] = address
	}
	return fields
}

func ensureNetboxSite(nb *netboxtool.NetboxClient, name string, lat, lng *float64, address string) (id uint, siteName string, outLat, outLng float64, err error) {
	existing, err := nb.GetSiteByName(name)
	if err != nil {
		return 0, "", 0, 0, err
	}
	if existing == nil {
		if lat == nil || lng == nil {
			return 0, "", 0, 0, invalidLocation("latitude and longitude are required to create site " + name)
		}
		slug, err := uniqueSiteSlug(nb, name)
		if err != nil {
			return 0, "", 0, 0, err
		}
		created, err := nb.CreateSite(name, slug, siteCoordFields(*lat, *lng, address))
		if err != nil {
			return 0, "", 0, 0, err
		}
		return created.ID, created.Name, *lat, *lng, nil
	}

	plotted, err := nb.GetSite(existing.ID)
	if err != nil {
		return 0, "", 0, 0, err
	}
	if lat != nil && lng != nil {
		if err := nb.UpdateSite(existing.ID, siteCoordFields(*lat, *lng, address)); err != nil {
			return 0, "", 0, 0, err
		}
		return existing.ID, existing.Name, *lat, *lng, nil
	}
	if plotted == nil || plotted.Latitude == nil || plotted.Longitude == nil {
		return 0, "", 0, 0, invalidLocation("latitude and longitude are required to set coordinates on site " + name)
	}
	return existing.ID, existing.Name, float64(*plotted.Latitude), float64(*plotted.Longitude), nil
}

func uniqueSiteSlug(nb *netboxtool.NetboxClient, name string) (string, error) {
	base := siteSlug(name)
	for i := 0; i < 50; i++ {
		slug := base
		if i > 0 {
			suffix := fmt.Sprintf("-%d", i+1)
			stem := base
			if len(stem)+len(suffix) > 100 {
				keep := 100 - len(suffix)
				if keep < 1 {
					keep = 1
				}
				stem = strings.Trim(stem[:keep], "-")
			}
			slug = stem + suffix
		}
		taken, err := nb.GetSiteBySlug(slug)
		if err != nil {
			return "", err
		}
		if taken == nil {
			return slug, nil
		}
	}
	return "", invalidLocation("could not allocate a unique site slug")
}

func siteSlug(s string) string {
	s = slugInvalidChars.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if len(s) > 100 {
		s = strings.Trim(s[:100], "-")
	}
	if s == "" {
		s = "site"
	}
	return s
}

func netboxSiteWithCoords(id uint, name string, lat, lng float64) *netboxtool.NetboxSite {
	la := netboxtool.NBDecimal(lat)
	lo := netboxtool.NBDecimal(lng)
	return &netboxtool.NetboxSite{ID: id, Name: name, Latitude: &la, Longitude: &lo}
}

func patchNetboxDeviceSite(nb *netboxtool.NetboxClient, device models.Device, siteID uint) error {
	changes := map[string]any{"site": siteID}
	if device.VM {
		return nb.UpdateVM(device.NetboxID, changes)
	}
	return nb.UpdateDevice(device.NetboxID, changes)
}
