package models

func (s *Settings) filter(devices, manufacturers, models, platforms, roles string) DeviceFilter {
	if s == nil {
		return DeviceFilter{}
	}
	return DeviceFilter{
		Devices:       IgnoreOrDefault(devices, s.CommonIgnoreDevices),
		Manufacturers: IgnoreOrDefault(manufacturers, s.CommonIgnoreManufacturers),
		Models:        IgnoreOrDefault(models, s.CommonIgnoreModels),
		Platforms:     IgnoreOrDefault(platforms, s.CommonIgnorePlatforms),
		Roles:         IgnoreOrDefault(roles, s.CommonRolesEnabled),
	}
}

func (s *Settings) interfaces(specific string) string {
	if s == nil {
		return ""
	}
	return IgnoreOrDefault(specific, s.CommonIgnoreInterfaces)
}

// DNSDeviceFilter is the DNS sync device filter, with empty destination
// lists filled from Destinations → Common.
func (s *Settings) DNSDeviceFilter() DeviceFilter {
	if s == nil {
		return DeviceFilter{}
	}
	return s.filter(s.DnsIgnoreDevices, s.DnsIgnoreManufacturers, s.DnsIgnoreModels, s.DnsIgnorePlatforms, s.DnsRolesEnabled)
}

func (s *Settings) DNSIgnoreInterfaces() string { return s.interfaces(s.DnsIgnoreInterfaces) }

func (s *Settings) IcingaDeviceFilter() DeviceFilter {
	if s == nil {
		return DeviceFilter{}
	}
	return s.filter(s.IcingaIgnoreDevices, s.IcingaIgnoreManufacturers, s.IcingaIgnoreModels, s.IcingaIgnorePlatforms, s.IcingaRolesEnabled)
}

func (s *Settings) IcingaIgnoreInterfacesEffective() string {
	return s.interfaces(s.IcingaIgnoreInterfaces)
}

func (s *Settings) LibrenmsDeviceFilter() DeviceFilter {
	if s == nil {
		return DeviceFilter{}
	}
	// Roles stay on the interface-alerting list, not this device filter.
	return DeviceFilter{
		Devices:       IgnoreOrDefault(s.LibrenmsIgnoreDevices, s.CommonIgnoreDevices),
		Manufacturers: IgnoreOrDefault(s.LibrenmsIgnoreManufacturers, s.CommonIgnoreManufacturers),
		Models:        IgnoreOrDefault(s.LibrenmsIgnoreModels, s.CommonIgnoreModels),
		Platforms:     IgnoreOrDefault(s.LibrenmsIgnorePlatforms, s.CommonIgnorePlatforms),
	}
}

func (s *Settings) LibrenmsRolesEffective() string {
	if s == nil {
		return ""
	}
	return IgnoreOrDefault(s.LibrenmsRolesEnabled, s.CommonRolesEnabled)
}

func (s *Settings) LibrenmsIgnoreInterfacesEffective() string {
	return s.interfaces(s.LibrenmsIgnoreInterfaces)
}

func (s *Settings) OxidizedDeviceFilter() DeviceFilter {
	if s == nil {
		return DeviceFilter{}
	}
	return s.filter(s.OxidizedIgnoreDevices, s.OxidizedIgnoreManufacturers, s.OxidizedIgnoreModels, s.OxidizedIgnorePlatforms, s.OxidizedRolesEnabled)
}

func (s *Settings) OxidizedIgnoreInterfacesEffective() string {
	return s.interfaces(s.OxidizedIgnoreInterfaces)
}

func (s *Settings) PrometheusDeviceFilter() DeviceFilter {
	if s == nil {
		return DeviceFilter{}
	}
	return s.filter(s.PrometheusIgnoreDevices, s.PrometheusIgnoreManufacturers, s.PrometheusIgnoreModels, s.PrometheusIgnorePlatforms, s.PrometheusRolesEnabled)
}

func (s *Settings) PrometheusIgnoreInterfacesEffective() string {
	return s.interfaces(s.PrometheusIgnoreInterfaces)
}
