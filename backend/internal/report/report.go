// Package report defines the schema-v2 JSON contract between the collector
// and the Quickshell frontend.
//
// The field order below is deliberate: encoding/json emits struct fields in
// declaration order, so the contract is deterministic.  The output is one
// compact JSON object followed by a newline; no pretty printing.
//
// normalize is the final boundary before encoding.  It clamps percentages,
// replaces non-finite numbers, and sanitizes model assumptions so every call
// to Encode/MarshalLine still produces one valid JSON line.
package report

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
)

// SchemaVersion is the current NDJSON schema version.
const SchemaVersion = 2

// SafetyMaxDrawW bounds absurd finite draw values before they reach JSON.
// One megawatt is far beyond any plausible component rail.
const SafetyMaxDrawW = 1_000_000.0

// Sentinel values and component identifiers.  The frontend must treat
// BatteryUnavailable as "unknown", not as a real battery level.
const (
	// BatteryUnavailable is emitted when no battery capacity can be read.
	BatteryUnavailable = -1

	IDCPU  = "cpu"
	IDGPU  = "gpu"
	IDRAM  = "ram"
	IDDisk = "disk"
)

// Explicit source state.  "AC" here means an online external supply, not a
// display-name guess.
const (
	PowerModeAC      = "ac"
	PowerModeBattery = "battery"
	PowerModeUnknown = "unknown"
)

// Battery status vocabulary.
const (
	BatteryCharging    = "charging"
	BatteryDischarging = "discharging"
	BatteryFull        = "full"
	BatteryNotCharging = "not_charging"
	BatteryUnknown     = "unknown"
)

// Top-process metric vocabulary.
const (
	TopMetricCPU = "cpu"
	TopMetricRSS = "rss"
)

// How a component's draw_w was obtained.
const (
	DrawKindMeasured    = "measured"
	DrawKindModeled     = "modeled"
	DrawKindUtilization = "utilization"
)

// How the source total_draw_w was obtained.
const (
	TotalKindMeasured = "measured"
	TotalKindModeled  = "modeled"
	TotalKindMixed    = "mixed"
	TotalKindNone     = "none"
)

// Source is the power source side of the contract.  TotalDrawW is the
// arithmetic sum of the emitted numeric component draws, not a battery/AC
// input-power measurement; power-supply state is described by PowerMode,
// BatteryPercentage and BatteryStatus.
type Source struct {
	Name              string  `json:"name"`
	TotalDrawW        float64 `json:"total_draw_w"`
	BatteryPercentage int     `json:"battery_percentage"`
	TotalDrawKind     string  `json:"total_draw_kind"`
	PowerMode         string  `json:"power_mode"`
	BatteryStatus     string  `json:"battery_status,omitempty"`
}

// Model describes a watt estimate.  Assumptions is a small, stable set of
// model inputs; encoding/json sorts map keys, keeping output deterministic.
type Model struct {
	ID          string         `json:"id"`
	Assumptions map[string]any `json:"assumptions"`
}

// TopProcess is optional CPU/RAM process metadata.  It is never a power
// claim: CPU values are a share of machine CPU time and RSS is resident
// memory in bytes.
type TopProcess struct {
	PID         int      `json:"pid"`
	Name        string   `json:"name"`
	Metric      string   `json:"metric"`
	Value       float64  `json:"value"`
	Unit        string   `json:"unit"`
	CorePercent *float64 `json:"core_percent,omitempty"`
	WindowMS    int      `json:"window_ms"`
	Scope       string   `json:"scope"`
	Source      string   `json:"source"`
	OK          bool     `json:"ok"`
}

// Component is one power consumer.
//
// DrawW and MaxW are pointers so utilization-only components can emit JSON
// null.  Measured and modeled components normally carry numeric values.
// activity_percent is the legacy design field (0..100); utilization_percent is
// the measured load/utilization when one is available.
type Component struct {
	ID                 string      `json:"id"`
	Label              string      `json:"label"`
	DrawW              *float64    `json:"draw_w"`
	MaxW               *float64    `json:"max_w"`
	ActivityPercent    int         `json:"activity_percent"`
	DrawKind           string      `json:"draw_kind"`
	DrawSource         string      `json:"draw_source"`
	UtilizationPercent *int        `json:"utilization_percent,omitempty"`
	UncertaintyPercent *int        `json:"uncertainty_percent,omitempty"`
	Model              *Model      `json:"model,omitempty"`
	TopProcess         *TopProcess `json:"top_process,omitempty"`
}

// Snapshot is the complete line-oriented contract.
type Snapshot struct {
	SchemaVersion int         `json:"schema_version"`
	Source        Source      `json:"source"`
	Components    []Component `json:"components"`
}

// Float64Ptr returns a pointer suitable for a component draw/max field.
func Float64Ptr(v float64) *float64 { return &v }

// IntPtr returns a pointer suitable for an optional integer field.
func IntPtr(v int) *int { return &v }

// Encode writes exactly one compact JSON object and a newline.  The object is
// marshaled completely before writing so an encoding failure cannot leave a
// partial JSON line on the writer.
func Encode(w io.Writer, s Snapshot) error {
	line, err := MarshalLine(s)
	if err != nil {
		return err
	}
	n, err := w.Write(line)
	if err != nil {
		return err
	}
	if n != len(line) {
		return io.ErrShortWrite
	}
	return nil
}

// MarshalLine returns one compact JSON object plus a trailing newline.
func MarshalLine(s Snapshot) ([]byte, error) {
	normalize(&s)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func normalize(s *Snapshot) {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = SchemaVersion
	}
	if s.Components == nil {
		s.Components = []Component{}
	}

	s.Source.TotalDrawW = sanitizeDrawValue(s.Source.TotalDrawW)
	s.Source.BatteryPercentage = clampInt(s.Source.BatteryPercentage, -1, 100)
	s.Source.PowerMode = normalizePowerMode(s.Source.PowerMode)
	if s.Source.BatteryStatus != "" {
		s.Source.BatteryStatus = normalizeBatteryStatus(s.Source.BatteryStatus)
	}
	switch s.Source.TotalDrawKind {
	case TotalKindMeasured, TotalKindModeled, TotalKindMixed, TotalKindNone:
	default:
		s.Source.TotalDrawKind = totalKind(s.Components)
	}

	for i := range s.Components {
		comp := &s.Components[i]
		comp.DrawW = sanitizeDrawPtr(comp.DrawW)
		comp.MaxW = sanitizeDrawPtr(comp.MaxW)
		comp.ActivityPercent = clampInt(comp.ActivityPercent, 0, 100)
		comp.UtilizationPercent = sanitizePercentPtr(comp.UtilizationPercent)
		comp.UncertaintyPercent = sanitizePercentPtr(comp.UncertaintyPercent)
		sanitizeModel(comp.Model)
		sanitizeTopProcess(comp.TopProcess)
	}
}

func sanitizeDrawValue(v float64) float64 {
	if !finite(v) {
		return 0
	}
	return clampFloat(v, 0, SafetyMaxDrawW)
}

func sanitizeDrawPtr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	if !finite(*p) {
		return nil
	}
	v := clampFloat(*p, 0, SafetyMaxDrawW)
	return &v
}

func sanitizePercentPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := clampInt(*p, 0, 100)
	return &v
}

func sanitizeTopProcess(p *TopProcess) {
	if p == nil {
		return
	}
	if !finite(p.Value) || p.Value < 0 {
		p.Value = 0
	} else if p.Metric == TopMetricCPU || p.Unit == "percent_of_machine" {
		p.Value = clampFloat(p.Value, 0, 100)
	}
	if p.CorePercent != nil {
		if !finite(*p.CorePercent) {
			p.CorePercent = nil
		} else {
			v := clampFloat(*p.CorePercent, 0, math.MaxFloat64)
			p.CorePercent = &v
		}
	}
	if p.WindowMS < 0 {
		p.WindowMS = 0
	}
}

func sanitizeModel(m *Model) {
	if m == nil || m.Assumptions == nil {
		return
	}
	for key, value := range m.Assumptions {
		m.Assumptions[key] = sanitizeAny(value)
	}
}

func sanitizeAny(value any) any {
	switch v := value.(type) {
	case float64:
		if !finite(v) {
			return 0.0
		}
		return v
	case float32:
		f := float64(v)
		if !finite(f) {
			return 0.0
		}
		return f
	case map[string]any:
		for key, child := range v {
			v[key] = sanitizeAny(child)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = sanitizeAny(child)
		}
		return v
	case []float64:
		for i, child := range v {
			if !finite(child) {
				v[i] = 0
			}
		}
		return v
	case []float32:
		for i, child := range v {
			if !finite(float64(child)) {
				v[i] = 0
			}
		}
		return v
	case map[string]float64:
		for key, child := range v {
			if !finite(child) {
				v[key] = 0
			}
		}
		return v
	case map[string]float32:
		for key, child := range v {
			if !finite(float64(child)) {
				v[key] = 0
			}
		}
		return v
	default:
		return value
	}
}

func normalizePowerMode(mode string) string {
	switch mode {
	case PowerModeAC, PowerModeBattery, PowerModeUnknown:
		return mode
	default:
		return PowerModeUnknown
	}
}

func normalizeBatteryStatus(status string) string {
	switch status {
	case BatteryCharging, BatteryDischarging, BatteryFull, BatteryNotCharging, BatteryUnknown:
		return status
	default:
		return BatteryUnknown
	}
}

func totalKind(components []Component) string {
	if len(components) == 0 {
		return TotalKindNone
	}
	allMeasured := true
	allModeled := true
	for _, comp := range components {
		if comp.DrawKind != DrawKindMeasured {
			allMeasured = false
		}
		if comp.DrawKind != DrawKindModeled {
			allModeled = false
		}
	}
	switch {
	case allMeasured:
		return TotalKindMeasured
	case allModeled:
		return TotalKindModeled
	default:
		return TotalKindMixed
	}
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
