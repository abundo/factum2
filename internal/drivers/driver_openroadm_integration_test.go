//go:build integration

package drivers

// Integration tests for the Open ROADM driver, run against a real NETCONF
// agent. There is no containerlab image for Open ROADM, so this is the
// "point it at a real device" tier only:
//
//	FACTUM_TEST_OPENROADM_HOST   device name or address (required, else skipped)
//	FACTUM_TEST_OPENROADM_USER   username (default "admin")
//	FACTUM_TEST_OPENROADM_PASS   password (default "admin")
//	FACTUM_TEST_OPENROADM_PORT   NETCONF port (default 830)
//
// Read-only: Version, GetInterfacesStatus, GetDeviceConfig, GetNeighbors,
// GetOpticalInventory. No write path is exercised.

import (
	"os"
	"testing"
)

func integrationOpenROADMDriver(t *testing.T) *OpenROADMDriver {
	t.Helper()
	host := os.Getenv("FACTUM_TEST_OPENROADM_HOST")
	if host == "" {
		t.Skip("FACTUM_TEST_OPENROADM_HOST not set, skipping - see the comment at the top of this file")
	}
	driver, err := NewOpenROADMDriver(DriverParam{
		Name:     host,
		Port:     envOr("FACTUM_TEST_OPENROADM_PORT", netconfDefaultPort),
		Username: envOr("FACTUM_TEST_OPENROADM_USER", "admin"),
		Password: envOr("FACTUM_TEST_OPENROADM_PASS", "admin"),
		Platform: "openroadm",
	})
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

func TestIntegrationOpenROADMRead(t *testing.T) {
	driver := integrationOpenROADMDriver(t)

	ver, err := driver.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if ver.ModelName == "" && ver.SerialNumber == "" && ver.Version == "" {
		t.Fatalf("Version empty: %+v", ver)
	}

	ifaces, err := driver.GetInterfacesStatus()
	if err != nil {
		t.Fatalf("GetInterfacesStatus: %v", err)
	}
	if len(ifaces) == 0 {
		t.Fatal("GetInterfacesStatus returned no ports")
	}

	dc, err := driver.GetDeviceConfig()
	if err != nil {
		t.Fatalf("GetDeviceConfig: %v", err)
	}
	if len(dc.Interfaces) == 0 {
		t.Fatal("GetDeviceConfig returned no interfaces")
	}

	if _, err := driver.GetNeighbors(); err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}

	inv, err := driver.GetOpticalInventory()
	if err != nil {
		t.Fatalf("GetOpticalInventory: %v", err)
	}
	if inv.OpticalKind == "" {
		t.Errorf("GetOpticalInventory: empty OpticalKind (node-type %q)", inv.NodeType)
	}
	if len(inv.Ports) == 0 {
		t.Error("GetOpticalInventory: no optical-role ports")
	}
}
