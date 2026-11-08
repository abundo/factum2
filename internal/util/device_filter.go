package util

import "github.com/abundo/factum2/models"

func deviceFilter(devices, manufacturers, modelList, platforms, roles string) models.DeviceFilter {
	return models.DeviceFilter{
		Devices:       devices,
		Manufacturers: manufacturers,
		Models:        modelList,
		Platforms:     platforms,
		Roles:         roles,
	}
}

func (c ConfigDNS) DeviceFilter() models.DeviceFilter {
	return deviceFilter(c.IgnoreDevices, c.IgnoreManufacturers, c.IgnoreModels, c.IgnorePlatforms, c.RolesEnabled)
}

func (c ConfigIcinga) DeviceFilter() models.DeviceFilter {
	return deviceFilter(c.IgnoreDevices, c.IgnoreManufacturers, c.IgnoreModels, c.IgnorePlatforms, c.RolesEnabled)
}

func (c ConfigOxidized) DeviceFilter() models.DeviceFilter {
	return deviceFilter(c.IgnoreDevices, c.IgnoreManufacturers, c.IgnoreModels, c.IgnorePlatforms, c.RolesEnabled)
}

func (c ConfigPrometheus) DeviceFilter() models.DeviceFilter {
	return deviceFilter(c.IgnoreDevices, c.IgnoreManufacturers, c.IgnoreModels, c.IgnorePlatforms, c.RolesEnabled)
}

func (c ConfigLibrenms) DeviceFilter() models.DeviceFilter {
	return deviceFilter(c.IgnoreDevices, c.IgnoreManufacturers, c.IgnoreModels, c.IgnorePlatforms, "")
}
