package drivers

import "testing"

func TestExpandInterfaceNameIOSXE(t *testing.T) {
	cases := map[string]string{
		"Gi9":              "GigabitEthernet9",
		"gi5":              "GigabitEthernet5",
		"GI0/4":            "GigabitEthernet0/4",
		"Gi0/4":            "GigabitEthernet0/4",
		"Te0/0/27":         "TenGigabitEthernet0/0/27",
		"te1/0/1":          "TenGigabitEthernet1/0/1",
		"Fa0/1":            "FastEthernet0/1",
		"fa0/1":            "FastEthernet0/1",
		"GigabitEthernet9": "GigabitEthernet9",
		"gigabitethernet9": "gigabitethernet9",
	}
	for _, platform := range []string{"ios-xe", "ios", "IOS"} {
		for in, want := range cases {
			if got := ExpandInterfaceName(platform, in); got != want {
				t.Errorf("ExpandInterfaceName(%s, %q) = %q, want %q", platform, in, got, want)
			}
		}
	}
}
