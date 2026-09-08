package ipam

import (
	"strings"
	"testing"
)

func TestPrefixDHCPValidate(t *testing.T) {
	ok := PrefixDHCP{
		Enabled:    true,
		RangeStart: "192.0.2.100",
		RangeEnd:   "192.0.2.200",
		Gateway:    "192.0.2.1",
		DnsServers: "192.0.2.53\n192.0.2.54",
	}
	if err := ok.validate("192.0.2.0/24"); err != nil {
		t.Fatalf("valid dhcp: %v", err)
	}

	cases := []struct {
		name string
		dhcp PrefixDHCP
		pfx  string
		want string
	}{
		{
			name: "range outside",
			dhcp: PrefixDHCP{RangeStart: "192.0.2.1", RangeEnd: "10.0.0.1"},
			pfx:  "192.0.2.0/24",
			want: "outside the prefix",
		},
		{
			name: "range start only",
			dhcp: PrefixDHCP{RangeStart: "192.0.2.10"},
			pfx:  "192.0.2.0/24",
			want: "both a start and an end",
		},
		{
			name: "range reversed",
			dhcp: PrefixDHCP{RangeStart: "192.0.2.200", RangeEnd: "192.0.2.100"},
			pfx:  "192.0.2.0/24",
			want: "start is after end",
		},
		{
			name: "gateway outside",
			dhcp: PrefixDHCP{Gateway: "10.0.0.1"},
			pfx:  "192.0.2.0/24",
			want: "outside the prefix",
		},
		{
			name: "bad dns",
			dhcp: PrefixDHCP{DnsServers: "not-an-ip"},
			pfx:  "192.0.2.0/24",
			want: "not an IP address",
		},
	}
	for _, tc := range cases {
		err := tc.dhcp.validate(tc.pfx)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestDefaultGateway(t *testing.T) {
	p := mustP(t, "192.0.2.0/24")
	if got := DefaultGateway(p).String(); got != "192.0.2.1" {
		t.Errorf("v4 /24 = %s", got)
	}
	p32 := mustP(t, "192.0.2.9/32")
	if got := DefaultGateway(p32).String(); got != "192.0.2.9" {
		t.Errorf("v4 /32 = %s", got)
	}
	p6 := mustP(t, "2001:db8::/64")
	if got := DefaultGateway(p6).String(); got != "2001:db8::1" {
		t.Errorf("v6 /64 = %s", got)
	}
}

func TestSplitLines(t *testing.T) {
	got := SplitLines(" 192.0.2.53 \n\n192.0.2.54\n")
	if len(got) != 2 || got[0] != "192.0.2.53" || got[1] != "192.0.2.54" {
		t.Fatalf("%#v", got)
	}
}
