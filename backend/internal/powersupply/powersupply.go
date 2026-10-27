// Package powersupply reads battery capacity and power-source state from
// /sys/class/power_supply.
//
// Battery capacity is a 0..100 integer, or report.BatteryUnavailable (-1)
// when no usable capacity can be read.  Multiple batteries are averaged,
// weighted by energy_full/charge_full when available.
//
// PowerMode is explicit:
//   - battery: at least one genuine system battery is discharging
//   - ac:      an external supply reports online=1, or a battery reports
//     charging without an AC sensor
//   - unknown: no trustworthy state is available
//
// A full battery alone does not prove external power.  Name is a display label
// derived only from these modes, never from sysfs device names.
package powersupply

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
	"github.com/bjornramberg/omarchy-power-flow/backend/internal/units"
)

// Display names.  NameFallback is kept as a compatibility alias for callers
// that used the old fallback constant; the truthful value is "Unknown".
const (
	NameAC       = "AC"
	NameBattery  = "Battery"
	NameUnknown  = "Unknown"
	NameFallback = NameUnknown
)

// Explicit source modes.
const (
	ModeAC      = "ac"
	ModeBattery = "battery"
	ModeUnknown = "unknown"
)

// Battery statuses.
const (
	BatteryCharging    = "charging"
	BatteryDischarging = "discharging"
	BatteryFull        = "full"
	BatteryNotCharging = "not_charging"
	BatteryUnknown     = "unknown"
)

// Status is the source-side telemetry for one sample.
type Status struct {
	Name           string
	BatteryPercent int
	PowerMode      string
	BatteryStatus  string
}

// Read scans /sys/class/power_supply.  It never returns an error; missing or
// unreadable entries degrade to unknown and the unavailable percentage
// sentinel.
func Read(fs *fsx.Root) Status {
	out := Status{
		Name:           NameUnknown,
		BatteryPercent: -1,
		PowerMode:      ModeUnknown,
	}
	if fs == nil {
		return out
	}

	entries, err := fs.ReadDir("sys", "class", "power_supply")
	if err != nil {
		return out
	}

	acOnline := false
	batteries := 0
	discharging := false
	charging := false
	full := false
	notCharging := false

	capacitySum := 0.0
	weightSum := 0.0
	capacityCount := 0

	for _, entry := range entries {
		dir := fs.Sys("class", "power_supply", entry.Name())
		typ := strings.ToLower(strings.TrimSpace(readString(filepath.Join(dir, "type"))))
		if typ == "" {
			continue
		}

		if typ != "battery" {
			// Only the online flag matters.  Do not infer external power
			// from device names such as "AC" or "ADP1".
			if strings.TrimSpace(readString(filepath.Join(dir, "online"))) == "1" {
				acOnline = true
			}
			continue
		}

		if strings.ToLower(strings.TrimSpace(readString(filepath.Join(dir, "scope")))) == "device" {
			continue // peripheral battery, not the system battery
		}
		if strings.TrimSpace(readString(filepath.Join(dir, "present"))) == "0" {
			continue
		}
		batteries++

		switch batteryStatus(readString(filepath.Join(dir, "status"))) {
		case BatteryDischarging:
			discharging = true
		case BatteryCharging:
			charging = true
		case BatteryFull:
			full = true
		case BatteryNotCharging:
			notCharging = true
		default:
			// Unknown status still counts as a present battery; the aggregate
			// below falls back to BatteryUnknown when no known status exists.
		}

		capacity, weight, ok := batteryLevel(dir)
		if ok {
			capacitySum += capacity * weight
			weightSum += weight
			capacityCount++
		}
	}

	if batteries > 0 {
		switch {
		case discharging:
			out.BatteryStatus = BatteryDischarging
		case charging:
			out.BatteryStatus = BatteryCharging
		case full:
			out.BatteryStatus = BatteryFull
		case notCharging:
			out.BatteryStatus = BatteryNotCharging
		default:
			// A battery exists but its status is unreadable/unrecognized.
			out.BatteryStatus = BatteryUnknown
		}
	}

	switch {
	case discharging:
		out.PowerMode = ModeBattery
	case acOnline:
		out.PowerMode = ModeAC
	case charging:
		// A battery that is actively charging implies an external supply even
		// when no AC device is exposed.  The battery status remains charging.
		out.PowerMode = ModeAC
	default:
		out.PowerMode = ModeUnknown
	}

	switch out.PowerMode {
	case ModeAC:
		out.Name = NameAC
	case ModeBattery:
		out.Name = NameBattery
	default:
		out.Name = NameUnknown
	}

	if capacityCount > 0 && weightSum > 0 {
		out.BatteryPercent = clampPercent(capacitySum / weightSum)
	}
	return out
}

func batteryStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "charging":
		return BatteryCharging
	case "discharging":
		return BatteryDischarging
	case "full":
		return BatteryFull
	case "not charging":
		return BatteryNotCharging
	default:
		return BatteryUnknown
	}
}

// batteryLevel returns capacity percent and an averaging weight.  capacity is
// preferred when present; energy_now/energy_full and charge_now/charge_full
// are used as fallbacks.
func batteryLevel(dir string) (capacity, weight float64, ok bool) {
	if v, ok := readNumber(filepath.Join(dir, "capacity")); ok {
		if v < 0 {
			return 0, 1, false
		}
		return units.Clamp(v, 0, 100), readWeight(dir), true
	}

	for _, pair := range [][2]string{
		{"energy_now", "energy_full"},
		{"charge_now", "charge_full"},
	} {
		now, okNow := readNumber(filepath.Join(dir, pair[0]))
		full, okFull := readNumber(filepath.Join(dir, pair[1]))
		if okNow && okFull && full > 0 {
			return units.Clamp(now/full*100, 0, 100), full, true
		}
	}
	return 0, 1, false
}

func readWeight(dir string) float64 {
	for _, name := range []string{"energy_full", "charge_full"} {
		if v, ok := readNumber(filepath.Join(dir, name)); ok && v > 0 {
			return v
		}
	}
	return 1
}

func clampPercent(v float64) int {
	if !units.Finite(v) {
		return -1
	}
	return int(math.Round(units.Clamp(v, 0, 100)))
}

func readString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readNumber(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || !units.Finite(v) {
		return 0, false
	}
	return v, true
}
