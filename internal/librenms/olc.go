package librenms

import (
	"errors"
	"math"
	"strings"
)

// Open Location Code (Plus Code). The integer encoding matches
// https://github.com/google/open-location-code so the published test
// vectors encode to the same strings.

const (
	olcAlphabet = "23456789CFGHJMPQRVWX"
	olcCodeLen  = 11
	olcPairLen  = 10
	olcMaxLen   = 15
	olcSepPos   = 8
	olcGridCols = 4
	olcGridRows = 5
	// 5**5 and 4**5, the grid refinements after the ten pair digits.
	olcGridLatFull = 3125
	olcGridLngFull = 1024
	// Pair precision is 8000. A length-11 code adds one 4×5 grid step,
	// about 2.8×3.5 m, which is inside coordEpsilon.
	olcFinalLatPrecision = 8000 * olcGridLatFull
	olcFinalLngPrecision = 8000 * olcGridLngFull
	olcLatMax            = 90
	olcLngMax            = 180
)

// openLocationCode is the full code for a point. The same cell shares one
// LibreNMS location. A short code that starts with "+" repeats about every
// 300 m, so two sites that far apart would share one pin.
func openLocationCode(lat, lng float64) string {
	return encodeOpenLocationCode(lat, lng, olcCodeLen)
}

// isOpenLocationCode reports whether name is a full code this sync may
// have written. Short codes and padded codes are left as human names.
func isOpenLocationCode(name string) bool {
	// 8 digits, "+", then 2 to 7 digits. 11 is the shortest full code
	// without padding; 16 is the 15-digit maximum.
	if len(name) < olcSepPos+1+2 || len(name) > olcMaxLen+1 || name[olcSepPos] != '+' {
		return false
	}
	for i := 0; i < len(name); i++ {
		if i == olcSepPos {
			continue
		}
		if !olcDigit(name[i]) {
			return false
		}
	}
	// Latitude only uses the first nine alphabet characters. Longitude
	// never emits the last two as its first digit.
	if olcIndex(name[0])*len(olcAlphabet) >= olcLatMax*2 {
		return false
	}
	if olcIndex(name[1])*len(olcAlphabet) >= olcLngMax*2 {
		return false
	}
	return true
}

func olcDigit(c byte) bool {
	return olcIndex(c) >= 0
}

func olcIndex(c byte) int {
	for i := 0; i < len(olcAlphabet); i++ {
		if olcAlphabet[i] == c {
			return i
		}
	}
	return -1
}

func encodeOpenLocationCode(lat, lng float64, codeLen int) string {
	return integerEncodeOLC(olcLatitude(lat), olcLongitude(lng), codeLen)
}

func olcLatitude(lat float64) int64 {
	latVal := int64(math.Floor(lat * olcFinalLatPrecision))
	latVal += olcLatMax * olcFinalLatPrecision
	limit := int64(2 * olcLatMax * olcFinalLatPrecision)
	if latVal < 0 {
		return 0
	}
	if latVal >= limit {
		return limit - 1
	}
	return latVal
}

func olcLongitude(lng float64) int64 {
	lngVal := int64(math.Floor(lng * olcFinalLngPrecision))
	lngVal += olcLngMax * olcFinalLngPrecision
	span := int64(2 * olcLngMax * olcFinalLngPrecision)
	if lngVal <= 0 {
		lngVal = lngVal%span + span
	} else if lngVal >= span {
		lngVal = lngVal % span
	}
	return lngVal
}

func integerEncodeOLC(latVal, lngVal int64, codeLen int) string {
	if codeLen <= 0 {
		codeLen = olcPairLen
	} else if codeLen < olcPairLen && codeLen%2 == 1 {
		codeLen++
	} else if codeLen > olcMaxLen {
		codeLen = olcMaxLen
	}
	code := []byte("00000000+0012345")
	if codeLen > olcPairLen {
		for i := olcMaxLen - olcPairLen; i >= 1; i-- {
			latDigit := latVal % olcGridRows
			lngDigit := lngVal % olcGridCols
			code[olcSepPos+2+i] = olcAlphabet[latDigit*olcGridCols+lngDigit]
			latVal /= olcGridRows
			lngVal /= olcGridCols
		}
	} else {
		latVal /= olcGridLatFull
		lngVal /= olcGridLngFull
	}
	code[olcSepPos+2], lngVal = olcPairStep(lngVal)
	code[olcSepPos+1], latVal = olcPairStep(latVal)
	for pairStart := olcPairLen/2 + 1; pairStart >= 0; pairStart -= 2 {
		code[pairStart+1], lngVal = olcPairStep(lngVal)
		code[pairStart], latVal = olcPairStep(latVal)
	}
	if codeLen >= olcSepPos {
		return string(code[:codeLen+1])
	}
	for i := codeLen; i < olcSepPos; i++ {
		code[i] = '0'
	}
	return string(code[:olcSepPos+1])
}

func olcPairStep(coordinate int64) (byte, int64) {
	digit := coordinate % int64(len(olcAlphabet))
	return olcAlphabet[digit], coordinate / int64(len(olcAlphabet))
}

// olcPairResolutions is the size, in degrees, of the area each pair of
// digits covers. Shortening drops pairs while the reference stays inside
// 0.3 of that size: four digits within about 33 km, six within about
// 1.7 km, and eight (a code that starts with "+") within about 80 m.
var olcPairResolutions = [...]float64{20.0, 1.0, 0.05, 0.0025, 0.000125}

type olcArea struct {
	LatLo, LngLo, LatHi, LngHi float64
	Len                        int
}

func (area olcArea) center() (float64, float64) {
	return math.Min(area.LatLo+(area.LatHi-area.LatLo)/2, olcLatMax),
		math.Min(area.LngLo+(area.LngHi-area.LngLo)/2, olcLngMax)
}

// shortenOpenLocationCode drops the prefix a nearby reference makes
// redundant. The full code is returned when the reference is too far.
func shortenOpenLocationCode(code string, lat, lng float64) (string, bool) {
	area, err := decodeOpenLocationCode(code)
	if err != nil || area.Len < 6 {
		return code, false
	}
	lat, lng = olcClipLat(lat), olcNormalizeLng(lng)
	centerLat, centerLng := area.center()
	distance := math.Max(math.Abs(centerLat-lat), math.Abs(centerLng-lng))
	for i := len(olcPairResolutions) - 2; i >= 1; i-- {
		if distance < olcPairResolutions[i]*0.3 {
			return code[(i+1)*2:], true
		}
	}
	return code, false
}

// isShortOpenLocationCode reports a code this sync writes after shortening:
// the "+" sits at 0, 2, or 4, and at least two digits follow it.
func isShortOpenLocationCode(code string) bool {
	plus := strings.IndexByte(code, '+')
	if plus < 0 || plus >= olcSepPos || plus%2 != 0 {
		return false
	}
	if len(code)-plus-1 < 2 {
		return false
	}
	for i := 0; i < len(code); i++ {
		if i == plus {
			continue
		}
		if !olcDigit(code[i]) {
			return false
		}
	}
	return true
}

func olcClipLat(lat float64) float64 {
	if lat > olcLatMax {
		return olcLatMax
	}
	if lat < -olcLatMax {
		return -olcLatMax
	}
	return lat
}

func olcNormalizeLng(lng float64) float64 {
	for lng < -olcLngMax {
		lng += 2 * olcLngMax
	}
	for lng >= olcLngMax {
		lng -= 2 * olcLngMax
	}
	return lng
}

func decodeOpenLocationCode(code string) (olcArea, error) {
	var area olcArea
	if !isOpenLocationCode(code) {
		return area, errors.New("not a full open location code")
	}
	stripped := strings.ReplaceAll(code, "+", "")
	codeLen := len(stripped)
	var lat, lng int64
	var height, width int64 = 1, 1
	base := int64(len(olcAlphabet))
	for i := 0; i < olcPairLen; i += 2 {
		lat *= base
		lng *= base
		height *= base
		if i < codeLen {
			lat += int64(olcIndex(stripped[i]))
			lng += int64(olcIndex(stripped[i+1]))
			height = 1
		}
	}
	width = height
	for i := olcPairLen; i < olcMaxLen; i++ {
		lat *= olcGridRows
		height *= olcGridRows
		lng *= olcGridCols
		width *= olcGridCols
		if i < codeLen {
			dval := int64(olcIndex(stripped[i]))
			lat += dval / olcGridCols
			lng += dval % olcGridCols
			height = 1
			width = 1
		}
	}
	latDegrees := float64(lat-olcLatMax*olcFinalLatPrecision) / float64(olcFinalLatPrecision)
	lngDegrees := float64(lng-olcLngMax*olcFinalLngPrecision) / float64(olcFinalLngPrecision)
	return olcArea{
		LatLo: latDegrees,
		LngLo: lngDegrees,
		LatHi: latDegrees + float64(height)/float64(olcFinalLatPrecision),
		LngHi: lngDegrees + float64(width)/float64(olcFinalLngPrecision),
		Len:   codeLen,
	}, nil
}
