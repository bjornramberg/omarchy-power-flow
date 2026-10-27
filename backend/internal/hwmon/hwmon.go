// Package hwmon reads GPU power sensors exposed through Linux hwmon.
//
// GPU drivers publish power1_input and/or power1_average in microwatts.
// Intel i915/xe GPUs may instead expose energy1_input in microjoules; that is
// sampled as a delta and converted to average watts.  power1_cap, when
// present, is used as the component's max_w.  Only known GPU drivers are
// considered so unrelated hwmon devices with a power/energy channel are not
// mistaken for a GPU.
package hwmon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
	"github.com/bjornramberg/omarchy-power-flow/backend/internal/units"
)

var gpuDrivers = map[string]bool{
	"amdgpu":     true,
	"radeon":     true,
	"nouveau":    true,
	"nvidia":     true,
	"nvidia-gpu": true,
	"i915":       true,
	"xe":         true,
	"panfrost":   true,
	"lima":       true,
	"msm":        true,
	"v3d":        true,
	"etnaviv":    true,
	"powervr":    true,
	"adreno":     true,
}

type device struct {
	name       string
	powerPaths []string
	energyPath string
	capPath    string
}

// Reading is the measurement for one GPU hwmon device.  DrawW comes either
// from an instantaneous power attribute or from an energy counter delta.
type Reading struct {
	Name  string
	DrawW float64
	MaxW  float64
	OK    bool
}

// Result aggregates all discovered GPU hwmon devices.
type Result struct {
	DrawW float64
	MaxW  float64
	Count int
	OK    bool
}

// Meter holds the discovered GPU devices and energy-counter state.
type Meter struct {
	devices    []device
	prevEnergy map[string]float64
	last       time.Time
}

// Discover locates GPU power/energy sensors below /sys/class/hwmon.
func Discover(fs *fsx.Root) *Meter {
	m := &Meter{prevEnergy: make(map[string]float64)}
	entries, err := fs.ReadDir("sys", "class", "hwmon")
	if err != nil {
		return m
	}

	for _, entry := range entries {
		dir := fs.Sys("class", "hwmon", entry.Name())
		name := strings.ToLower(strings.TrimSpace(readString(filepath.Join(dir, "name"))))
		if !gpuDrivers[name] {
			continue
		}

		dev := device{name: name, capPath: filepath.Join(dir, "power1_cap")}
		for _, candidate := range []string{"power1_input", "power1_average"} {
			path := filepath.Join(dir, candidate)
			if _, err := os.Stat(path); err == nil {
				dev.powerPaths = append(dev.powerPaths, path)
			}
		}
		energyPath := filepath.Join(dir, "energy1_input")
		if _, err := os.Stat(energyPath); err == nil {
			dev.energyPath = energyPath
		}
		if len(dev.powerPaths) == 0 && dev.energyPath == "" {
			continue
		}
		m.devices = append(m.devices, dev)
	}
	return m
}

// ReadingsAt returns one entry per discovered GPU device.  Instantaneous
// power attributes win; energy counters are used when no power attribute is
// usable.  Entries with unreadable or malformed values have OK == false.
func (m *Meter) ReadingsAt(now time.Time) []Reading {
	if m == nil {
		return nil
	}

	elapsed := 0.0
	if !m.last.IsZero() {
		elapsed = now.Sub(m.last).Seconds()
	}
	if elapsed < 0 {
		elapsed = 0
	}

	nextEnergy := make(map[string]float64, len(m.devices))
	out := make([]Reading, 0, len(m.devices))
	for _, dev := range m.devices {
		r := Reading{Name: dev.name}
		if w, ok := readPower(dev.powerPaths); ok {
			r.DrawW = w
			r.OK = true
		}

		if dev.energyPath != "" {
			energyUJ, ok := readNumber(dev.energyPath)
			if ok {
				nextEnergy[dev.energyPath] = energyUJ
				if !r.OK && elapsed > 0 {
					if prev, ok := m.prevEnergy[dev.energyPath]; ok && energyUJ >= prev {
						if w, ok := units.MicrojoulesToWatts(energyUJ-prev, elapsed); ok {
							r.DrawW = w
							r.OK = true
						}
					}
				}
			}
		}

		if r.OK {
			if uW, ok := readNumber(dev.capPath); ok {
				if w, ok := units.MicrowattsToWatts(uW); ok {
					r.MaxW = w
				}
			}
		}
		out = append(out, r)
	}

	m.prevEnergy = nextEnergy
	m.last = now
	return out
}

// Sample aggregates all working GPU sensors at one instant.  For energy-only
// devices the first call only primes the counter.
func (m *Meter) Sample(now time.Time) Result {
	var res Result
	for _, r := range m.ReadingsAt(now) {
		if !r.OK {
			continue
		}
		res.DrawW += r.DrawW
		res.MaxW += r.MaxW
		res.Count++
		res.OK = true
	}
	return res
}

// IsNVIDIA reports whether a hwmon driver belongs to an NVIDIA GPU.  The
// collector uses this to avoid counting the same GPU twice when nvidia-smi is
// also available.
func IsNVIDIA(name string) bool {
	switch strings.ToLower(name) {
	case "nouveau", "nvidia", "nvidia-gpu":
		return true
	}
	return false
}

func readPower(paths []string) (float64, bool) {
	for _, path := range paths {
		uW, ok := readNumber(path)
		if !ok {
			continue
		}
		if w, ok := units.MicrowattsToWatts(uW); ok {
			return w, true
		}
	}
	return 0, false
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
	if err != nil || !units.Finite(v) || v < 0 {
		return 0, false
	}
	return v, true
}
