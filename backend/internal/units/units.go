// Package units contains the explicit unit conversions and numeric helpers
// shared by the sensor collectors.
//
// Kernel interfaces are inconsistent about units: RAPL and AMD energy files
// report microjoules, hwmon power files report microwatts, nvidia-smi reports
// watts, and /proc/diskstats reports milliseconds.  Keeping the conversions
// here makes each call site state its input unit explicitly and gives the
// conversions a single, testable implementation.
package units

import "math"

const (
	// MicrojoulesPerJoule converts RAPL/AMD energy counters to joules.
	MicrojoulesPerJoule = 1_000_000.0
	// MicrowattsPerWatt converts hwmon power attributes to watts.
	MicrowattsPerWatt = 1_000_000.0
)

// Finite reports whether v is a usable ordinary number.  Sensor files
// occasionally contain "NaN" or "inf"; those must never reach the JSON
// encoder, which cannot represent them.
func Finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// MicrojoulesToWatts converts an energy delta in microjoules over seconds
// into average watts.  It rejects non-finite values, negative deltas and
// non-positive intervals.
func MicrojoulesToWatts(deltaUJ, seconds float64) (float64, bool) {
	if !Finite(deltaUJ) || !Finite(seconds) || deltaUJ < 0 || seconds <= 0 {
		return 0, false
	}
	w := deltaUJ / MicrojoulesPerJoule / seconds
	if !Finite(w) {
		return 0, false
	}
	return w, true
}

// MicrowattsToWatts converts a microwatt reading to watts.
func MicrowattsToWatts(uw float64) (float64, bool) {
	if !Finite(uw) || uw < 0 {
		return 0, false
	}
	w := uw / MicrowattsPerWatt
	if !Finite(w) {
		return 0, false
	}
	return w, true
}

// Clamp constrains v to [lo, hi].
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Percent returns draw/max as an integer percentage in [0, 100].  A
// non-positive or non-finite maximum yields zero.
func Percent(draw, max float64) int {
	if !Finite(draw) || !Finite(max) || max <= 0 {
		return 0
	}
	p := draw / max * 100
	return int(math.Round(Clamp(p, 0, 100)))
}

// Round rounds v to a fixed number of decimal places.  Non-finite values are
// returned unchanged so callers can detect and reject them.
func Round(v float64, places int) float64 {
	if !Finite(v) {
		return v
	}
	p := math.Pow10(places)
	return math.Round(v*p) / p
}

// Round2 rounds v to two decimal places, the precision emitted in JSON.
func Round2(v float64) float64 {
	return Round(v, 2)
}
