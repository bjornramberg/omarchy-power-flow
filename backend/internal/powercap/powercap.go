// Package powercap reads Linux powercap energy counters (Intel RAPL and any
// other driver exposing energy_uj in /sys/class/powercap).
//
// Energy counters are cumulative microjoules; power is derived from a delta
// over elapsed wall time.  The first sample only primes the counters, so the
// collector emits power components from the second sample onward.
package powercap

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
	"github.com/bjornramberg/omarchy-power-flow/backend/internal/units"
)

// Kind is the component a powercap zone contributes to.
type Kind int

const (
	KindCPU Kind = iota
	KindRAM
)

type zone struct {
	path      string
	kind      Kind
	maxPowerW float64
}

type reading struct {
	energyUJ   float64
	maxRangeUJ float64
}

// Group is one aggregated component measurement.
type Group struct {
	DrawW float64
	MaxW  float64
	OK    bool
}

// Result holds the CPU and RAM measurements from one sampling instant.
type Result struct {
	CPU Group
	RAM Group
}

// Meter tracks cumulative zone counters between samples.
type Meter struct {
	zones   []zone
	maxCPUW float64
	maxRAMW float64
	prev    map[string]reading
	last    time.Time
}

// Discover builds a Meter from /sys/class/powercap.  Zones are identified by
// their "name" attribute (package-*, dram, psys), never by the numeric
// directory name, so Intel and modern AMD powercap drivers behave the same.
//
// energy_uj is commonly readable only by root.  A permission error is treated
// exactly like a missing sensor: the zone is skipped and no component is
// emitted.  Missing directories and unreadable attributes are likewise
// ignored; the returned Meter is always usable.
func Discover(fs *fsx.Root) *Meter {
	m := &Meter{prev: make(map[string]reading)}
	entries, err := fs.ReadDir("sys", "class", "powercap")
	if err != nil {
		return m
	}

	for _, entry := range entries {
		name := entry.Name()
		dir := fs.Sys("class", "powercap", name)
		energyPath := filepath.Join(dir, "energy_uj")
		if _, err := os.Stat(energyPath); err != nil {
			continue
		}

		zoneName := strings.TrimSpace(readString(filepath.Join(dir, "name")))
		kind, keep := classify(zoneName)
		if !keep {
			continue
		}

		// "enabled" controls power capping, not energy monitoring.  A zone
		// with enabled=0 still exposes a usable energy_uj counter.

		z := zone{path: dir, kind: kind}
		uW, ok := readFloat(filepath.Join(dir, "constraint_0_max_power_uw"))
		if !ok || uW == 0 {
			uW, ok = readFloat(filepath.Join(dir, "constraint_0_power_limit_uw"))
		}
		if ok {
			if w, ok := units.MicrowattsToWatts(uW); ok {
				z.maxPowerW = w
			}
		}
		m.zones = append(m.zones, z)
		if kind == KindCPU {
			m.maxCPUW += z.maxPowerW
		} else {
			m.maxRAMW += z.maxPowerW
		}
	}

	return m
}

// classify maps a powercap zone to CPU or RAM using the zone's name file.
// Package zones ("package-0", "package-1", ...) count as CPU, "dram" zones as
// RAM, and "psys" zones are skipped because they represent the whole machine.
// Anything else (core, uncore, gt, unknown) is ignored.
//
// Zone names, rather than directory names such as intel-rapl:0, are the
// stable interface: modern AMD powercap drivers use the same layout and names.
func classify(zoneName string) (Kind, bool) {
	name := strings.ToLower(strings.TrimSpace(zoneName))
	switch {
	case name == "":
		return 0, false
	case strings.HasPrefix(name, "package"):
		return KindCPU, true
	case name == "dram", strings.HasPrefix(name, "dram-"), strings.HasPrefix(name, "dram_"):
		return KindRAM, true
	case strings.HasPrefix(name, "psys"):
		return 0, false
	default:
		return 0, false
	}
}

// Sample returns power over the interval since the previous Sample call.
// Zones that cannot be read are simply left out.  Group.MaxW is still
// reported on the first sample so the frontend can scale components while
// power is being primed.
func (m *Meter) Sample(now time.Time) Result {
	if m == nil {
		return Result{}
	}
	res := Result{
		CPU: Group{MaxW: m.maxCPUW},
		RAM: Group{MaxW: m.maxRAMW},
	}
	if len(m.zones) == 0 {
		return res
	}

	hasPrev := !m.last.IsZero()
	elapsed := 0.0
	if hasPrev {
		elapsed = now.Sub(m.last).Seconds()
	}
	if elapsed < 0 {
		elapsed = 0
	}

	next := make(map[string]reading, len(m.zones))
	for _, z := range m.zones {
		cur, ok := readZone(z.path)
		if !ok {
			// Drop the previous value so the next successful sample starts a
			// fresh interval instead of reporting a bogus delta.
			continue
		}

		if old, ok := m.prev[z.path]; ok && elapsed > 0 {
			delta := cur.energyUJ - old.energyUJ
			if delta < 0 {
				// Microjoule counters wrap at max_energy_range_uj.  Use the
				// largest range we have seen to reconstruct the delta.
				rangeUJ := cur.maxRangeUJ
				if old.maxRangeUJ > rangeUJ {
					rangeUJ = old.maxRangeUJ
				}
				if rangeUJ > 0 {
					delta += rangeUJ
				} else {
					delta = 0
				}
			}
			if drawW, ok := units.MicrojoulesToWatts(delta, elapsed); ok {
				if z.kind == KindCPU {
					res.CPU.DrawW += drawW
					res.CPU.OK = true
				} else {
					res.RAM.DrawW += drawW
					res.RAM.OK = true
				}
			}
		}

		next[z.path] = cur
	}

	m.prev = next
	m.last = now
	return res
}

func readZone(dir string) (reading, bool) {
	energy, ok := readFloat(filepath.Join(dir, "energy_uj"))
	if !ok {
		return reading{}, false
	}
	var maxRange float64
	if v, ok := readFloat(filepath.Join(dir, "max_energy_range_uj")); ok {
		maxRange = v
	}
	return reading{energyUJ: energy, maxRangeUJ: maxRange}, true
}

func readString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readFloat(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || !units.Finite(v) || v < 0 {
		return 0, false
	}
	return v, true
}
