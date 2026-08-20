package optical

import "math"

const speedOfLight = 299792458.0 // m/s

// THz converts Hz to terahertz.
func THz(hz uint64) float64 {
	if hz == 0 {
		return 0
	}
	return float64(hz) / 1e12
}

// Nm converts Hz to nanometres. Returns 0 if hz is 0.
func Nm(hz uint64) float64 {
	if hz == 0 {
		return 0
	}
	return speedOfLight * 1e9 / float64(hz)
}

// HzFromNm converts a wavelength in nm to Hz.
func HzFromNm(nm float64) uint64 {
	if nm <= 0 {
		return 0
	}
	return uint64(math.Round(speedOfLight * 1e9 / nm))
}

// HzFromTHz converts THz to Hz.
func HzFromTHz(thz float64) uint64 {
	if thz <= 0 {
		return 0
	}
	return uint64(math.Round(thz * 1e12))
}

// FreqFromITU maps a G.694.1 channel number on a grid (GHz) onto Hz.
// Anchor: channel 0 ≈ 193.1 THz on the 50 GHz grid is conventional for
// "type a channel" — we use 193.1 THz as channel 0 of the 50 GHz grid
// (ITU C-band often numbers from 190+). Default UI grid is 50 GHz.
//
// Here channel N on a 50 GHz grid is 193.1 THz + N*50 GHz. Negative
// channels are allowed (below 193.1 THz).
func FreqFromITU(channel int, gridGHz float64) uint64 {
	if gridGHz <= 0 {
		gridGHz = 50
	}
	hz := 193.1e12 + float64(channel)*gridGHz*1e9
	if hz <= 0 {
		return 0
	}
	return uint64(math.Round(hz))
}
