// Package collector orchestrates all sensor sources into one schema-v2
// contract snapshot.
//
// Sensor discovery happens once at construction.  Sampling is stateful: the
// first call primes delta-based sources (RAPL/AMD energy, GPU energy, disk
// io_ticks, CPU jiffies), so the daemon first samples, waits for a short
// warm-up, and then begins emitting complete lines.
//
// Measured hardware watts always win.  When CPU/RAM watt sensors are absent,
// honest marked models may fill in, controlled by --estimates.
package collector

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"

	"uthpower/backend/internal/amdenergy"
	"uthpower/backend/internal/cpumodel"
	"uthpower/backend/internal/cpustat"
	"uthpower/backend/internal/diskstats"
	"uthpower/backend/internal/fsx"
	"uthpower/backend/internal/hwmon"
	"uthpower/backend/internal/nvidia"
	"uthpower/backend/internal/powercap"
	"uthpower/backend/internal/powersupply"
	"uthpower/backend/internal/rammodel"
	"uthpower/backend/internal/report"
	"uthpower/backend/internal/units"
)

// Fallback max_w values for measured components, taken from the design
// document.  They are only used for frontend scaling when the hardware does
// not expose a power cap or limit; they are never used as draw readings.
const (
	DefaultCPUMaxW  = 150.0
	DefaultGPUMaxW  = 300.0
	DefaultRAMMaxW  = 15.0
	DefaultDiskMaxW = diskstats.DefaultMaxW
)

const (
	cpuModelID  = "cpu-load/1"
	ramModelID  = "ram-capacity/1"
	diskModelID = "disk-busy/1"
)

// NvidiaMode controls whether nvidia-smi may be executed.
type NvidiaMode int

const (
	// NvidiaAuto runs nvidia-smi only on the real host filesystem, so a
	// --root verification run stays hermetic.
	NvidiaAuto NvidiaMode = iota
	// NvidiaOn always allows nvidia-smi when it is available.
	NvidiaOn
	// NvidiaOff never runs nvidia-smi.
	NvidiaOff
)

// ParseNvidiaMode parses auto/on/off.
func ParseNvidiaMode(value string) (NvidiaMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return NvidiaAuto, nil
	case "on":
		return NvidiaOn, nil
	case "off":
		return NvidiaOff, nil
	default:
		return NvidiaAuto, fmt.Errorf("invalid nvidia mode %q (want auto, on or off)", value)
	}
}

// EstimatesMode controls CPU/RAM modeled and utilization-only fallbacks.
type EstimatesMode int

const (
	// EstimatesAuto enables honest CPU/RAM models when measured watts are
	// unavailable.
	EstimatesAuto EstimatesMode = iota
	// EstimatesOff keeps measured components but omits CPU/RAM fallbacks.
	EstimatesOff
)

// ParseEstimatesMode parses auto/off.
func ParseEstimatesMode(value string) (EstimatesMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return EstimatesAuto, nil
	case "off":
		return EstimatesOff, nil
	default:
		return EstimatesAuto, fmt.Errorf("invalid estimates mode %q (want auto or off)", value)
	}
}

// Options configures a Collector.
type Options struct {
	Root          string
	NvidiaMode    NvidiaMode
	NvidiaSmiPath string
	NvidiaRunner  nvidia.Runner
	NvidiaTimeout time.Duration
	Estimates     EstimatesMode
	Arch          string // test override; defaults to runtime.GOARCH
	Now           func() time.Time
}

// Collector holds discovered sensors and delta state.
type Collector struct {
	fs        *fsx.Root
	powercap  *powercap.Meter
	amd       *amdenergy.Meter
	gpu       *hwmon.Meter
	nvidia    *nvidia.Reader
	disk      *diskstats.Meter
	cpu       *cpustat.Meter
	cpuInfo   cpumodel.Info
	estimates bool
	now       func() time.Time
}

// New discovers sensors and returns a Collector.  Discovery never fails:
// unsupported or unreadable sensors simply produce no component.
func New(opts Options) *Collector {
	fs := fsx.New(opts.Root)
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	arch := opts.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}

	c := &Collector{
		fs:        fs,
		powercap:  powercap.Discover(fs),
		amd:       amdenergy.Discover(fs),
		gpu:       hwmon.Discover(fs),
		disk:      diskstats.New(fs),
		cpu:       cpustat.New(),
		cpuInfo:   cpumodel.Detect(fs, arch),
		estimates: opts.Estimates != EstimatesOff,
		now:       now,
	}

	path := opts.NvidiaSmiPath
	if path == "" {
		path = "nvidia-smi"
	}
	available := opts.NvidiaRunner != nil
	if !available && opts.NvidiaMode != NvidiaOff &&
		(opts.NvidiaMode == NvidiaOn || fs.IsDefault()) {
		available = nvidia.Available(path)
	}
	enable := false
	switch opts.NvidiaMode {
	case NvidiaOff:
		enable = false
	case NvidiaOn:
		enable = available
	case NvidiaAuto:
		enable = available && fs.IsDefault()
	}
	if enable {
		c.nvidia = &nvidia.Reader{
			Path:    path,
			Runner:  opts.NvidiaRunner,
			Timeout: opts.NvidiaTimeout,
		}
	}

	return c
}

// Sample returns a snapshot using the configured clock.
func (c *Collector) Sample() report.Snapshot {
	return c.SampleAt(c.now())
}

// SampleAt returns one snapshot at a controlled instant.  Tests use it to
// exercise delta math without sleeping.
func (c *Collector) SampleAt(now time.Time) report.Snapshot {
	if c == nil {
		return report.Snapshot{
			SchemaVersion: report.SchemaVersion,
			Source: report.Source{
				Name:              powersupply.NameFallback,
				BatteryPercentage: report.BatteryUnavailable,
				TotalDrawKind:     report.TotalKindNone,
			},
			Components: []report.Component{},
		}
	}

	pc := c.powercap.Sample(now)
	amd := c.amd.Sample(now)
	cpuUtil := c.cpu.Sample(c.fs)
	disk := c.disk.Sample(now)
	ramInfo := rammodel.ReadInfo(c.fs)
	psu := powersupply.Read(c.fs)

	components := make([]report.Component, 0, 4)

	if comp := c.cpuComponent(pc.CPU, amd.CPU, cpuUtil); comp != nil {
		components = append(components, *comp)
	}
	if comp, ok := c.gpuComponent(now); ok {
		components = append(components, comp)
	}
	if comp := c.ramComponent(pc.RAM, amd.RAM, ramInfo, cpuUtil); comp != nil {
		components = append(components, *comp)
	}
	if disk.OK {
		draw := units.Round2(disk.DrawW)
		maxW := normalizeMax(disk.MaxW, draw, DefaultDiskMaxW)
		components = append(components, report.Component{
			ID:                 report.IDDisk,
			Label:              "DISK",
			DrawW:              report.Float64Ptr(draw),
			MaxW:               report.Float64Ptr(maxW),
			ActivityPercent:    disk.ActivityPercent,
			DrawKind:           report.DrawKindModeled,
			DrawSource:         "model:" + diskModelID,
			UncertaintyPercent: report.IntPtr(50),
			Model: &report.Model{ID: diskModelID, Assumptions: map[string]any{
				"idle_w":   diskstats.IdleDrawW,
				"full_w":   diskstats.DefaultMaxW,
				"activity": "sum(io_ticks delta) / elapsed",
			}},
		})
	}

	// The source-side total is deliberately the sum of emitted numeric
	// component measurements.  It is not battery/AC input power; power-supply
	// state is independent telemetry exposed through Name/BatteryPercentage.
	total := numericTotal(components)

	return report.Snapshot{
		SchemaVersion: report.SchemaVersion,
		Source: report.Source{
			Name:              psu.Name,
			TotalDrawW:        units.Round2(total),
			BatteryPercentage: psu.BatteryPercent,
			TotalDrawKind:     totalDrawKind(components),
		},
		Components: components,
	}
}

func (c *Collector) cpuComponent(pc powercap.Group, amd amdenergy.Group, util cpustat.Result) *report.Component {
	measured := pickMeasured(
		measuredGroup{DrawW: pc.DrawW, MaxW: pc.MaxW, OK: pc.OK, Source: "powercap:rapl"},
		measuredGroup{DrawW: amd.DrawW, MaxW: amd.MaxW, OK: amd.OK, Source: "amd_energy"},
	)
	if measured.OK {
		draw := units.Round2(measured.DrawW)
		maxW := normalizeMax(measured.MaxW, draw, DefaultCPUMaxW)
		activity := units.Percent(draw, maxW)
		var utilization *int
		if util.OK {
			activity = util.Percent
			utilization = report.IntPtr(util.Percent)
		}
		return &report.Component{
			ID:                 report.IDCPU,
			Label:              "CPU",
			DrawW:              report.Float64Ptr(draw),
			MaxW:               report.Float64Ptr(maxW),
			ActivityPercent:    activity,
			DrawKind:           report.DrawKindMeasured,
			DrawSource:         measured.Source,
			UtilizationPercent: utilization,
		}
	}

	if !c.estimates || !util.OK {
		return nil
	}

	if c.cpuInfo.VM || !c.cpuInfo.X86 {
		return utilizationComponent(util.Percent)
	}

	ratio, freqOK := cpumodel.FrequencyRatio(c.fs)
	est, ok := cpumodel.EstimateCPU(c.cpuInfo, util.Percent, ratio, freqOK)
	if !ok {
		return utilizationComponent(util.Percent)
	}

	return &report.Component{
		ID:                 report.IDCPU,
		Label:              "CPU",
		DrawW:              report.Float64Ptr(est.DrawW),
		MaxW:               report.Float64Ptr(est.MaxW),
		ActivityPercent:    util.Percent,
		DrawKind:           report.DrawKindModeled,
		DrawSource:         "model:" + cpuModelID,
		UtilizationPercent: report.IntPtr(util.Percent),
		UncertaintyPercent: report.IntPtr(est.UncertaintyPercent),
		Model:              &report.Model{ID: cpuModelID, Assumptions: est.Assumptions},
	}
}

func utilizationComponent(utilPercent int) *report.Component {
	return &report.Component{
		ID:                 report.IDCPU,
		Label:              "CPU",
		DrawW:              nil,
		MaxW:               nil,
		ActivityPercent:    utilPercent,
		DrawKind:           report.DrawKindUtilization,
		DrawSource:         "proc:stat",
		UtilizationPercent: report.IntPtr(utilPercent),
	}
}

func (c *Collector) ramComponent(pc powercap.Group, amd amdenergy.Group, info rammodel.Info, util cpustat.Result) *report.Component {
	measured := pickMeasured(
		measuredGroup{DrawW: pc.DrawW, MaxW: pc.MaxW, OK: pc.OK, Source: "powercap:rapl"},
		measuredGroup{DrawW: amd.DrawW, MaxW: amd.MaxW, OK: amd.OK, Source: "amd_energy"},
	)
	if measured.OK {
		draw := units.Round2(measured.DrawW)
		maxW := normalizeMax(measured.MaxW, draw, DefaultRAMMaxW)
		var utilization *int
		if pct, ok := rammodel.UtilizationPercent(info); ok {
			utilization = report.IntPtr(pct)
		}
		return &report.Component{
			ID:                 report.IDRAM,
			Label:              "RAM",
			DrawW:              report.Float64Ptr(draw),
			MaxW:               report.Float64Ptr(maxW),
			ActivityPercent:    units.Percent(draw, maxW),
			DrawKind:           report.DrawKindMeasured,
			DrawSource:         measured.Source,
			UtilizationPercent: utilization,
		}
	}

	if !c.estimates {
		return nil
	}
	est, ok := rammodel.EstimateWatts(info, util.Percent, util.OK)
	if !ok {
		return nil
	}
	return &report.Component{
		ID:                 report.IDRAM,
		Label:              "RAM",
		DrawW:              report.Float64Ptr(est.DrawW),
		MaxW:               report.Float64Ptr(est.MaxW),
		ActivityPercent:    est.ActivityPercent,
		DrawKind:           report.DrawKindModeled,
		DrawSource:         "model:" + ramModelID,
		UtilizationPercent: report.IntPtr(est.UtilizationPercent),
		UncertaintyPercent: report.IntPtr(est.UncertaintyPercent),
		Model:              &report.Model{ID: ramModelID, Assumptions: est.Assumptions},
	}
}

// gpuComponent aggregates hwmon GPUs and nvidia-smi.  When nvidia-smi works,
// NVIDIA hwmon devices (nouveau/nvidia) are skipped to avoid double counting
// the same physical GPU.
func (c *Collector) gpuComponent(now time.Time) (report.Component, bool) {
	var nv nvidia.Result
	if c.nvidia != nil {
		nv = c.nvidia.Sample(context.Background())
	}

	var (
		draw    float64
		max     float64
		count   int
		capped  int
		hasUtil bool
		util    float64
	)
	hwmonNames := make(map[string]bool)
	for _, r := range c.gpu.ReadingsAt(now) {
		if !r.OK {
			continue
		}
		if nv.OK && hwmon.IsNVIDIA(r.Name) {
			continue
		}
		draw += r.DrawW
		max += r.MaxW
		count++
		hwmonNames[r.Name] = true
		if r.MaxW > 0 {
			capped++
		}
	}
	if nv.OK {
		draw += nv.DrawW
		max += nv.MaxW
		count += nv.Count
		capped += nv.Capped
		if nv.HasUtil {
			hasUtil = true
			util = nv.UtilPercent
		}
	}

	if count == 0 {
		return report.Component{}, false
	}

	draw = units.Round2(draw)
	// Apply the design's fallback cap per GPU that did not report a limit.
	max += float64(count-capped) * DefaultGPUMaxW
	max = normalizeMax(max, draw, DefaultGPUMaxW)
	activity := units.Percent(draw, max)
	var utilization *int
	if hasUtil {
		pct := int(math.Round(units.Clamp(util, 0, 100)))
		activity = pct
		utilization = report.IntPtr(pct)
	}

	source := hwmonSource(hwmonNames)
	if nv.OK {
		if source != "" {
			source += "+nvidia-smi"
		} else {
			source = "nvidia-smi"
		}
	}

	return report.Component{
		ID:                 report.IDGPU,
		Label:              "GPU",
		DrawW:              report.Float64Ptr(draw),
		MaxW:               report.Float64Ptr(max),
		ActivityPercent:    activity,
		DrawKind:           report.DrawKindMeasured,
		DrawSource:         source,
		UtilizationPercent: utilization,
	}, true
}

func hwmonSource(names map[string]bool) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		for name := range names {
			return "hwmon:" + name
		}
	}
	return "hwmon:multiple"
}

type measuredGroup struct {
	DrawW  float64
	MaxW   float64
	OK     bool
	Source string
}

func pickMeasured(primary, fallback measuredGroup) measuredGroup {
	if primary.OK {
		return primary
	}
	if fallback.OK {
		return fallback
	}
	return measuredGroup{}
}

func totalDrawKind(components []report.Component) string {
	if len(components) == 0 {
		return report.TotalKindNone
	}
	allMeasured := true
	allModeled := true
	for _, comp := range components {
		if comp.DrawKind != report.DrawKindMeasured {
			allMeasured = false
		}
		if comp.DrawKind != report.DrawKindModeled {
			allModeled = false
		}
	}
	switch {
	case allMeasured:
		return report.TotalKindMeasured
	case allModeled:
		return report.TotalKindModeled
	default:
		return report.TotalKindMixed
	}
}

func numericTotal(components []report.Component) float64 {
	total := 0.0
	for _, comp := range components {
		if comp.DrawW != nil {
			total += *comp.DrawW
		}
	}
	return total
}

func normalizeMax(maxW, drawW, fallback float64) float64 {
	if !units.Finite(maxW) || maxW < 0 {
		maxW = 0
	}
	if maxW == 0 {
		maxW = fallback
	}
	if drawW > maxW {
		maxW = drawW
	}
	return units.Round2(maxW)
}
