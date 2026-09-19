package dcim

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestUToTicks(t *testing.T) {
	if got := UToTicks(1); got != 2 {
		t.Fatalf("1U = %d, want 2", got)
	}
	if got := UToTicks(0.5); got != 1 {
		t.Fatalf("0.5U = %d, want 1", got)
	}
	if got := UToTicks(2); got != 4 {
		t.Fatalf("2U = %d, want 4", got)
	}
}

func TestOffsetFromNetboxPosition(t *testing.T) {
	// 42U rack, U1 at bottom, device at U1 height 2U → offset 0
	got := OffsetFromNetboxPosition(42, models.RackNumberingAscending, 1, 1, 2)
	if got != 0 {
		t.Fatalf("ascending bottom = %d, want 0", got)
	}
	got = OffsetFromNetboxPosition(42, models.RackNumberingAscending, 1, 2, 1)
	if got != 2 {
		t.Fatalf("ascending U2 = %d, want 2", got)
	}
	// descending: U1 at top. 2U device at position 1 sits at the top.
	got = OffsetFromNetboxPosition(42, models.RackNumberingDescending, 1, 1, 2)
	if got != 80 {
		t.Fatalf("descending top = %d, want 80", got)
	}
	got = OffsetFromNetboxPosition(42, models.RackNumberingDescending, 1, 41, 2)
	if got != 0 {
		t.Fatalf("descending bottom = %d, want 0", got)
	}
}

func TestDisplayUnit(t *testing.T) {
	if got := DisplayUnit(42, 1, 0, models.RackNumberingAscending); got != 1 {
		t.Fatalf("asc bottom label = %d, want 1", got)
	}
	if got := DisplayUnit(42, 1, 41, models.RackNumberingAscending); got != 42 {
		t.Fatalf("asc top label = %d, want 42", got)
	}
	if got := DisplayUnit(42, 1, 0, models.RackNumberingDescending); got != 42 {
		t.Fatalf("desc bottom label = %d, want 42", got)
	}
	if got := DisplayUnit(42, 1, 41, models.RackNumberingDescending); got != 1 {
		t.Fatalf("desc top label = %d, want 1", got)
	}
}

func TestConflictsWithFaces(t *testing.T) {
	existing := []Occupied{{
		DeviceID: 1, Start: 0, End: 4, Front: true, Rear: true, FullDepth: true,
	}}
	if ConflictsWith(existing, 2, 2, 6, true, false) == nil {
		t.Fatal("full-depth should block the front")
	}
	shallow := []Occupied{{
		DeviceID: 1, Start: 0, End: 4, Front: true,
	}}
	if ConflictsWith(shallow, 2, 0, 4, false, true) != nil {
		t.Fatal("rear should be free when a shallow device is on the front")
	}
	if ConflictsWith(shallow, 2, 0, 4, true, false) == nil {
		t.Fatal("front should overlap")
	}
}

func TestUtilizedTicksFullDepthOnce(t *testing.T) {
	occ := []Occupied{{
		DeviceID: 1, Start: 0, End: 4, Front: true, Rear: true, FullDepth: true,
	}}
	if got := UtilizedTicks(occ, 84); got != 4 {
		t.Fatalf("utilized = %d, want 4", got)
	}
}

func TestOutOfRack(t *testing.T) {
	if !OutOfRack(80, 6, 84) {
		t.Fatal("want out of bounds")
	}
	if OutOfRack(80, 4, 84) {
		t.Fatal("exactly at the top should fit")
	}
}

func TestWholeUBoundary(t *testing.T) {
	if !WholeUBoundary(0, 4) {
		t.Fatal("2U at bottom is a whole-U boundary")
	}
	if WholeUBoundary(1, 2) {
		t.Fatal("half-U offset is not a whole-U boundary")
	}
}
