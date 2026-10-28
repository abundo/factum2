package drivers

import (
	"strings"
	"testing"
)

func TestIbosRegistered(t *testing.T) {
	if _, ok := SupportedPlatforms["ibos"]; !ok {
		t.Fatal("ibos not in SupportedPlatforms")
	}
	d, err := NewDriver(DriverParam{Platform: "ibos", Name: "asr1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.(ExternalInventory); !ok {
		t.Fatal("ibos driver does not mark inventory as external")
	}
	cfg, err := d.GetDeviceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Interfaces) != 0 || len(cfg.InterfacesByName) != 0 {
		t.Fatalf("config = %+v, want empty", cfg)
	}
	if _, err := d.GetNeighbors(); err == nil || !strings.Contains(err.Error(), "community") {
		t.Fatalf("GetNeighbors err = %v, want a missing-community error", err)
	}
	if err := d.RunningConfigSave(); err == nil || !strings.Contains(err.Error(), "another system") {
		t.Fatalf("RunningConfigSave err = %v", err)
	}
}

func TestExpandInterfaceNameIbos(t *testing.T) {
	cases := []struct{ in, want string }{
		{"gi3", "gigabitethernet3"},
		{"Gi3", "gigabitethernet3"},
		{"gigabitethernet 3", "gigabitethernet3"},
		{"gigabitethernet3", "gigabitethernet3"},
		{"te1", "tengigabitethernet1"},
		{"tengigabitethernet2", "tengigabitethernet2"},
		{"fo2", "fortygigabitethernet2"},
		{"hu1", "hundredgigabitethernet1"},
		{"tf1", "twentyfivegigabitethernet1"},
		{"tw1", "twogigabitethernet1"},
		{"fi1", "fivegigabitethernet1"},
		{"fe1", "fastethernet1"},
		{"et1", "ethernet1"},
		{"ethernet1", "ethernet1"},
		{"gi1/0/2", "gigabitethernet1/0/2"},
		{"gi3/vid511", "gi3/vid511"},
		{"ETH1", "ETH1"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ExpandInterfaceName("ibos", tc.in); got != tc.want {
			t.Errorf("ExpandInterfaceName(ibos, %q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := ExpandInterfaceName("IBOS", tc.in); got != tc.want {
			t.Errorf("ExpandInterfaceName(IBOS, %q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
