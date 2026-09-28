package radius

import (
	"errors"
	"net"
	"testing"
)

func testIndex() *Index {
	return BuildIndex(Config{
		Enabled: true,
		Listen:  ":1812",
		Clients: []Client{{Name: "wdm1", Address: "10.0.0.5", Secret: "testing123"}},
		Policies: []Policy{
			{GroupDN: "CN=wdm-ops,DC=example,DC=com", Roles: []string{"WDM"}},
			{GroupDN: "CN=net-admins,DC=example,DC=com", AllDevices: true},
		},
		Devices: []Device{
			{Name: "wdm1", Role: "WDM", Enabled: true, Addresses: []string{"10.0.0.5", "10.0.0.5/32"}},
			{Name: "core", Role: "Router", Enabled: true, Addresses: []string{"10.0.0.6"}},
			{Name: "old", Role: "WDM", Enabled: false, Addresses: []string{"10.0.0.7"}},
		},
	})
}

func TestAuthorizeRoleAndAdmin(t *testing.T) {
	idx := testIndex()
	wdm := []string{"CN=WDM-OPS,DC=example,DC=com"}
	out := authorize("ada", "10.0.0.5", "10.0.0.5", idx, wdm, true, nil)
	if out.code != codeAccessAccept || out.deviceName != "wdm1" {
		t.Fatalf("wdm accept: %+v", out)
	}
	out = authorize("ada", "10.0.0.6", "10.0.0.6", idx, wdm, true, nil)
	if out.reason != reasonNotAllowed {
		t.Fatalf("router: %+v", out)
	}
	admin := []string{"cn=net-admins,dc=example,dc=com"}
	out = authorize("admin", "10.0.0.6", "10.0.0.6", idx, admin, true, nil)
	if out.code != codeAccessAccept || out.deviceRole != "Router" {
		t.Fatalf("admin: %+v", out)
	}
	out = authorize("ada", "10.0.0.5", "10.0.0.5", idx, wdm, false, nil)
	if out.reason != reasonBadUser {
		t.Fatalf("password: %+v", out)
	}
	out = authorize("ada", "10.0.0.5", "10.0.0.5", idx, wdm, false, errors.New("down"))
	if out.reason != reasonDirectory {
		t.Fatalf("directory: %+v", out)
	}
	out = authorize("ada", "10.0.0.7", "10.0.0.7", idx, admin, true, nil)
	if out.reason != reasonDeviceDisabled {
		t.Fatalf("disabled: %+v", out)
	}
	out = authorize("ada", "10.9.9.9", "10.9.9.9", idx, admin, true, nil)
	if out.reason != reasonUnknownDevice {
		t.Fatalf("unknown: %+v", out)
	}
	out = authorize("ada", "10.0.0.6", "10.0.0.5", idx, admin, true, nil)
	if out.reason != reasonNASMismatch {
		t.Fatalf("mismatch: %+v", out)
	}
	out = authorize("ada", "", "10.0.0.5", idx, wdm, true, nil)
	if out.code != codeAccessAccept {
		t.Fatalf("source only: %+v", out)
	}
}

func TestGateRejectsMissingAuthenticator(t *testing.T) {
	idx := testIndex()
	raw := buildRequest(t, 1, "testing123", "ada", "secret", net.IPv4(10, 0, 0, 5))
	// Strip Message-Authenticator (last 18 bytes) and fix the length.
	raw = raw[:len(raw)-18]
	raw[2] = 0
	raw[3] = byte(len(raw))
	_, out, ok := gate(raw, "10.0.0.5", idx)
	if ok || out.reason != reasonBadAuth || out.code != 0 {
		t.Fatalf("ok=%v out=%+v", ok, out)
	}
	if out.username != "" {
		t.Fatal("username recorded from an unverified packet")
	}
}

func TestAmbiguousAddressOmitted(t *testing.T) {
	idx := BuildIndex(Config{
		Clients: []Client{
			{Address: "10.0.0.1", Secret: "a"},
			{Address: "10.0.0.1", Secret: "b"},
		},
		Devices: []Device{
			{Name: "a", Role: "WDM", Enabled: true, Addresses: []string{"10.1.1.1"}},
			{Name: "b", Role: "Router", Enabled: true, Addresses: []string{"10.1.1.1"}},
		},
	})
	if _, ok := idx.clients["10.0.0.1"]; ok {
		t.Fatal("duplicate client kept")
	}
	if _, ok := idx.devices["10.1.1.1"]; ok {
		t.Fatal("duplicate device address kept")
	}
}
