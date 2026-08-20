//go:build integration

package drivers

// Cross-vendor ELINE integration test: pushes a real point-to-point
// service between a live Arista EOS device and a live Nokia SR OS device,
// verifies both sides parse back correctly via GetDeviceConfig, then tears
// it down - the one thing neither driver_arista_eos_eline_test.go's fake
// eAPI server nor driver_nokia_sros_eline_test.go's network-free command
// tests can exercise: that the two vendors' rendered config actually
// interop over a real MPLS LDP pseudowire / spoke-sdp between real boxes.
//
//	FACTUM_TEST_EOS_HOST/_USER/_PASS/_IFACE    the EOS endpoint (see
//	                                            driver_arista_eos_integration_test.go)
//	FACTUM_TEST_SROS_HOST/_USER/_PASS/_IFACE   the SR OS endpoint (see
//	                                            driver_nokia_sros_integration_test.go)
//
// Skipped unless both hosts (and both IFACE vars) are set. Always tears
// itself down via t.Cleanup, regardless of whether the apply/verify steps
// passed, using a service name/VLAN/pseudowire-ID that are obviously
// test-only and outside factum's real numbering scheme
// (pseudowireIDFromServiceID, web/handler_service_eline.go) so a stray
// leftover can't be mistaken for real customer config.

import (
	"fmt"
	"os"
	"testing"
)

const (
	testELINEName         = "TEST-FACTUM2-ELINE"
	testELINEVLAN         = 3999
	testELINEPseudowireID = 3999001
)

// testELINELoopback returns label's parsed loopback address off ifaceName,
// failing the test if it can't be found - both ApplyELINE calls below need
// the peer's real loopback as the pseudowire/spoke-sdp neighbor. ifaceName
// is caller-supplied (deviceLoopbackIfaceName's platform-aware pick in
// web/handler_service_eline.go: "Loopback0" for EOS, "system" for SR OS,
// which unlike EOS's operator-chosen convention is a fixed, unrenameable
// SR OS interface name) rather than a single hardcoded value.
func testELINELoopback(t *testing.T, dc *DeviceConfig, label, ifaceName string) string {
	t.Helper()
	lo, ok := dc.InterfacesByName[ifaceName]
	if !ok || len(lo.IPAddresses) == 0 {
		t.Fatalf("%s: no %s address found in parsed device config", label, ifaceName)
	}
	return lo.IPAddresses[0].Address.Addr().String()
}

// TestIntegrationNokiaELINERoundTrip exercises the real Go driver code's
// full commit path on its own - ApplyELINE (a real commit, not a dry-run
// discard), GetDeviceConfig verification, RemoveELINE cleanup - using a
// fake/unused neighbor address so it derives an SDP ID (srosSDPID) that
// can't collide with any real SDP already on the device. Complements
// TestIntegrationELINECrossVendor: that one proves real vendor interop but,
// against lu17-lab-r0/lu17-lab-r4 specifically, deterministically collides
// with a real pre-existing SDP (see its own comment) - this one proves the
// driver's commit/verify/cleanup mechanics work correctly regardless of
// that collision. The pseudowire itself won't come up operationally (no
// real peer answers at the fake neighbor address), which is fine - what
// this checks is that factum's own config lands and reads back correctly.
func TestIntegrationNokiaELINERoundTrip(t *testing.T) {
	host := os.Getenv("FACTUM_TEST_SROS_HOST")
	if host == "" {
		t.Skip("FACTUM_TEST_SROS_HOST not set, skipping")
	}
	iface := os.Getenv("FACTUM_TEST_SROS_IFACE")
	if iface == "" {
		t.Skip("FACTUM_TEST_SROS_IFACE not set, skipping")
	}
	driver := integrationNokiaDriver(t)

	intent := &ELINEIntent{
		Name:             testELINEName,
		Description:      fmt.Sprintf("ID=%s factum2 integration test", testELINEName),
		LocalIface:       iface,
		LocalVLAN:        testELINEVLAN,
		ServiceNumericID: testELINEPseudowireID,
		Remote: &ELINERemotePeer{
			NeighborIP:   "172.27.250.201", // fake/unused - see comment above
			PseudowireID: testELINEPseudowireID,
			DeviceName:   "test-peer",
			RemoteIface:  "Ethernet12",
			RemoteVLAN:   testELINEVLAN,
		},
	}

	t.Cleanup(func() {
		if err := driver.RemoveELINE(&ELINERemoval{Name: testELINEName}); err != nil {
			t.Errorf("cleanup: RemoveELINE: %v", err)
		}
	})

	if err := driver.ApplyELINE(intent); err != nil {
		t.Fatalf("ApplyELINE: %v", err)
	}

	dc, err := driver.GetDeviceConfig()
	if err != nil {
		t.Fatalf("GetDeviceConfig after apply: %v", err)
	}
	eline, ok := dc.ELINEs[testELINEName]
	if !ok {
		t.Fatalf("%s not found after apply; have %v", testELINEName, dc.ELINEs)
	}
	t.Logf("eline: %+v (conn1=%+v conn2=%+v)", eline, eline.Conn1, eline.Conn2)
	pw, ok := eline.Conn2.(*Pseudowire)
	if !ok {
		t.Fatalf("Conn2 = %+v, want *Pseudowire", eline.Conn2)
	}
	if pw.Neighbor != intent.Remote.NeighborIP {
		t.Errorf("pseudowire neighbor = %q, want %q", pw.Neighbor, intent.Remote.NeighborIP)
	}
	if pw.PWID != testELINEPseudowireID {
		t.Errorf("pseudowire PWID = %d, want %d", pw.PWID, testELINEPseudowireID)
	}
}

func TestIntegrationELINECrossVendor(t *testing.T) {
	eosHost := os.Getenv("FACTUM_TEST_EOS_HOST")
	srosHost := os.Getenv("FACTUM_TEST_SROS_HOST")
	if eosHost == "" || srosHost == "" {
		t.Skip("FACTUM_TEST_EOS_HOST and FACTUM_TEST_SROS_HOST must both be set, skipping")
	}
	eosIface := os.Getenv("FACTUM_TEST_EOS_IFACE")
	srosIface := os.Getenv("FACTUM_TEST_SROS_IFACE")
	if eosIface == "" || srosIface == "" {
		t.Skip("FACTUM_TEST_EOS_IFACE and FACTUM_TEST_SROS_IFACE must both be set, skipping")
	}

	eos := integrationDriver(t)
	sros := integrationNokiaDriver(t)

	eosDC, err := eos.GetDeviceConfig()
	if err != nil {
		t.Fatalf("eos GetDeviceConfig: %v", err)
	}
	srosDC, err := sros.GetDeviceConfig()
	if err != nil {
		t.Fatalf("sros GetDeviceConfig: %v", err)
	}
	eosLoopback := testELINELoopback(t, eosDC, "eos", "Loopback0")
	srosLoopback := testELINELoopback(t, srosDC, "sros", "system")
	t.Logf("eos loopback: %s, sros loopback: %s", eosLoopback, srosLoopback)

	description := fmt.Sprintf("ID=%s factum2 integration test", testELINEName)

	t.Cleanup(func() {
		if err := eos.RemoveELINE(&ELINERemoval{Name: testELINEName}); err != nil {
			t.Errorf("cleanup: eos RemoveELINE: %v", err)
		}
		if err := sros.RemoveELINE(&ELINERemoval{Name: testELINEName}); err != nil {
			t.Errorf("cleanup: sros RemoveELINE: %v", err)
		}
	})

	eosIntent := &ELINEIntent{
		Name:             testELINEName,
		Description:      description,
		LocalIface:       eosIface,
		LocalVLAN:        testELINEVLAN,
		ServiceNumericID: testELINEPseudowireID,
		Remote: &ELINERemotePeer{
			NeighborIP:   srosLoopback,
			PseudowireID: testELINEPseudowireID,
			MTU:          9100,
			ControlWord:  true,
			DeviceName:   srosHost,
			RemoteIface:  srosIface,
			RemoteVLAN:   testELINEVLAN,
		},
	}
	srosIntent := &ELINEIntent{
		Name:             testELINEName,
		Description:      description,
		LocalIface:       srosIface,
		LocalVLAN:        testELINEVLAN,
		ServiceNumericID: testELINEPseudowireID,
		Remote: &ELINERemotePeer{
			NeighborIP:   eosLoopback,
			PseudowireID: testELINEPseudowireID,
			MTU:          9100,
			ControlWord:  true,
			DeviceName:   eosHost,
			RemoteIface:  eosIface,
			RemoteVLAN:   testELINEVLAN,
		},
	}

	if err := eos.ApplyELINE(eosIntent); err != nil {
		t.Fatalf("eos ApplyELINE: %v", err)
	}
	if err := sros.ApplyELINE(srosIntent); err != nil {
		t.Fatalf("sros ApplyELINE: %v", err)
	}

	eosDC, err = eos.GetDeviceConfig()
	if err != nil {
		t.Fatalf("eos GetDeviceConfig after apply: %v", err)
	}
	if eline, ok := eosDC.ELINEs[testELINEName]; !ok {
		t.Errorf("eos: %s not found after apply; have %v", testELINEName, eosDC.ELINEs)
	} else {
		t.Logf("eos eline: %+v (conn1=%+v conn2=%+v)", eline, eline.Conn1, eline.Conn2)
	}

	srosDC, err = sros.GetDeviceConfig()
	if err != nil {
		t.Fatalf("sros GetDeviceConfig after apply: %v", err)
	}
	if eline, ok := srosDC.ELINEs[testELINEName]; !ok {
		t.Errorf("sros: %s not found after apply; have %v", testELINEName, srosDC.ELINEs)
	} else {
		t.Logf("sros eline: %+v (conn1=%+v conn2=%+v)", eline, eline.Conn1, eline.Conn2)
	}
}

// TestIntegrationELINEReprovision exercises the shared template "cleanup"
// define on real hardware: apply on one VLAN, re-apply on another with
// StaleSubinterfaces for the old EOS subif, verify the old subif is gone
// and the new ELINE parses back on both platforms, then RemoveELINE
// (cleanup-only render of the same define).
func TestIntegrationELINEReprovision(t *testing.T) {
	eosHost := os.Getenv("FACTUM_TEST_EOS_HOST")
	srosHost := os.Getenv("FACTUM_TEST_SROS_HOST")
	if eosHost == "" || srosHost == "" {
		t.Skip("FACTUM_TEST_EOS_HOST and FACTUM_TEST_SROS_HOST must both be set, skipping")
	}
	eosIface := os.Getenv("FACTUM_TEST_EOS_IFACE")
	srosIface := os.Getenv("FACTUM_TEST_SROS_IFACE")
	if eosIface == "" || srosIface == "" {
		t.Skip("FACTUM_TEST_EOS_IFACE and FACTUM_TEST_SROS_IFACE must both be set, skipping")
	}

	eos := integrationDriver(t)
	sros := integrationNokiaDriver(t)

	eosDC, err := eos.GetDeviceConfig()
	if err != nil {
		t.Fatalf("eos GetDeviceConfig: %v", err)
	}
	srosDC, err := sros.GetDeviceConfig()
	if err != nil {
		t.Fatalf("sros GetDeviceConfig: %v", err)
	}
	eosLoopback := testELINELoopback(t, eosDC, "eos", "Loopback0")
	srosLoopback := testELINELoopback(t, srosDC, "sros", "system")

	const name = "TEST-FACTUM2-ELINE-REPROV"
	const vlanOld, vlanNew = 3998, 3997
	const pwid = 3997001
	description := fmt.Sprintf("ID=%s factum2 reprovision test", name)

	t.Cleanup(func() {
		_ = eos.RemoveELINE(&ELINERemoval{
			Name: name,
			StaleSubinterfaces: []ELINEStaleSubinterface{
				{Iface: eosIface, VLAN: vlanOld},
				{Iface: eosIface, VLAN: vlanNew},
			},
		})
		_ = sros.RemoveELINE(&ELINERemoval{Name: name})
	})

	eosIntent := &ELINEIntent{
		Name: name, Description: description,
		LocalIface: eosIface, LocalVLAN: vlanOld, ServiceNumericID: pwid,
		Remote: &ELINERemotePeer{
			NeighborIP: srosLoopback, PseudowireID: pwid,
			MTU: 9100, ControlWord: true,
			DeviceName: srosHost, RemoteIface: srosIface, RemoteVLAN: vlanOld,
		},
	}
	srosIntent := &ELINEIntent{
		Name: name, Description: description,
		LocalIface: srosIface, LocalVLAN: vlanOld, ServiceNumericID: pwid,
		Remote: &ELINERemotePeer{
			NeighborIP: eosLoopback, PseudowireID: pwid,
			DeviceName: eosHost, RemoteIface: eosIface, RemoteVLAN: vlanOld,
		},
	}

	if err := eos.ApplyELINE(eosIntent); err != nil {
		t.Fatalf("eos ApplyELINE (vlan %d): %v", vlanOld, err)
	}
	if err := sros.ApplyELINE(srosIntent); err != nil {
		t.Fatalf("sros ApplyELINE (vlan %d): %v", vlanOld, err)
	}

	// Re-provision: new VLAN, EOS lists the old subif as stale so the
	// template cleanup define must remove it in the same session.
	eosIntent.LocalVLAN = vlanNew
	eosIntent.Remote.RemoteVLAN = vlanNew
	eosIntent.StaleSubinterfaces = []ELINEStaleSubinterface{{Iface: eosIface, VLAN: vlanOld}}
	srosIntent.LocalVLAN = vlanNew
	srosIntent.Remote.RemoteVLAN = vlanNew

	if err := eos.ApplyELINE(eosIntent); err != nil {
		t.Fatalf("eos ApplyELINE (vlan %d, cleanup): %v", vlanNew, err)
	}
	if err := sros.ApplyELINE(srosIntent); err != nil {
		t.Fatalf("sros ApplyELINE (vlan %d, cleanup): %v", vlanNew, err)
	}

	eosDC, err = eos.GetDeviceConfig()
	if err != nil {
		t.Fatalf("eos GetDeviceConfig after re-apply: %v", err)
	}
	if _, ok := eosDC.ELINEs[name]; !ok {
		t.Errorf("eos: %s missing after re-apply; have %v", name, eosDC.ELINEs)
	}
	oldName := fmt.Sprintf("%s.%d", eosIface, vlanOld)
	newName := fmt.Sprintf("%s.%d", eosIface, vlanNew)
	if _, ok := eosDC.InterfacesByName[oldName]; ok {
		t.Errorf("eos: stale subinterface %s still present after cleanup", oldName)
	}
	if _, ok := eosDC.InterfacesByName[newName]; !ok {
		t.Errorf("eos: new subinterface %s missing after re-apply", newName)
	}

	srosDC, err = sros.GetDeviceConfig()
	if err != nil {
		t.Fatalf("sros GetDeviceConfig after re-apply: %v", err)
	}
	if eline, ok := srosDC.ELINEs[name]; !ok {
		t.Errorf("sros: %s missing after re-apply; have %v", name, srosDC.ELINEs)
	} else {
		t.Logf("sros eline after re-apply: %+v conn1=%+v", eline, eline.Conn1)
	}

	// Full remove via template cleanup-only path
	if err := eos.RemoveELINE(&ELINERemoval{
		Name:               name,
		StaleSubinterfaces: []ELINEStaleSubinterface{{Iface: eosIface, VLAN: vlanNew}},
	}); err != nil {
		t.Fatalf("eos RemoveELINE: %v", err)
	}
	if err := sros.RemoveELINE(&ELINERemoval{Name: name}); err != nil {
		t.Fatalf("sros RemoveELINE: %v", err)
	}

	eosDC, err = eos.GetDeviceConfig()
	if err != nil {
		t.Fatalf("eos GetDeviceConfig after remove: %v", err)
	}
	if _, ok := eosDC.ELINEs[name]; ok {
		t.Errorf("eos: %s still present after RemoveELINE", name)
	}
	if _, ok := eosDC.InterfacesByName[newName]; ok {
		t.Errorf("eos: subinterface %s still present after RemoveELINE", newName)
	}
	srosDC, err = sros.GetDeviceConfig()
	if err != nil {
		t.Fatalf("sros GetDeviceConfig after remove: %v", err)
	}
	if _, ok := srosDC.ELINEs[name]; ok {
		t.Errorf("sros: %s still present after RemoveELINE", name)
	}
}
