//go:build integration

package drivers

// Integration tests for the Nokia SR OS driver, run against a real device -
// there's no containerlab image for SR OS (see DEV.md's Testing section), so
// unlike driver_arista_eos_integration_test.go this has no lab-up/lab-down
// tier, only the "point it at a real device" one:
//
//	FACTUM_TEST_SROS_HOST   device name or address (required, else skipped)
//	FACTUM_TEST_SROS_USER   username (default "admin")
//	FACTUM_TEST_SROS_PASS   password (default "admin")
//	FACTUM_TEST_SROS_PORT   NETCONF port (default 830)
//	FACTUM_TEST_SROS_IFACE  port the write test may modify, e.g. "1/1/1"
//	                        (required for TestIntegrationSetInterfaceDescription,
//	                        which is skipped without it - unlike Arista's
//	                        Ethernet1 default, guessing a port-id on someone's
//	                        real device isn't safe)
//
// What this covers that a fake CLI server can't: the NETCONF transport
// (openconfig.go's reads, and - most usefully - the candidate+lock+commit
// write path (netconfEditConfigCandidate) against nokia-conf, which SR OS
// requires and Arista/IOS-XR's direct-running edit doesn't exercise the
// same way. See driver_nokia_sros.go's docstring for why writes go through
// the native model instead of OpenConfig.
//
// envOr is shared with driver_arista_eos_integration_test.go (same package,
// same build tag).

import (
	"os"
	"testing"

	"github.com/abundo/netboxtool"
)

func integrationNokiaDriver(t *testing.T) *NokiaDriver {
	t.Helper()
	host := os.Getenv("FACTUM_TEST_SROS_HOST")
	if host == "" {
		t.Skip("FACTUM_TEST_SROS_HOST not set, skipping - see the comment at the top of this file")
	}
	driver, err := NewNokiaDriver(DriverParam{
		Name:     host,
		Port:     envOr("FACTUM_TEST_SROS_PORT", netconfDefaultPort),
		Username: envOr("FACTUM_TEST_SROS_USER", "admin"),
		Password: envOr("FACTUM_TEST_SROS_PASS", "admin"),
		Platform: "sros",
	})
	if err != nil {
		t.Fatalf("NewNokiaDriver: %v", err)
	}
	return driver
}

func TestIntegrationNokiaVersion(t *testing.T) {
	got, err := integrationNokiaDriver(t).Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got.Version == "" || got.ModelName == "" {
		t.Errorf("Version() = %+v, want at least version and model filled in", got)
	}
	t.Logf("device: %s running %s", got.ModelName, got.Version)
}

func TestIntegrationNokiaRunningConfigGet(t *testing.T) {
	driver := integrationNokiaDriver(t)

	got, err := driver.RunningConfigGet(false)
	if err != nil {
		t.Fatalf("RunningConfigGet: %v", err)
	}
	if len(got.ConfigStr) < 100 {
		t.Errorf("running config is only %d bytes, want a real config", len(got.ConfigStr))
	}
}

func TestIntegrationNokiaExec(t *testing.T) {
	got, err := integrationNokiaDriver(t).Exec("show system information")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got.Result == "" {
		t.Error("Exec returned no output")
	}
}

// RunningConfigSave is deliberately unsupported for SR OS - see the comment
// on driver_nokia_sros.go's RunningConfigSave - so the integration-worthy
// assertion is that it fails rather than silently no-opping.
func TestIntegrationNokiaRunningConfigSave(t *testing.T) {
	err := integrationNokiaDriver(t).RunningConfigSave()
	if err == nil {
		t.Error("RunningConfigSave() = nil error, want unsupported error")
	}
}

func TestIntegrationNokiaGetInterfacesStatus(t *testing.T) {
	got, err := integrationNokiaDriver(t).GetInterfacesStatus()
	if err != nil {
		t.Fatalf("GetInterfacesStatus: %v", err)
	}
	if len(got) == 0 {
		t.Error("GetInterfacesStatus returned no interfaces")
	}
	for _, intf := range got {
		t.Logf("%s: admin=%s oper=%s descr=%q", intf.Name, intf.InterfaceStatus, intf.LineProtocolStatus, intf.Description)
	}
}

// Writes to the device: sets a description on FACTUM_TEST_SROS_IFACE over
// NETCONF (candidate + commit), reads it back, then restores whatever was
// there before. SetInterfaceDescription(s) only accepts port-ids, not
// subinterfaces (driver_nokia_sros.go's docstring), so this needs a real
// port on the target device - there's no safe default to guess, unlike
// Arista's Ethernet1, so the test skips itself without one.
func TestIntegrationNokiaSetInterfaceDescription(t *testing.T) {
	driver := integrationNokiaDriver(t)
	portID := os.Getenv("FACTUM_TEST_SROS_IFACE")
	if portID == "" {
		t.Skip("FACTUM_TEST_SROS_IFACE not set, skipping - see the comment at the top of this file")
	}

	before, err := nokiaDescriptionOf(driver, portID)
	if err != nil {
		t.Fatalf("reading %s: %v", portID, err)
	}
	t.Cleanup(func() {
		if err := driver.SetInterfaceDescription(&netboxtool.NBInterface{Name: portID, Description: before}); err != nil {
			t.Errorf("restoring %s description to %q: %v", portID, before, err)
		}
	})

	const want = "factum2 integration test"
	if err := driver.SetInterfaceDescription(&netboxtool.NBInterface{Name: portID, Description: want}); err != nil {
		t.Fatalf("SetInterfaceDescription: %v", err)
	}
	got, err := nokiaDescriptionOf(driver, portID)
	if err != nil {
		t.Fatalf("reading %s back: %v", portID, err)
	}
	if got != want {
		t.Errorf("%s description = %q, want %q", portID, got, want)
	}
}

func nokiaDescriptionOf(driver *NokiaDriver, portID string) (string, error) {
	interfaces, err := driver.GetInterfacesStatus()
	if err != nil {
		return "", err
	}
	for _, intf := range interfaces {
		if intf.Name == portID {
			return intf.Description, nil
		}
	}
	return "", nil
}
