package drivers

import "testing"

func TestExpandInterfaceNameIOSXE(t *testing.T) {
	cases := map[string]string{
		"Gi9":              "GigabitEthernet9",
		"Te0/0/27":         "TenGigabitEthernet0/0/27",
		"Fa0/1":            "FastEthernet0/1",
		"GigabitEthernet9": "GigabitEthernet9",
		"gi9":              "gi9",
	}
	for in, want := range cases {
		if got := ExpandInterfaceName("ios-xe", in); got != want {
			t.Errorf("ExpandInterfaceName(ios-xe, %q) = %q, want %q", in, got, want)
		}
	}
}
