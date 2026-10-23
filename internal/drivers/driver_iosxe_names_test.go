package drivers

import "testing"

func TestExpandInterfaceNameIOSXE(t *testing.T) {
	cases := map[string]string{
		"Gi9":              "GigabitEthernet9",
		"Gi0/4":            "GigabitEthernet0/4",
		"Te0/0/27":         "TenGigabitEthernet0/0/27",
		"Fa0/1":            "FastEthernet0/1",
		"GigabitEthernet9": "GigabitEthernet9",
		"gi9":              "gi9",
	}
	for _, platform := range []string{"ios-xe", "ios", "IOS"} {
		for in, want := range cases {
			if got := ExpandInterfaceName(platform, in); got != want {
				t.Errorf("ExpandInterfaceName(%s, %q) = %q, want %q", platform, in, got, want)
			}
		}
	}
}
