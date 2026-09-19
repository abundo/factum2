package dcim

import "testing"

func TestRotatedFootprintAndOverlap(t *testing.T) {
	a := rotatedFootprint(0, 0, 600, 1000, 0)
	if a.MaxX != 600 || a.MaxY != 1000 {
		t.Fatalf("0deg %+v", a)
	}
	b := rotatedFootprint(0, 0, 600, 1000, 90)
	if b.MaxX != 1000 || b.MaxY != 600 {
		t.Fatalf("90deg %+v", b)
	}
	c := rotatedFootprint(600, 0, 600, 1000, 0)
	if footprintsOverlap(a, c) {
		t.Fatal("adjacent racks should not overlap")
	}
	d := rotatedFootprint(100, 0, 600, 1000, 0)
	if !footprintsOverlap(a, d) {
		t.Fatal("offset racks should overlap")
	}
}

func TestSnapMM(t *testing.T) {
	if got := SnapMM(750, 600); got != 600 {
		t.Fatalf("snap = %d", got)
	}
	if got := SnapMM(900, 600); got != 1200 {
		t.Fatalf("snap = %d", got)
	}
}
