// Package powersupply reads battery capacity and AC state from
// /sys/class/power_supply.
//
// Battery capacity is a 0..100 integer, or report.BatteryUnavailable (-1)
// when no usable capacity can be read.  Multiple batteries are averaged,
// weighted by energy_full/charge_full when available.  The source name is a
// display label: "AC" when external power is online, "Battery" when running
// from a battery, and the generic "PSU / Battery" when neither can be
// determined.
package powersupply

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"uthpower/backend/internal/fsx"
	"uthpower/backend/internal/units"
)

const (
	// NameAC is shown when external power is online.
	NameAC = "AC"
	// NameBattery is shown when the machine is running from a battery.
	NameBattery = "Battery"
	// NameFallback is shown when no power-supply information is available.
	NameFallback = "PSU / Battery"
)

// Status is the source-side telemetry for one sample.
type Status struct {
	Name           string
	BatteryPercent int
}

// Read scans /sys/class/power_supply.  It never returns an error; missing or
// unreadable entries degrade to the fallback name and the unavailable
// percentage sentinel.
func Read(fs *fsx.Root) Status {
	out := Status{Name: NameFallback, BatteryPercent: -1}
	if fs == nil {
		return out
	}

	entries, err := fs.ReadDir("sys", "class", "power_supply")
	if err != nil {
		return out
	}

	acOnline := false
	batteryExternal := false
	batteries := 0
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

		status := strings.ToLower(strings.TrimSpace(readString(filepath.Join(dir, "status"))))
		switch status {
		case "charging", "full", "not charging":
			batteryExternal = true
		}

		capacity, weight, ok := batteryLevel(dir)
		if ok {
			capacitySum += capacity * weight
			weightSum += weight
			capacityCount++
		}
	}

	switch {
	case acOnline || batteryExternal:
		out.Name = NameAC
	case batteries > 0:
		out.Name = NameBattery
	default:
		out.Name = NameFallback
	}

	if capacityCount > 0 && weightSum > 0 {
		out.BatteryPercent = clampPercent(capacitySum / weightSum)
	}
	return out
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
