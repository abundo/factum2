package ipam

import (
	"errors"
	"net/netip"
	"testing"
)

func mustP(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", s, err)
	}
	return p
}

func TestParsePrefix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"10.1.2.3/24", "10.1.2.0/24"},
		{"0.0.0.0", "0.0.0.0/0"},
		{"0.0.0.0/0", "0.0.0.0/0"},
		{"::", "::/0"},
		{"::/0", "::/0"},
		{"2001:db8::1/32", "2001:db8::/32"},
	}
	for _, tc := range cases {
		got, err := ParsePrefix(tc.in)
		if err != nil {
			t.Fatalf("ParsePrefix(%q): %v", tc.in, err)
		}
		if got.String() != tc.want {
			t.Errorf("ParsePrefix(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	if _, err := ParsePrefix("not-a-prefix"); err == nil {
		t.Fatal("expected error for garbage prefix")
	}
}

func TestCheckAllocate(t *testing.T) {
	existing := []allocation{
		{Prefix: mustP(t, "10.0.0.0/16"), VRFID: 1},
	}

	if err := checkAllocate(existing, mustP(t, "10.1.0.0/16"), 1); err != nil {
		t.Errorf("same-VRF sibling: %v", err)
	}
	if err := checkAllocate(existing, mustP(t, "10.0.1.0/24"), 1); err != nil {
		t.Errorf("same-VRF child: %v", err)
	}
	if err := checkAllocate(existing, mustP(t, "10.0.1.0/24"), 2); err == nil {
		t.Error("expected child in other VRF to fail")
	} else if !errors.Is(err, errWrongParentVRF) {
		t.Errorf("got %v, want errWrongParentVRF", err)
	}
	if err := checkAllocate(existing, mustP(t, "10.0.0.0/16"), 2); err == nil {
		t.Error("expected exact reuse in other VRF to fail")
	}
	if err := checkAllocate(existing, mustP(t, "10.0.0.0/16"), 1); err == nil {
		t.Error("expected exact duplicate to fail")
	} else if !errors.Is(err, errDuplicate) {
		t.Errorf("got %v, want errDuplicate", err)
	}
	if err := checkAllocate(existing, mustP(t, "192.168.0.0/16"), 1); err != nil {
		t.Errorf("unrelated prefix in same VRF: %v", err)
	}
	if err := checkAllocate(existing, mustP(t, "2001:db8::/32"), 1); err != nil {
		t.Errorf("IPv6: %v", err)
	}
}

func TestCheckAddPool(t *testing.T) {
	existing := []netip.Prefix{mustP(t, "10.0.0.0/8")}
	if err := checkAddPool(existing, mustP(t, "10.0.0.0/8")); err == nil {
		t.Fatal("expected duplicate pool to fail")
	}
	if err := checkAddPool(existing, mustP(t, "10.1.0.0/16")); err != nil {
		t.Errorf("nested pool should be allowed: %v", err)
	}
	if err := checkAddPool(existing, mustP(t, "2001:db8::/32")); err != nil {
		t.Errorf("other family: %v", err)
	}
}

func TestCoverageFrac(t *testing.T) {
	parent := mustP(t, "10.0.0.0/8")
	children := []netip.Prefix{mustP(t, "10.0.0.0/16")}
	got := coverageFrac(parent, children)
	// /16 is 1/256 of a /8
	want := 1.0 / 256
	if got < want-1e-9 || got > want+1e-9 {
		t.Errorf("coverage = %v, want %v", got, want)
	}
}
