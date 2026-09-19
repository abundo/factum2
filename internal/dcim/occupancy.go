package dcim

import (
	"math"
	"sort"

	"github.com/abundo/factum2/models"
)

// Occupied is one device's occupied interval on a rack face, in ticks from
// the bottom of the rack. End is exclusive.
type Occupied struct {
	DeviceID  uint
	Start     int
	End       int
	Front     bool
	Rear      bool
	FullDepth bool
}

func UToTicks(u float64) int {
	return int(math.Round(u * float64(models.TicksPerU)))
}

func TicksToU(ticks int) float64 {
	return float64(ticks) / float64(models.TicksPerU)
}

// OffsetFromNetboxPosition converts a NetBox position (lowest-numbered U)
// into ticks from the bottom of the rack.
func OffsetFromNetboxPosition(rackHeightU int, numbering string, startUnit int, positionU, heightU float64) int {
	if startUnit <= 0 {
		startUnit = 1
	}
	rel := positionU - float64(startUnit)
	if numbering == models.RackNumberingDescending {
		return UToTicks(float64(rackHeightU) - rel - heightU)
	}
	return UToTicks(rel)
}

// DisplayUnit returns the labelled U number for a 1U slot whose bottom is
// offsetU whole units from the rack bottom.
func DisplayUnit(rackHeightU, startUnit, offsetU int, numbering string) int {
	if startUnit <= 0 {
		startUnit = 1
	}
	if numbering == models.RackNumberingDescending {
		return startUnit + rackHeightU - 1 - offsetU
	}
	return startUnit + offsetU
}

func OccupiesFace(face string, fullDepth bool, queryFace string) bool {
	if fullDepth {
		return true
	}
	if face == "" {
		face = models.DeviceFaceFront
	}
	if queryFace == "" {
		queryFace = models.DeviceFaceFront
	}
	return face == queryFace
}

func IntervalsOverlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}

func PlacementInterval(offsetTicks, heightTicks int) (start, end int) {
	return offsetTicks, offsetTicks + heightTicks
}

func OutOfRack(offsetTicks, heightTicks, rackTicks int) bool {
	if offsetTicks < 0 || heightTicks < 0 {
		return true
	}
	return offsetTicks+heightTicks > rackTicks
}

// OccupancyFromPlacements builds occupied intervals. heightFor returns the
// device height in ticks (0 unknown/zero-U). fullDepthFor reports whether
// the device blocks both faces.
func OccupancyFromPlacements(placements []models.DevicePlacement, heightFor func(deviceID uint) int, fullDepthFor func(deviceID uint) bool) []Occupied {
	out := make([]Occupied, 0, len(placements))
	for _, p := range placements {
		h := heightFor(p.DeviceID)
		if h <= 0 {
			continue
		}
		full := fullDepthFor(p.DeviceID)
		start, end := PlacementInterval(p.OffsetTicks, h)
		o := Occupied{
			DeviceID:  p.DeviceID,
			Start:     start,
			End:       end,
			FullDepth: full,
		}
		if full || p.Face == models.DeviceFaceFront || p.Face == "" {
			o.Front = true
		}
		if full || p.Face == models.DeviceFaceRear {
			o.Rear = true
		}
		out = append(out, o)
	}
	return out
}

func ConflictsWith(existing []Occupied, deviceID uint, start, end int, front, rear bool) *Occupied {
	for i := range existing {
		o := &existing[i]
		if o.DeviceID == deviceID {
			continue
		}
		if !IntervalsOverlap(start, end, o.Start, o.End) {
			continue
		}
		if (front && o.Front) || (rear && o.Rear) {
			return o
		}
	}
	return nil
}

// UtilizedTicks is the union of occupied ticks on either face. A full-depth
// device is counted once.
func UtilizedTicks(occupied []Occupied, rackTicks int) int {
	if rackTicks <= 0 {
		return 0
	}
	type mark struct {
		at int
		d  int
	}
	marks := make([]mark, 0, len(occupied)*2)
	for _, o := range occupied {
		start, end := o.Start, o.End
		if start < 0 {
			start = 0
		}
		if end > rackTicks {
			end = rackTicks
		}
		if end <= start {
			continue
		}
		marks = append(marks, mark{start, 1}, mark{end, -1})
	}
	sort.Slice(marks, func(i, j int) bool {
		if marks[i].at == marks[j].at {
			return marks[i].d < marks[j].d
		}
		return marks[i].at < marks[j].at
	})
	cover, depth, last := 0, 0, 0
	for _, m := range marks {
		if depth > 0 {
			cover += m.at - last
		}
		depth += m.d
		last = m.at
	}
	return cover
}

func OccupancyPercent(occupied []Occupied, rackTicks int) int {
	if rackTicks <= 0 {
		return 0
	}
	u := UtilizedTicks(occupied, rackTicks)
	return int(math.Round(float64(u) * 100 / float64(rackTicks)))
}

func WholeUBoundary(offsetTicks, heightTicks int) bool {
	return offsetTicks%models.TicksPerU == 0 && heightTicks%models.TicksPerU == 0
}
