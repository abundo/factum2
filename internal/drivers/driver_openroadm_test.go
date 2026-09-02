package drivers

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

func loadORFixture(t *testing.T, name string) *orDevice {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "openroadm", name))
	if err != nil {
		t.Fatal(err)
	}
	dev, err := parseORDevice(data)
	if err != nil {
		t.Fatalf("parseORDevice(%s): %v", name, err)
	}
	return dev
}

func TestOpenROADMRegistered(t *testing.T) {
	if _, ok := SupportedPlatforms["openroadm"]; !ok {
		t.Fatal("openroadm not in SupportedPlatforms")
	}
}

func TestNewOpenROADMDriverRequiresCreds(t *testing.T) {
	if _, err := NewOpenROADMDriver(DriverParam{Name: "x"}); err == nil {
		t.Fatal("expected missing-username error")
	}
	d, err := NewOpenROADMDriver(DriverParam{Name: "x", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetInterfaceVLANs(nil, nil); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("SetInterfaceVLANs: %v", err)
	}
	if _, err := d.Exec("show"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("Exec: %v", err)
	}
	if err := d.RunningConfigSave(); err == nil {
		t.Fatal("expected read-only error from RunningConfigSave")
	}
	if err := d.SetInterfaceDescriptions([]string{"a"}, []*netboxtool.NBInterface{{Name: "a"}}); err == nil {
		t.Fatal("expected read-only error from SetInterfaceDescriptions")
	}
}

func TestOpenROADMFilterXML(t *testing.T) {
	got, err := xml.Marshal(orDeviceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, `xmlns="http://org/openroadm/device"`) {
		t.Errorf("filter missing namespace: %s", s)
	}
	if !strings.Contains(s, "org-openroadm-device") {
		t.Errorf("filter missing local name: %s", s)
	}
}

func TestParseORDeviceROADM(t *testing.T) {
	dev := loadORFixture(t, "device-rdm.xml")
	if dev.Info.NodeID != "ROADM-A" || dev.Info.NodeType != "rdm" {
		t.Fatalf("info = %+v", dev.Info)
	}
	if len(dev.CircuitPacks) != 1 || len(dev.CircuitPacks[0].Ports) != 4 {
		t.Fatalf("circuit-packs = %+v", dev.CircuitPacks)
	}
	if len(dev.Interfaces) != 2 || len(dev.Degrees) != 2 || len(dev.SRGs) != 1 {
		t.Fatalf("ifaces=%d degrees=%d srgs=%d", len(dev.Interfaces), len(dev.Degrees), len(dev.SRGs))
	}

	v := versionFromORDevice(dev)
	if v.ModelName != "ROADM-2000" || v.SerialNumber != "SN123" || v.Version != "1.2.3" {
		t.Errorf("version = %+v", v)
	}
	if v.SystemMacAddress != "00:11:22:33:44:55" || v.InternalVersion != "7.1.0" {
		t.Errorf("version extras = %+v", v)
	}

	ifaces := interfacesFromORDevice(dev)
	if len(ifaces) != 4 {
		t.Fatalf("GetInterfacesStatus count = %d, want 4 physical ports", len(ifaces))
	}
	byName := map[string]*netboxtool.NBInterface{}
	for _, i := range ifaces {
		byName[i.Name] = i
	}
	west := byName["1/0/DEG1"]
	if west == nil || west.Description != "West degree" || west.InterfaceStatus != "UP" || west.LineProtocolStatus != "UP" {
		t.Errorf("DEG1 = %+v", west)
	}
	east := byName["1/0/DEG2"]
	if east == nil || east.LineProtocolStatus != "UP" {
		t.Errorf("DEG2 degraded should map to UP, got %+v", east)
	}

	dc := deviceConfigFromORDevice(dev)
	if _, ok := dc.InterfacesByName["1/0/AD1"]; !ok {
		t.Errorf("GetDeviceConfig missing add/drop port; have %v", dc.InterfacesByName)
	}

	nbs := neighborsFromORDevice(dev)
	if len(nbs) != 1 {
		t.Fatalf("neighbors = %+v", nbs)
	}
	if nbs[0].LocalInterface != "1/0/DEG2" || nbs[0].RemoteName != "ROADM-B" || nbs[0].RemoteInterface != "2/0/DEG1" {
		t.Errorf("neighbor = %+v", nbs[0])
	}
}

func TestInventoryORDeviceROADM(t *testing.T) {
	inv := inventoryFromORDevice(loadORFixture(t, "device-rdm.xml"))
	if inv.OpticalKind != models.OpticalKindROADM || inv.NodeType != "rdm" {
		t.Fatalf("kind/type = %s/%s", inv.OpticalKind, inv.NodeType)
	}

	roles := map[string]string{}
	freqs := map[string]uint64{}
	for _, p := range inv.Ports {
		roles[p.Name] = p.Role
		freqs[p.Name] = p.FreqHz
	}
	if roles["1/0/DEG1"] != models.PortROADMDegree || roles["1/0/DEG2"] != models.PortROADMDegree {
		t.Errorf("degree roles = %v", roles)
	}
	if roles["1/0/AD1"] != models.PortROADMAddDrop {
		t.Errorf("add/drop role = %v", roles)
	}
	if _, ok := roles["1/0/FAN"]; ok {
		t.Errorf("internal FAN port should not be in optical inventory: %v", roles)
	}

	wantHz := optical.HzFromTHz(193.1)
	if freqs["1/0/AD1"] != wantHz || freqs["1/0/DEG1"] != wantHz {
		t.Errorf("freqs = %v, want %d on AD1 and DEG1", freqs, wantHz)
	}

	if len(inv.XConnects) != 2 {
		t.Fatalf("xconnects = %+v", inv.XConnects)
	}
	var xc, passthrough OpticalXConnectView
	for _, x := range inv.XConnects {
		switch x.Kind {
		case models.XCAddDrop:
			xc = x
		case models.XCPassthrough:
			passthrough = x
		}
	}
	if xc.Name != "xc-1" || xc.CircuitID != "VL001" || xc.FreqHz != wantHz {
		t.Errorf("add/drop xconnect = %+v", xc)
	}
	if (xc.PortA != "1/0/AD1" || xc.PortB != "1/0/DEG1") && (xc.PortA != "1/0/DEG1" || xc.PortB != "1/0/AD1") {
		t.Errorf("add/drop xconnect ports = %s %s", xc.PortA, xc.PortB)
	}
	if passthrough.Name != "wss-to-amp" {
		t.Errorf("internal-link xconnect = %+v", passthrough)
	}
}

func TestParseORDeviceXPDRWrappedInData(t *testing.T) {
	inv := inventoryFromORDevice(loadORFixture(t, "device-xpdr.xml"))
	if inv.OpticalKind != models.OpticalKindWDMShelf || inv.NodeID != "XPDR-1" {
		t.Fatalf("inventory = %+v", inv)
	}
	roles := map[string]string{}
	for _, p := range inv.Ports {
		roles[p.Name] = p.Role
	}
	if roles["1/1/C1"] != models.PortTXPClient || roles["1/1/L1"] != models.PortTXPLine {
		t.Errorf("xpdr roles = %v", roles)
	}
	ifaces := interfacesFromORDevice(loadORFixture(t, "device-xpdr.xml"))
	var line *netboxtool.NBInterface
	for _, i := range ifaces {
		if i.Name == "1/1/L1" {
			line = i
		}
	}
	if line == nil || line.InterfaceStatus != "DOWN" {
		t.Errorf("line admin outOfService should be DOWN, got %+v", line)
	}
	if len(inv.XConnects) != 1 || inv.XConnects[0].Kind != models.XCTributary {
		t.Errorf("connection-map xconnects = %+v", inv.XConnects)
	}
	if inv.XConnects[0].PortA != "1/1/C1" || inv.XConnects[0].PortB != "1/1/L1" {
		t.Errorf("tributary ports = %+v", inv.XConnects[0])
	}
	if inv.XConnects[0].FreqHz != optical.HzFromTHz(193.1) {
		t.Errorf("line OCH frequency on xconnect = %d", inv.XConnects[0].FreqHz)
	}
}

func TestParseORDeviceILA(t *testing.T) {
	inv := inventoryFromORDevice(loadORFixture(t, "device-ila.xml"))
	if inv.OpticalKind != models.OpticalKindILA {
		t.Fatalf("kind = %s", inv.OpticalKind)
	}
	if len(inv.Ports) != 2 {
		t.Fatalf("ports = %+v", inv.Ports)
	}
	for _, p := range inv.Ports {
		if p.Role != models.PortFiber {
			t.Errorf("ila port %s role = %s, want %s", p.Name, p.Role, models.PortFiber)
		}
	}
}

func TestORLocalNameAndStatus(t *testing.T) {
	if got := orLocalName("openROADM-if:opticalChannel"); got != "opticalChannel" {
		t.Errorf("prefix: %q", got)
	}
	if got := orLocalName("{http://org/openroadm/interfaces}opticalChannel"); got != "opticalChannel" {
		t.Errorf("clark: %q", got)
	}
	if orToOCStatus("inService") != "UP" || orToOCStatus("degraded") != "UP" {
		t.Error("inService/degraded should be UP")
	}
	if orToOCStatus("outOfService") != "DOWN" || orToOCStatus("maintenance") != "DOWN" {
		t.Error("outOfService/maintenance should be DOWN")
	}
	if orToOCStatus("") != "" {
		t.Error("empty state should stay empty")
	}
}

func TestInventorySRGPackWithoutPortList(t *testing.T) {
	const xmlBody = `
<org-openroadm-device xmlns="http://org/openroadm/device">
  <info><node-type>rdm</node-type></info>
  <circuit-packs>
    <circuit-pack-name>2/0</circuit-pack-name>
    <ports>
      <port-name>DEG</port-name>
      <port-qual>roadm-external</port-qual>
    </ports>
    <ports>
      <port-name>AD</port-name>
      <port-qual>roadm-external</port-qual>
    </ports>
  </circuit-packs>
  <degree>
    <degree-number>1</degree-number>
    <connection-ports>
      <circuit-pack-name>2/0</circuit-pack-name>
      <port-name>DEG</port-name>
    </connection-ports>
  </degree>
  <shared-risk-group>
    <srg-number>1</srg-number>
    <circuit-packs>
      <circuit-pack-name>2/0</circuit-pack-name>
    </circuit-packs>
  </shared-risk-group>
  <roadm-connections>
    <connection-name>express-1</connection-name>
    <source><src-if>NMC-A</src-if></source>
    <destination><dst-if>NMC-B</dst-if></destination>
  </roadm-connections>
  <interface>
    <name>NMC-A</name>
    <supporting-circuit-pack-name>2/0</supporting-circuit-pack-name>
    <supporting-port>DEG</supporting-port>
  </interface>
  <interface>
    <name>NMC-B</name>
    <supporting-circuit-pack-name>2/0</supporting-circuit-pack-name>
    <supporting-port>DEG</supporting-port>
  </interface>
</org-openroadm-device>`
	dev, err := parseORDevice([]byte(xmlBody))
	if err != nil {
		t.Fatal(err)
	}
	inv := inventoryFromORDevice(dev)
	roles := map[string]string{}
	for _, p := range inv.Ports {
		roles[p.Name] = p.Role
	}
	if roles["2/0/DEG"] != models.PortROADMDegree {
		t.Errorf("degree: %v", roles)
	}
	if roles["2/0/AD"] != models.PortROADMAddDrop {
		t.Errorf("srg pack fallback add/drop: %v", roles)
	}
	if len(inv.XConnects) != 1 || inv.XConnects[0].Kind != models.XCExpress {
		t.Errorf("degree-to-degree xconnect = %+v", inv.XConnects)
	}
}

func TestOROpticalKind(t *testing.T) {
	cases := map[string]string{
		"rdm":     models.OpticalKindROADM,
		"xpdr":    models.OpticalKindWDMShelf,
		"ila":     models.OpticalKindILA,
		"extplug": models.OpticalKindWDMShelf,
		"other":   "",
	}
	for in, want := range cases {
		if got := orOpticalKind(in); got != want {
			t.Errorf("orOpticalKind(%q) = %q, want %q", in, got, want)
		}
	}
}
