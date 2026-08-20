package ipam

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// ParsePrefix accepts a CIDR or a bare address that means "the whole
// family": 0.0.0.0 and 0.0.0.0/0 are IPv4-any; :: and ::/0 are IPv6-any.
// The result is always masked (10.1.2.3/24 → 10.1.2.0/24).
func ParsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "0.0.0.0":
		s = "0.0.0.0/0"
	case "::":
		s = "::/0"
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid prefix %q", s)
	}
	return p.Masked(), nil
}

func familyOf(p netip.Prefix) int {
	if p.Addr().Is4() {
		return 4
	}
	return 6
}

// containsPrefix reports whether parent fully contains child (equal counts
// as contained). Different address families never contain each other.
func containsPrefix(parent, child netip.Prefix) bool {
	if parent.Addr().BitLen() != child.Addr().BitLen() {
		return false
	}
	if parent.Bits() == child.Bits() {
		return parent == child
	}
	return parent.Bits() < child.Bits() && parent.Contains(child.Addr())
}

func strictlyContains(parent, child netip.Prefix) bool {
	return containsPrefix(parent, child) && parent.Bits() < child.Bits()
}

type allocation struct {
	Prefix netip.Prefix
	VRFID  uint
}

var (
	errDuplicate      = errors.New("prefix is already allocated in this namespace")
	errWrongParentVRF = errors.New("prefix overlaps an allocation in another VRF or the namespace root")
	errPoolOverlap    = errors.New("prefix overlaps an existing allowed prefix")
	errPoolDuplicate  = errors.New("this allowed prefix already exists")
)

// checkAllocate enforces namespace-wide uniqueness. Root prefixes (the
// default VRF) and every extra VRF share one address plan: same prefix
// twice is rejected; overlap with a different VRF (or with the root) is
// rejected; same-VRF nesting (subdivision) is allowed.
func checkAllocate(existing []allocation, candidate netip.Prefix, vrfID uint) error {
	for _, got := range existing {
		if got.Prefix.Addr().BitLen() != candidate.Addr().BitLen() {
			continue
		}
		if got.Prefix == candidate {
			return errDuplicate
		}
		if !got.Prefix.Overlaps(candidate) {
			continue
		}
		// Masked CIDRs that overlap are always nested (or equal, already
		// handled). Different VRF may not claim any overlapping space.
		if got.VRFID != vrfID {
			return errWrongParentVRF
		}
	}
	return nil
}

// checkAddPool rejects an exact duplicate or a non-nested overlap with an
// existing pool. Nested pools (10.0.0.0/8 plus 10.1.0.0/16) are allowed.
func checkAddPool(existing []netip.Prefix, candidate netip.Prefix) error {
	for _, got := range existing {
		if got.Addr().BitLen() != candidate.Addr().BitLen() {
			continue
		}
		if got == candidate {
			return errPoolDuplicate
		}
		if !got.Overlaps(candidate) {
			continue
		}
		if containsPrefix(got, candidate) || containsPrefix(candidate, got) {
			continue
		}
		return errPoolOverlap
	}
	return nil
}

// coveredByAny reports whether p sits inside at least one of the prefixes.
func coveredByAny(p netip.Prefix, prefixes []netip.Prefix) bool {
	for _, parent := range prefixes {
		if containsPrefix(parent, p) {
			return true
		}
	}
	return false
}

// poolStillNeeded reports whether any allocation would lose coverage if
// this pool were removed.
func poolStillNeeded(remaining []netip.Prefix, allocations []netip.Prefix) bool {
	for _, a := range allocations {
		if !coveredByAny(a, remaining) {
			return true
		}
	}
	return false
}

// coverageFrac is the fraction of parent covered by immediate children
// (each child counted once; overlapping children are not expected).
func coverageFrac(parent netip.Prefix, children []netip.Prefix) float64 {
	var sum float64
	for _, child := range children {
		if !strictlyContains(parent, child) && child != parent {
			continue
		}
		if child == parent {
			return 1
		}
		delta := child.Bits() - parent.Bits()
		if delta <= 0 {
			continue
		}
		// 1 / 2^delta. For huge IPv6 deltas this underflows to 0, which
		// is the honest answer for a /32 sitting in ::/0.
		part := 1.0
		for i := 0; i < delta && part > 0; i++ {
			part /= 2
		}
		sum += part
	}
	if sum > 1 {
		return 1
	}
	return sum
}

func formatUsed(frac float64, hasChildren bool) string {
	if !hasChildren || frac == 0 {
		if hasChildren {
			return "<0.01%"
		}
		return "—"
	}
	if frac < 0.0001 {
		return "<0.01%"
	}
	return fmt.Sprintf("%.2f%%", frac*100)
}
