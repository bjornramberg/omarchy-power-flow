// Package amdenergy reads AMD CPU/DRAM energy counters exposed by the
// amd_energy hwmon driver (energyN_input in microjoules).
//
// amd_energy can expose package-level channels, per-core channels and DRAM
// channels.  To avoid double counting, package channels win when present;
// otherwise core/unknown channels are summed.  DRAM channels are always
// reported separately as RAM.
package amdenergy

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
	"github.com/bjornramberg/omarchy-power-flow/backend/internal/units"
)

var energyFile = regexp.MustCompile(`^energy[0-9]+_input$`)

type channelClass int

const (
	classUnknown channelClass = iota
	classPackage
	classCore
	classRAM
)

type channel struct {
	path  string
	class channelClass
}

type reading struct {
	energyUJ float64
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

// Meter tracks cumulative channel counters between samples.
type Meter struct {
	channels   []channel
	hasPackage bool
	hasCore    bool
	hasRAM     bool
	maxCPUW    float64
	prev       map[string]reading
	last       time.Time
}

// Discover finds amd_energy hwmon devices below /sys/class/hwmon.  Any
// unreadable or malformed attribute is ignored.
func Discover(fs *fsx.Root) *Meter {
	m := &Meter{prev: make(map[string]reading)}
	hwmons, err := fs.ReadDir("sys", "class", "hwmon")
	if err != nil {
		return m
	}

	for _, hw := range hwmons {
		dir := fs.Sys("class", "hwmon", hw.Name())
		name := strings.ToLower(strings.TrimSpace(readString(filepath.Join(dir, "name"))))
		if name != "amd_energy" {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		deviceChannels := 0
		for _, entry := range entries {
			fileName := entry.Name()
			if !energyFile.MatchString(fileName) {
				continue
			}
			label := strings.TrimSpace(readString(filepath.Join(dir,
				strings.TrimSuffix(fileName, "_input")+"_label")))
			if label == "" {
				label = strings.TrimSuffix(fileName, "_input")
			}
			class := classify(label)
			switch class {
			case classPackage:
				m.hasPackage = true
			case classCore:
				m.hasCore = true
			case classRAM:
				m.hasRAM = true
			}
			m.channels = append(m.channels, channel{
				path:  filepath.Join(dir, fileName),
				class: class,
			})
			if class != classRAM {
				deviceChannels++
			}
		}

		// Some AMD hwmon devices also expose a package power cap.  Use it for
		// scaling when it is present.
		if deviceChannels > 0 {
			if uW, ok := readFloat(filepath.Join(dir, "power1_cap")); ok {
				if w, ok := units.MicrowattsToWatts(uW); ok {
					m.maxCPUW += w
				}
			}
		}
	}

	return m
}

func classify(label string) channelClass {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "dram"), strings.Contains(l, "mem"):
		return classRAM
	case strings.Contains(l, "package"), strings.Contains(l, "socket"),
		strings.Contains(l, "pkg"), strings.Contains(l, "tdp"):
		return classPackage
	case strings.Contains(l, "core"), strings.Contains(l, "ccd"),
		strings.Contains(l, "fabric"), strings.Contains(l, "fclk"):
		return classCore
	default:
		return classUnknown
	}
}

// Sample returns average power over the interval since the preceding call.
// The first call only primes counters.
func (m *Meter) Sample(now time.Time) Result {
	if m == nil {
		return Result{}
	}
	res := Result{CPU: Group{MaxW: m.maxCPUW}}
	if len(m.channels) == 0 {
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

	sums := map[channelClass]float64{}
	measured := map[channelClass]int{}
	next := make(map[string]reading, len(m.channels))

	for _, ch := range m.channels {
		cur, ok := readChannel(ch.path)
		if !ok {
			continue
		}
		if old, ok := m.prev[ch.path]; ok && elapsed > 0 {
			delta := cur.energyUJ - old.energyUJ
			if delta < 0 {
				// No documented wrap range is available for hwmon energy
				// counters; treat a decreasing counter as a reset and skip
				// this interval rather than inventing a value.
				delta = 0
				next[ch.path] = cur
				continue
			}
			if drawW, ok := units.MicrojoulesToWatts(delta, elapsed); ok {
				sums[ch.class] += drawW
				measured[ch.class]++
			}
		}
		next[ch.path] = cur
	}

	if m.hasPackage {
		if measured[classPackage] > 0 {
			res.CPU.DrawW = sums[classPackage]
			res.CPU.OK = true
		}
	} else if measured[classCore]+measured[classUnknown] > 0 {
		res.CPU.DrawW = sums[classCore] + sums[classUnknown]
		res.CPU.OK = true
	}

	if m.hasRAM && measured[classRAM] > 0 {
		res.RAM.DrawW = sums[classRAM]
		res.RAM.OK = true
	}

	m.prev = next
	m.last = now
	return res
}

func readChannel(path string) (reading, bool) {
	v, ok := readFloat(path)
	if !ok {
		return reading{}, false
	}
	return reading{energyUJ: v}, true
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
