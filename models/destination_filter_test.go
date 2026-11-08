package models

import "testing"

func TestRoleAndInterfaceMatchers(t *testing.T) {
	if !RoleMatches("edge", "") || !RoleMatches("edge", " \n") {
		t.Fatal("empty role list should include every role")
	}
	if !RoleMatches("edge-access", "edge") || RoleMatches("core", "edge") {
		t.Fatal("role regex match")
	}
	if RoleMatches("", "edge") || !RoleMatches("", "^$") {
		t.Fatal("empty role matches only a regex that matches the empty string")
	}
	if err := ValidateRegexpList("edge\n["); err == nil {
		t.Fatal("invalid role regex")
	}
	if err := ValidateRegexpList(" \nedge.*\n"); err != nil {
		t.Fatal(err)
	}
	if MatchesWholeRegexp("", ".*") || !MatchesWholeRegexp("Vlan10", "Vlan.*") {
		t.Fatal("whole interface match")
	}
	if MatchesWholeRegexp("xe-0/0/0", "0/0") {
		t.Fatal("interface regex must match the whole name")
	}
}

func TestDeviceFilterExcludes(t *testing.T) {
	device := &Device{Name: "sw1", Manufacturer: "Cisco", ModelName: "C9300", Platform: "ios", Role: "edge"}
	if (DeviceFilter{}).Excludes(device) {
		t.Fatal("empty filter excludes nothing")
	}
	if !(DeviceFilter{Roles: "core"}).Excludes(device) {
		t.Fatal("role allowlist")
	}
	if (DeviceFilter{Roles: "edge|core"}).Excludes(device) {
		t.Fatal("matching role")
	}
	if !(DeviceFilter{Manufacturers: "Cisco"}).Excludes(device) {
		t.Fatal("manufacturer ignore")
	}
	if !(DeviceFilter{}).Excludes(nil) {
		t.Fatal("nil device")
	}
}

func TestDestinationFilters(t *testing.T) {
	s := &Settings{
		CommonIgnoreDevices:       "shared-sw",
		CommonIgnoreManufacturers: "shared-vendor",
		CommonIgnoreModels:        "shared-model",
		CommonIgnorePlatforms:     "shared-platform",
		CommonIgnoreInterfaces:    "Vlan.*",
		CommonRolesEnabled:        "edge",
		DnsRolesEnabled:           "core",
		PrometheusIgnoreDevices:   "own-sw",
		LibrenmsRolesEnabled:      "uplink",
	}
	dns := s.DNSDeviceFilter()
	if dns.Devices != "shared-sw" || dns.Manufacturers != "shared-vendor" || dns.Models != "shared-model" || dns.Platforms != "shared-platform" || dns.Roles != "core" {
		t.Fatalf("dns filter = %+v", dns)
	}
	if s.DNSIgnoreInterfaces() != "Vlan.*" {
		t.Fatalf("dns interfaces = %q", s.DNSIgnoreInterfaces())
	}
	prom := s.PrometheusDeviceFilter()
	if prom.Devices != "own-sw" || prom.Roles != "edge" {
		t.Fatalf("prometheus filter = %+v", prom)
	}
	lnms := s.LibrenmsDeviceFilter()
	if lnms.Devices != "shared-sw" || lnms.Roles != "" {
		t.Fatalf("librenms device filter = %+v", lnms)
	}
	if s.LibrenmsRolesEffective() != "uplink" {
		t.Fatalf("librenms roles = %q", s.LibrenmsRolesEffective())
	}
	s.LibrenmsRolesEnabled = " \n"
	if s.LibrenmsRolesEffective() != "edge" {
		t.Fatalf("librenms roles fallback = %q", s.LibrenmsRolesEffective())
	}
	icinga := s.IcingaDeviceFilter()
	if icinga.Roles != "edge" || icinga.Models != "shared-model" {
		t.Fatalf("icinga filter = %+v", icinga)
	}
	ox := s.OxidizedDeviceFilter()
	if ox.Platforms != "shared-platform" || s.OxidizedIgnoreInterfacesEffective() != "Vlan.*" {
		t.Fatalf("oxidized filter = %+v interfaces %q", ox, s.OxidizedIgnoreInterfacesEffective())
	}
}
