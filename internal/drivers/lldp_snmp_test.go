package drivers

import (
	"errors"
	"strings"
	"testing"
)

type fakeLLDP struct {
	get  map[string]lldpBinding
	walk map[string][]lldpBinding
}

func (f *fakeLLDP) Walk(base string) ([]lldpBinding, error) {
	return f.walk[base], nil
}

func (f *fakeLLDP) Get(oid string) (lldpBinding, bool, error) {
	b, ok := f.get[oid]
	return b, ok, nil
}

func (f *fakeLLDP) Close() error { return nil }

func strBind(oid, s string) lldpBinding {
	return lldpBinding{OID: oid, Raw: []byte(s)}
}

func numBind(oid string, n int64) lldpBinding {
	return lldpBinding{OID: oid, IsNum: true, Num: n}
}

func TestAssembleLLDPRemTable(t *testing.T) {
	// Local port 5 is GigabitEthernet0/0/1 (interfaceName). Remote row
	// timeMark 0, port 5, index 1: local port id, MAC chassis, sysName.
	mac := []byte{0xbc, 0xa5, 0x11, 0xaf, 0x29, 0x8c}
	q := &fakeLLDP{
		get: map[string]lldpBinding{
			oidLLDPLocSysName: strBind(oidLLDPLocSysName, "sw1"),
		},
		walk: map[string][]lldpBinding{
			oidLLDPLocPortEntry: {
				numBind(oidLLDPLocPortEntry+".2.5", lldpPortInterfaceName),
				strBind(oidLLDPLocPortEntry+".3.5", "GigabitEthernet0/0/1"),
				numBind(oidLLDPLocPortEntry+".2.6", lldpPortInterfaceName),
				strBind(oidLLDPLocPortEntry+".3.6", "GigabitEthernet0/0/2"),
			},
			oidLLDPRemEntry: {
				numBind(oidLLDPRemEntry+".6.0.5.1", lldpPortLocal),
				strBind(oidLLDPRemEntry+".7.0.5.1", "1/g48"),
				numBind(oidLLDPRemEntry+".4.0.5.1", lldpChassisMAC),
				{OID: oidLLDPRemEntry + ".5.0.5.1", Raw: mac},
				strBind(oidLLDPRemEntry+".9.0.5.1", "accesssw1_bhus"),
				// MAC port id is not an interface name.
				numBind(oidLLDPRemEntry+".6.0.6.1", 3),
				{OID: oidLLDPRemEntry + ".7.0.6.1", Raw: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}},
				strBind(oidLLDPRemEntry+".9.0.6.1", "ignored"),
			},
		},
	}
	neighbors, err := neighborsFromQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1: %+v", len(neighbors), neighbors)
	}
	n := neighbors[0]
	if n.LocalInterface != "GigabitEthernet0/0/1" || n.RemoteInterface != "1/g48" || n.RemoteName != "accesssw1_bhus" {
		t.Errorf("neighbor = %+v", n)
	}
	if n.SystemID != "bc:a5:11:af:29:8c" {
		t.Errorf("SystemID = %q", n.SystemID)
	}
}

func TestAssembleLLDPV2WhenV1Absent(t *testing.T) {
	// Index: timeMark . ifIndex . 6 MAC octets . remIndex.
	const idx = ".0.12.0.1.2.3.4.5.1"
	q := &fakeLLDP{
		get: map[string]lldpBinding{
			oidLLDPV2LocSysName: strBind(oidLLDPV2LocSysName, "sw1"),
		},
		walk: map[string][]lldpBinding{
			oidLLDPV2LocPortEntry: {
				numBind(oidLLDPV2LocPortEntry+".2.12", lldpPortInterfaceName),
				strBind(oidLLDPV2LocPortEntry+".3.12", "Ethernet12"),
			},
			oidLLDPV2RemEntry: {
				numBind(oidLLDPV2RemEntry+".7"+idx, lldpPortInterfaceName),
				strBind(oidLLDPV2RemEntry+".8"+idx, "GigabitEthernet0/4"),
				strBind(oidLLDPV2RemEntry+".10"+idx, "edge.example.net"),
			},
		},
	}
	neighbors, err := neighborsFromQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("got %+v", neighbors)
	}
	n := neighbors[0]
	if n.LocalInterface != "Ethernet12" || n.RemoteInterface != "GigabitEthernet0/4" || n.RemoteName != "edge.example.net" {
		t.Errorf("neighbor = %+v", n)
	}
}

func TestLLDPMIBMissing(t *testing.T) {
	_, err := neighborsFromQuery(&fakeLLDP{get: map[string]lldpBinding{}, walk: map[string][]lldpBinding{}})
	if !errors.Is(err, errLLDPMIBMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestLLDPNoNeighbors(t *testing.T) {
	q := &fakeLLDP{
		get: map[string]lldpBinding{
			oidLLDPLocSysName: strBind(oidLLDPLocSysName, "sw1"),
		},
		walk: map[string][]lldpBinding{},
	}
	neighbors, err := neighborsFromQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 0 {
		t.Fatalf("got %+v", neighbors)
	}
}

func TestLLDPNeighborsRequireCommunity(t *testing.T) {
	_, err := lldpNeighbors("sw1.example", nil)
	if err == nil || !strings.Contains(err.Error(), "community") {
		t.Fatalf("err = %v", err)
	}
	d := &VrpDriver{p: DriverParam{Name: "sw1"}}
	if _, err := d.GetNeighbors(); err == nil {
		t.Fatal("VRP GetNeighbors without a community succeeded")
	}
}
