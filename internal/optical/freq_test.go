package optical

import "testing"

func TestNm(t *testing.T) {
	got := Nm(193100000000000)
	if got < 1552.51 || got > 1552.53 {
		t.Errorf("Nm(193.1 THz) = %v, want ~1552.52", got)
	}
	if Nm(0) != 0 {
		t.Errorf("Nm(0) = %v, want 0", Nm(0))
	}
}

func TestHzRoundTrip(t *testing.T) {
	hz := uint64(193100000000000)
	nm := Nm(hz)
	back := HzFromNm(nm)
	// 1 MHz tolerance after float round-trip
	delta := int64(back) - int64(hz)
	if delta < 0 {
		delta = -delta
	}
	if delta > 1e6 {
		t.Errorf("round-trip hz %d -> nm %v -> %d (delta %d)", hz, nm, back, delta)
	}
}
