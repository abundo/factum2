package drivers

import (
	"strings"
	"testing"
)

// srosTestConfigure builds a fixture matching the "nokia-conf:configure"
// subtree of MD-CLI's "show configuration json" output, deep enough to
// exercise port/interface/eline/elan/l3vpn parsing without a live device.
func srosTestConfigure() map[string]any {
	return map[string]any{
		"port": []any{
			map[string]any{"port-id": "1/1/1", "description": "NNI-LINK"},
		},
		"router": []any{
			map[string]any{
				"interface": []any{
					map[string]any{
						"interface-name": "Loopback500",
						"ipv4": map[string]any{
							"primary": map[string]any{"address": "195.196.192.45", "prefix-length": float64(32)},
						},
					},
				},
			},
		},
		"service": map[string]any{
			"sdp": []any{
				map[string]any{"sdp-id": float64(28), "far-end": map[string]any{"ip-address": "172.27.250.28"}},
			},
			"epipe": []any{
				map[string]any{
					"service-name": "CN00570-4 GC-KNET",
					"description":  "CN00570-4 GC-KNET",
					"sap":          []any{map[string]any{"sap-id": "1/1/1:3704.*"}},
					"spoke-sdp":    []any{map[string]any{"sdp-bind-id": "28:1005704"}},
				},
			},
			"vpls": []any{
				map[string]any{
					"service-name": "CN00380 Kommuntelefoni",
					"bgp": []any{
						map[string]any{
							"route-distinguisher": "1234:13211",
							"route-target":        map[string]any{"import": "target:1234:13211", "export": "target:1234:13211"},
						},
					},
					"sap": []any{map[string]any{"sap-id": "1/1/5:*"}},
				},
			},
			"vprn": []any{
				map[string]any{
					"service-name": "POLARIX",
					"bgp-ipvpn": map[string]any{
						"mpls": map[string]any{
							"route-distinguisher": "1234:500",
							"vrf-target":          map[string]any{"community": "target:1234:500"},
						},
					},
					"interface": []any{
						map[string]any{
							"interface-name": "Loopback500",
							"ipv4":           map[string]any{"primary": map[string]any{"address": "195.196.192.45", "prefix-length": float64(32)}},
						},
					},
				},
			},
		},
	}
}

func srosTestDeviceConfig() *DeviceConfig {
	configure := srosTestConfigure()
	dc := NewDeviceConfig()
	srosParsePorts(dc, configure)
	srosParseRouterInterfaces(dc, configure)
	srosParseEline(dc, configure)
	srosParseElan(dc, configure)
	srosParseL3VPN(dc, configure)
	return dc
}

func TestSROSParsePortsAndRouterInterfaces(t *testing.T) {
	dc := srosTestDeviceConfig()

	port, ok := dc.InterfacesByName["1/1/1"]
	if !ok {
		t.Fatalf("port 1/1/1 not parsed; have %v", dc.InterfacesByName)
	}
	if port.Description != "NNI-LINK" || port.Type != "other" {
		t.Errorf("port = %+v, want Type=other (physical, Netbox rejects cabling a virtual interface)", port)
	}

	// Loopback500 is parsed twice - once as a router interface, once (with
	// the same name) via the vprn's interface list - AddInterface keeps
	// only the later one in InterfacesByName (a plain map assignment).
	lo, ok := dc.InterfacesByName["Loopback500"]
	if !ok {
		t.Fatalf("Loopback500 not parsed")
	}
	if lo.Type != "virtual" {
		t.Errorf("Loopback500.Type = %q, want virtual", lo.Type)
	}
	if len(lo.IPAddresses) != 1 || lo.IPAddresses[0].Address.String() != "195.196.192.45/32" {
		t.Errorf("Loopback500 addresses = %+v", lo.IPAddresses)
	}
}

func TestSROSParseEline(t *testing.T) {
	dc := srosTestDeviceConfig()
	eline, ok := dc.ELINEs["CN00570-4 GC-KNET"]
	if !ok {
		t.Fatalf("ELINE not parsed; have %v", dc.ELINEs)
	}
	sapID, ok := eline.Conn1.(string)
	if !ok || sapID != "1/1/1:3704.*" {
		t.Errorf("Conn1 = %+v, want sap-id string", eline.Conn1)
	}
	pw, ok := eline.Conn2.(*Pseudowire)
	if !ok {
		t.Fatalf("Conn2 = %+v, want *Pseudowire", eline.Conn2)
	}
	if pw.Neighbor != "172.27.250.28" || pw.PWID != 1005704 {
		t.Errorf("pw = %+v", pw)
	}

	// The SAP must also be registered as its own Interface, named exactly
	// like the device's sap-id - otherwise device-sync's interfacesDelete
	// treats the Netbox interface admins create as a SAP substitute (SR OS
	// has no real subinterfaces) as gone and deletes it.
	sapIface, ok := dc.InterfacesByName["1/1/1:3704.*"]
	if !ok {
		t.Fatalf("sap 1/1/1:3704.* not registered as an Interface; have %v", dc.InterfacesByName)
	}
	if sapIface.Type != "virtual" {
		t.Errorf("sap interface Type = %q, want virtual", sapIface.Type)
	}
}

func TestSROSParseElan(t *testing.T) {
	dc := srosTestDeviceConfig()
	elan, ok := dc.ELANs["CN00380 Kommuntelefoni"]
	if !ok {
		t.Fatalf("ELAN not parsed; have %v", dc.ELANs)
	}
	if elan.RD != "1234:13211" || elan.RTImport != "target:1234:13211" || elan.RTExport != "target:1234:13211" {
		t.Errorf("elan = %+v", elan)
	}
	if len(elan.Interfaces) != 1 || elan.Interfaces[0] != "1/1/5:*" {
		t.Errorf("elan.Interfaces = %+v", elan.Interfaces)
	}
	if _, ok := dc.InterfacesByName["1/1/5:*"]; !ok {
		t.Fatalf("sap 1/1/5:* not registered as an Interface; have %v", dc.InterfacesByName)
	}
}

func TestSROSParseL3VPN(t *testing.T) {
	dc := srosTestDeviceConfig()
	l3vpn, ok := dc.L3VPNs["POLARIX"]
	if !ok {
		t.Fatalf("L3VPN not parsed; have %v", dc.L3VPNs)
	}
	if l3vpn.VRF == nil || l3vpn.VRF.Name != "POLARIX" || l3vpn.VRF.RD != "1234:500" {
		t.Errorf("l3vpn.VRF = %+v", l3vpn.VRF)
	}
	if l3vpn.VRF.RTImport != "1234:500" || l3vpn.VRF.RTExport != "1234:500" {
		t.Errorf("l3vpn.VRF rt import/export = %q/%q", l3vpn.VRF.RTImport, l3vpn.VRF.RTExport)
	}
	// The vrf built here is only ever attached to its L3VPN, never
	// registered in DeviceConfig.VRFs.
	if len(dc.VRFs) != 0 {
		t.Errorf("DeviceConfig.VRFs = %v, want empty", dc.VRFs)
	}
}

func TestSROSParseNeighbors(t *testing.T) {
	output := strings.Join([]string{
		"Port 1/1/3 Bridge nearest-bridge Remote Peer Information",
		"PortId Subtype        : 5 (interfaceName)",
		"Port Id               : 78:67:65:69:2D:30:2F:31:2F:30:2F:31:34",
		`                        "xgei-0/1/0/14"`,
		"System Name           : 8905-Arvidsjaur",
	}, "\n")

	neighbors := srosParseNeighbors(output)
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1: %+v", len(neighbors), neighbors)
	}
	n := neighbors[0]
	if n.LocalInterface != "1/1/3" {
		t.Errorf("LocalInterface = %q", n.LocalInterface)
	}
	if n.RemoteInterface != "xgei-0/1/0/14" {
		t.Errorf("RemoteInterface = %q, want unquoted value from the line after Port Id", n.RemoteInterface)
	}
	if n.RemoteName != "8905-Arvidsjaur" {
		t.Errorf("RemoteName = %q", n.RemoteName)
	}
}
