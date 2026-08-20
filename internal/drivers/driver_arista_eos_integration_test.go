//go:build integration

package drivers

// Integration tests for the Arista EOS driver, run against a real device or a
// containerlab cEOS node. Build-tagged so `go test ./...` never picks them up:
//
//	make lab-up                                  # start the cEOS lab
//	make test-integration                        # run these against it
//	make lab-down
//
// Point them at something else with the environment:
//
//	FACTUM_TEST_EOS_HOST   device name or address (required, else skipped)
//	FACTUM_TEST_EOS_USER   username (default "admin")
//	FACTUM_TEST_EOS_PASS   password (default "admin")
//	FACTUM_TEST_EOS_PORT   NETCONF port (default 830)
//	FACTUM_TEST_EOS_IFACE  interface the write tests may modify
//	                       (default "Ethernet1" - see TestIntegrationSetInterfaceDescription)
//
// What these cover that driver_arista_eos_test.go can't: the NETCONF
// transport (openconfig.go), the real EOS reply shapes both transports
// produce, and - most usefully - that the two transports agree, which is the
// property the eAPI fallback rests on and which no fake can verify.

import (
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/abundo/netboxtool"
)

func integrationDriver(t *testing.T) *AristaDriver {
	t.Helper()
	host := os.Getenv("FACTUM_TEST_EOS_HOST")
	if host == "" {
		t.Skip("FACTUM_TEST_EOS_HOST not set, skipping - see the comment at the top of this file")
	}
	driver, err := NewAristaDriver(DriverParam{
		Name:     host,
		Port:     envOr("FACTUM_TEST_EOS_PORT", netconfDefaultPort),
		Username: envOr("FACTUM_TEST_EOS_USER", "admin"),
		Password: envOr("FACTUM_TEST_EOS_PASS", "admin"),
		Platform: "eos",
	})
	if err != nil {
		t.Fatalf("NewAristaDriver: %v", err)
	}
	return driver
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func TestIntegrationVersion(t *testing.T) {
	got, err := integrationDriver(t).Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got.Version == "" || got.ModelName == "" {
		t.Errorf("Version() = %+v, want at least version and model filled in", got)
	}
	t.Logf("device: %s running %s", got.ModelName, got.Version)
}

func TestIntegrationRunningConfigGet(t *testing.T) {
	driver := integrationDriver(t)

	text, err := driver.RunningConfigGet(false)
	if err != nil {
		t.Fatalf("RunningConfigGet(false): %v", err)
	}
	if len(text.ConfigStr) < 100 {
		t.Errorf("running config is only %d bytes, want a real config", len(text.ConfigStr))
	}

	structured, err := driver.RunningConfigGet(true)
	if err != nil {
		t.Fatalf("RunningConfigGet(true): %v", err)
	}
	if len(structured.ConfigStr) < 100 {
		t.Errorf("structured running config is only %d bytes", len(structured.ConfigStr))
	}
}

func TestIntegrationExec(t *testing.T) {
	got, err := integrationDriver(t).Exec("show hostname")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got.Result == "" {
		t.Error("Exec returned no output")
	}
}

// The whole point of the fallback: whichever transport answers, the caller
// gets the same interfaces, with the same descriptions and the same status
// vocabulary. This is the test a fake can't stand in for - it's asserting that
// two independent real implementations agree.
//
// Agreement is compared by name, not by position: the two transports enumerate
// in genuinely different orders (NETCONF walks the config tree, emitting each
// subinterface right after its parent; eAPI's order is whatever key order
// "show interfaces description" happens to encode, which puts Management0
// last). Nothing in factum indexes these lists positionally - every caller
// looks interfaces up by name - so identical ordering was never a property the
// drivers guaranteed.
func TestIntegrationTransportsAgree(t *testing.T) {
	driver := integrationDriver(t)

	viaNetconf, err := driver.GetInterfacesStatus()
	if err != nil {
		t.Fatalf("GetInterfacesStatus (NETCONF): %v - is the NETCONF agent enabled?", err)
	}
	viaEAPI, err := driver.getInterfacesStatusEAPI()
	if err != nil {
		t.Fatalf("getInterfacesStatusEAPI: %v", err)
	}

	netconfByName, eapiByName := byName(viaNetconf), byName(viaEAPI)
	if !slices.Equal(sortedNames(netconfByName), sortedNames(eapiByName)) {
		t.Fatalf("transports report different interfaces:\nnetconf: %v\neapi:    %v",
			names(viaNetconf), names(viaEAPI))
	}
	for _, name := range sortedNames(netconfByName) {
		n, e := netconfByName[name], eapiByName[name]
		if n.Description != e.Description {
			t.Errorf("%s description: NETCONF %q, eAPI %q", name, n.Description, e.Description)
		}
		if n.InterfaceStatus != e.InterfaceStatus {
			t.Errorf("%s admin status: NETCONF %q, eAPI %q", name, n.InterfaceStatus, e.InterfaceStatus)
		}
		if n.LineProtocolStatus != e.LineProtocolStatus {
			t.Errorf("%s oper status: NETCONF %q, eAPI %q", name, n.LineProtocolStatus, e.LineProtocolStatus)
		}
	}
}

func names(interfaces []*netboxtool.NBInterface) []string {
	out := make([]string, len(interfaces))
	for i, intf := range interfaces {
		out[i] = intf.Name
	}
	return out
}

func byName(interfaces []*netboxtool.NBInterface) map[string]*netboxtool.NBInterface {
	out := make(map[string]*netboxtool.NBInterface, len(interfaces))
	for _, intf := range interfaces {
		out[intf.Name] = intf
	}
	return out
}

func sortedNames(byName map[string]*netboxtool.NBInterface) []string {
	return slices.Sorted(maps.Keys(byName))
}

// Writes to the device: sets a description over NETCONF, reads it back, then
// restores whatever was there before. Runs against FACTUM_TEST_EOS_IFACE, so
// point that at an interface you don't mind touching (a lab node's Ethernet1
// by default).
func TestIntegrationSetInterfaceDescription(t *testing.T) {
	driver := integrationDriver(t)
	ifname := envOr("FACTUM_TEST_EOS_IFACE", "Ethernet1")

	before, err := descriptionOf(driver, ifname)
	if err != nil {
		t.Fatalf("reading %s: %v", ifname, err)
	}
	t.Cleanup(func() {
		if err := driver.SetInterfaceDescription(&netboxtool.NBInterface{Name: ifname, Description: before}); err != nil {
			t.Errorf("restoring %s description to %q: %v", ifname, before, err)
		}
	})

	const want = "factum2 integration test"
	if err := driver.SetInterfaceDescription(&netboxtool.NBInterface{Name: ifname, Description: want}); err != nil {
		t.Fatalf("SetInterfaceDescription: %v", err)
	}
	got, err := descriptionOf(driver, ifname)
	if err != nil {
		t.Fatalf("reading %s back: %v", ifname, err)
	}
	if got != want {
		t.Errorf("%s description = %q, want %q", ifname, got, want)
	}

	// An empty description removes the leaf rather than setting it to "".
	if err := driver.SetInterfaceDescription(&netboxtool.NBInterface{Name: ifname, Description: ""}); err != nil {
		t.Fatalf("SetInterfaceDescription(empty): %v", err)
	}
	got, err = descriptionOf(driver, ifname)
	if err != nil {
		t.Fatalf("reading %s back: %v", ifname, err)
	}
	if got != "" {
		t.Errorf("%s description = %q after removal, want empty", ifname, got)
	}
}

func descriptionOf(driver *AristaDriver, ifname string) (string, error) {
	interfaces, err := driver.GetInterfacesStatus()
	if err != nil {
		return "", err
	}
	for _, intf := range interfaces {
		if intf.Name == ifname {
			return intf.Description, nil
		}
	}
	return "", nil
}
