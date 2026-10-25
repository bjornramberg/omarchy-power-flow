// Package rammodel reads memory capacity/usage/pressure and produces an
// explicitly modeled RAM power estimate.
//
// The model deliberately does not scale watts with occupancy.  Exact occupancy
// is exposed separately as utilization_percent; a weak activity index combines
// memory pressure (when available) with CPU utilization to choose a point
// between idle and active watts per GiB.
package rammodel

import (
	"math"
	"strconv"
	"strings"

	"uthpower/backend/internal/fsx"
	"uthpower/backend/internal/units"
)

const (
	// IdleWPerGiB and ActiveWPerGiB are conservative generic coefficients.
	IdleWPerGiB   = 0.15
	ActiveWPerGiB = 0.35

	bytesPerGiB = 1024 * 1024 * 1024
)

// Info is one /proc/meminfo + pressure reading.
type Info struct {
	TotalBytes     uint64
	AvailableBytes uint64
	TotalOK        bool
	AvailableOK    bool
	PressureAvg10  float64
	PressureOK     bool
}

// Estimate is a modeled RAM power reading.
type Estimate struct {
	DrawW              float64
	MaxW               float64
	ActivityPercent    int
	UtilizationPercent int
	UncertaintyPercent int
	CapacityGiB        float64
	ActivityIndex      float64
	Assumptions        map[string]any
}

// ReadInfo parses /proc/meminfo and /proc/pressure/memory.
func ReadInfo(fs *fsx.Root) Info {
	var info Info
	if fs == nil {
		return info
	}
	if data, err := fs.ReadFile("proc", "meminfo"); err == nil {
		info.TotalBytes, info.AvailableBytes, info.TotalOK, info.AvailableOK = parseMemInfo(data)
	}
	if data, err := fs.ReadFile("proc", "pressure", "memory"); err == nil {
		info.PressureAvg10, info.PressureOK = parsePressure(data)
	}
	return info
}

func parseMemInfo(data []byte) (total, available uint64, totalOK, availableOK bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		bytes := value * 1024
		switch fields[0] {
		case "MemTotal:":
			total, totalOK = bytes, bytes > 0
		case "MemAvailable:":
			available, availableOK = bytes, true
		}
	}
	if totalOK && availableOK && available > total {
		available = total
	}
	return total, available, totalOK, availableOK
}

func parsePressure(data []byte) (float64, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			value, ok := strings.CutPrefix(field, "avg10=")
			if !ok {
				continue
			}
			v, err := strconv.ParseFloat(value, 64)
			if err != nil || !units.Finite(v) || v < 0 {
				return 0, false
			}
			return math.Min(v, 100), true
		}
	}
	return 0, false
}

// UtilizationPercent returns exact used/total memory occupancy.
func UtilizationPercent(info Info) (int, bool) {
	if !info.TotalOK || !info.AvailableOK || info.TotalBytes == 0 {
		return 0, false
	}
	available := info.AvailableBytes
	if available > info.TotalBytes {
		available = info.TotalBytes
	}
	used := info.TotalBytes - available
	pct := float64(used) / float64(info.TotalBytes) * 100
	return int(math.Round(units.Clamp(pct, 0, 100))), true
}

// EstimateWatts models RAM power.  It requires capacity and available memory;
// without MemTotal (or MemAvailable) there is no honest model and callers
// should omit RAM.
func EstimateWatts(info Info, cpuUtil int, cpuOK bool) (Estimate, bool) {
	util, ok := UtilizationPercent(info)
	if !ok {
		return Estimate{}, false
	}

	capacityGiB := float64(info.TotalBytes) / bytesPerGiB
	idle := capacityGiB * IdleWPerGiB
	maximum := capacityGiB * ActiveWPerGiB

	cpuFrac := 0.0
	if cpuOK {
		cpuFrac = clamp01(float64(cpuUtil) / 100)
	}
	pressureFrac := 0.0
	if info.PressureOK {
		pressureFrac = clamp01(info.PressureAvg10 / 100)
	}

	activity := 0.0
	formula := "idle + (max-idle) * (0.25 * cpu_util)"
	if info.PressureOK {
		activity = 0.75*pressureFrac + 0.25*cpuFrac
		formula = "idle + (max-idle) * clamp(0.75 * mem_pressure_avg10 + 0.25 * cpu_util)"
	} else {
		activity = 0.25 * cpuFrac
	}
	activity = clamp01(activity)

	draw := idle + (maximum-idle)*activity
	draw = math.Max(idle, math.Min(maximum, draw))

	uncertainty := 50
	if !info.PressureOK || !cpuOK {
		uncertainty = 55
	}

	assumptions := map[string]any{
		"capacity_gib":       round2(capacityGiB),
		"idle_w_per_gib":     IdleWPerGiB,
		"active_w_per_gib":   ActiveWPerGiB,
		"pressure_available": info.PressureOK,
		"cpu_available":      cpuOK,
		"formula":            formula,
	}

	return Estimate{
		DrawW:              round2(draw),
		MaxW:               round2(maximum),
		ActivityPercent:    int(math.Round(activity * 100)),
		UtilizationPercent: util,
		UncertaintyPercent: uncertainty,
		CapacityGiB:        round2(capacityGiB),
		ActivityIndex:      activity,
		Assumptions:        assumptions,
	}, true
}

func clamp01(v float64) float64 {
	if !math.IsNaN(v) && v > 0 {
		if v > 1 {
			return 1
		}
		return v
	}
	return 0
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
