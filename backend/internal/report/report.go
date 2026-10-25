// Package report defines the schema-v2 JSON contract between the collector
// and the Quickshell frontend.
//
// The field order below is deliberate: encoding/json emits struct fields in
// declaration order, so the contract is deterministic.  The output is one
// compact JSON object followed by a newline; no pretty printing.
package report

import (
	"bytes"
	"encoding/json"
	"io"
)

// SchemaVersion is the current NDJSON schema version.
const SchemaVersion = 2

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
// input-power measurement; power-supply state only selects Name and
// BatteryPercentage.
type Source struct {
	Name              string  `json:"name"`
	TotalDrawW        float64 `json:"total_draw_w"`
	BatteryPercentage int     `json:"battery_percentage"`
	TotalDrawKind     string  `json:"total_draw_kind"`
}

// Model describes a watt estimate.  Assumptions is a small, stable set of
// model inputs; encoding/json sorts map keys, keeping output deterministic.
type Model struct {
	ID          string         `json:"id"`
	Assumptions map[string]any `json:"assumptions"`
}

// Component is one power consumer.
//
// DrawW and MaxW are pointers so utilization-only components can emit JSON
// null.  Measured and modeled components always carry numeric values.
// activity_percent is the legacy design field (0..100); utilization_percent is
// the measured load/utilization when one is available.
type Component struct {
	ID                 string   `json:"id"`
	Label              string   `json:"label"`
	DrawW              *float64 `json:"draw_w"`
	MaxW               *float64 `json:"max_w"`
	ActivityPercent    int      `json:"activity_percent"`
	DrawKind           string   `json:"draw_kind"`
	DrawSource         string   `json:"draw_source"`
	UtilizationPercent *int     `json:"utilization_percent,omitempty"`
	UncertaintyPercent *int     `json:"uncertainty_percent,omitempty"`
	Model              *Model   `json:"model,omitempty"`
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

// Encode writes exactly one compact JSON object and a newline.
func Encode(w io.Writer, s Snapshot) error {
	normalize(&s)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(s)
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
}
