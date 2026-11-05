package librenms

import "testing"

func TestOpenLocationCode(t *testing.T) {
	// Vectors from the Open Location Code encoding.csv. Length 11 is one
	// grid digit past the published length-10 code.
	cases := []struct {
		lat, lng float64
		length   int
		want     string
	}{
		{20.375, 2.775, 6, "7FG49Q00+"},
		{20.3700625, 2.7821875, 10, "7FG49QCJ+2V"},
		{20.3701125, 2.782234375, 11, "7FG49QCJ+2VX"},
		{47.0000625, 8.0000625, 10, "8FVC2222+22"},
		{-41.2730625, 174.7859375, 10, "4VCPPQGP+Q9"},
		{0.5, -179.5, 4, "62G20000+"},
		{90, 1, 10, "CFX3X2X2+X2"},
		{1, 180, 4, "62H20000+"},
		{1.2, 3.4, 10, "6FH56C22+22"},
		{37.539669125, -122.375069724, 15, "849VGJQF+VX7QR3J"},
		{35.6, 3.033, 10, "8F75J22M+26"},
		{-89.9999375, -179.9999375, 10, "22222222+22"},
		{80.0100000001, 58.57, 15, "CHGW2H6C+2222222"},
		{80.00999996, 58.57, 15, "CHGW2H5C+X2RRRRR"},
	}
	for _, tc := range cases {
		if got := encodeOpenLocationCode(tc.lat, tc.lng, tc.length); got != tc.want {
			t.Errorf("encode(%v, %v, %d) = %q, want %q", tc.lat, tc.lng, tc.length, got, tc.want)
		}
	}
	if got := openLocationCode(47.0000625, 8.0000625); got != "8FVC2222+22G" {
		t.Fatalf("default length: %q", got)
	}
	// shortCodeTests.csv: 9C3W9QCJ+2VX shortened against its own center,
	// then against a point just outside the eight-digit range.
	const full = "9C3W9QCJ+2VX"
	if got, ok := shortenOpenLocationCode(full, 51.3701125, -1.217765625); !ok || got != "+2VX" {
		t.Fatalf("shorten at center: %q ok=%v", got, ok)
	}
	if got, ok := shortenOpenLocationCode(full, 51.3708675, -1.217765625); !ok || got != "CJ+2VX" {
		t.Fatalf("shorten outside eight digits: %q ok=%v", got, ok)
	}
	if got, ok := shortenOpenLocationCode(full, 51.3852125, -1.217765625); !ok || got != "9QCJ+2VX" {
		t.Fatalf("shorten outside six digits: %q ok=%v", got, ok)
	}
	if _, ok := shortenOpenLocationCode(full, 38.8, -9.06); ok {
		t.Fatal("far reference shortened")
	}
	if !isShortOpenLocationCode("+2VX") || !isShortOpenLocationCode("CJ+2VX") || !isShortOpenLocationCode("9QCJ+2VX") {
		t.Fatal("short codes were rejected")
	}
	if isShortOpenLocationCode(full) || isShortOpenLocationCode("+") || isShortOpenLocationCode("Lab") {
		t.Fatal("accepted a non-short code")
	}
	if !isOpenLocationCode("8FVC2222+22") || !isOpenLocationCode("7FG49QCJ+2VX") {
		t.Fatal("full codes were rejected")
	}
	for _, name := range []string{"", "+", "Lab", "+2VX", "CJ+2VX", "7FG40000+", "WXXX2222+22", "location <1.000000,2.000000>"} {
		if isOpenLocationCode(name) {
			t.Fatalf("accepted %q", name)
		}
	}
}
