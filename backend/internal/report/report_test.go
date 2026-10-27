package report

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func sampleSnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: SchemaVersion,
		Source: Source{
			Name:              "AC",
			TotalDrawW:        138.2,
			BatteryPercentage: 87,
			TotalDrawKind:     TotalKindMixed,
			PowerMode:         PowerModeAC,
			BatteryStatus:     BatteryCharging,
		},
		Components: []Component{
			{
				ID: IDCPU, Label: "CPU",
				DrawW: Float64Ptr(45.5), MaxW: Float64Ptr(150),
				ActivityPercent: 40, DrawKind: DrawKindMeasured,
				DrawSource: "powercap:rapl", UtilizationPercent: IntPtr(40),
			},
			{
				ID: IDGPU, Label: "GPU",
				DrawW: Float64Ptr(85), MaxW: Float64Ptr(300),
				ActivityPercent: 80, DrawKind: DrawKindMeasured,
				DrawSource: "nvidia-smi", UtilizationPercent: IntPtr(80),
			},
			{
				ID: IDRAM, Label: "RAM",
				DrawW: Float64Ptr(5.2), MaxW: Float64Ptr(15),
				ActivityPercent: 30, DrawKind: DrawKindModeled,
				DrawSource: "model:ram-capacity/1", UtilizationPercent: IntPtr(60),
				UncertaintyPercent: IntPtr(50),
				Model: &Model{ID: "ram-capacity/1", Assumptions: map[string]any{
					"active_w_per_gib":   0.35,
					"idle_w_per_gib":     0.15,
					"pressure_available": true,
				}},
			},
			{
				ID: IDDisk, Label: "DISK",
				DrawW: Float64Ptr(2.5), MaxW: Float64Ptr(10),
				ActivityPercent: 15, DrawKind: DrawKindModeled,
				DrawSource:         "model:disk-busy/1",
				UncertaintyPercent: IntPtr(50),
				Model: &Model{ID: "disk-busy/1", Assumptions: map[string]any{
					"full_w": 10,
					"idle_w": 0.5,
				}},
			},
		},
	}
}

func TestMarshalLineExactContract(t *testing.T) {
	got, err := MarshalLine(sampleSnapshot())
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}
	want := `{"schema_version":2,"source":{"name":"AC","total_draw_w":138.2,"battery_percentage":87,"total_draw_kind":"mixed","power_mode":"ac","battery_status":"charging"},"components":[{"id":"cpu","label":"CPU","draw_w":45.5,"max_w":150,"activity_percent":40,"draw_kind":"measured","draw_source":"powercap:rapl","utilization_percent":40},{"id":"gpu","label":"GPU","draw_w":85,"max_w":300,"activity_percent":80,"draw_kind":"measured","draw_source":"nvidia-smi","utilization_percent":80},{"id":"ram","label":"RAM","draw_w":5.2,"max_w":15,"activity_percent":30,"draw_kind":"modeled","draw_source":"model:ram-capacity/1","utilization_percent":60,"uncertainty_percent":50,"model":{"id":"ram-capacity/1","assumptions":{"active_w_per_gib":0.35,"idle_w_per_gib":0.15,"pressure_available":true}}},{"id":"disk","label":"DISK","draw_w":2.5,"max_w":10,"activity_percent":15,"draw_kind":"modeled","draw_source":"model:disk-busy/1","uncertainty_percent":50,"model":{"id":"disk-busy/1","assumptions":{"full_w":10,"idle_w":0.5}}}]}` + "\n"
	if string(got) != want {
		t.Fatalf("unexpected JSON:\n got: %s\nwant: %s", got, want)
	}
	if strings.Count(string(got), "\n") != 1 || !strings.HasSuffix(string(got), "\n") {
		t.Fatalf("expected exactly one trailing newline, got %q", got)
	}
}

func TestTopProcessJSON(t *testing.T) {
	got, err := MarshalLine(Snapshot{
		Source: Source{
			Name: "Battery", BatteryPercentage: 42,
			TotalDrawKind: TotalKindMixed, PowerMode: PowerModeBattery,
			BatteryStatus: BatteryDischarging,
		},
		Components: []Component{{
			ID: IDCPU, Label: "CPU",
			DrawW: Float64Ptr(12), MaxW: Float64Ptr(142),
			ActivityPercent: 20, DrawKind: DrawKindMeasured,
			DrawSource: "powercap:rapl", UtilizationPercent: IntPtr(20),
			TopProcess: &TopProcess{
				PID: 4242, Name: "firefox", Metric: TopMetricCPU,
				Value: 12.5, Unit: "percent_of_machine",
				CorePercent: Float64Ptr(50), WindowMS: 1000,
				Scope: "visible_processes", Source: "proc:stat", OK: true,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if !strings.Contains(out, `"power_mode":"battery","battery_status":"discharging"`) {
		t.Errorf("source state missing: %s", out)
	}
	wantTop := `"top_process":{"pid":4242,"name":"firefox","metric":"cpu","value":12.5,"unit":"percent_of_machine","core_percent":50,"window_ms":1000,"scope":"visible_processes","source":"proc:stat","ok":true}`
	if !strings.Contains(out, wantTop) {
		t.Errorf("top_process missing/unexpected:\n%s\nwant substring: %s", out, wantTop)
	}
}

func TestUtilizationOnlyNulls(t *testing.T) {
	got, err := MarshalLine(Snapshot{
		Source: Source{
			Name: "Unknown", BatteryPercentage: BatteryUnavailable,
			TotalDrawKind: TotalKindMixed, PowerMode: PowerModeUnknown,
		},
		Components: []Component{{
			ID: IDCPU, Label: "CPU", ActivityPercent: 42,
			DrawKind: DrawKindUtilization, DrawSource: "proc:stat",
			UtilizationPercent: IntPtr(42),
		}},
	})
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, `"draw_w":null,"max_w":null`) {
		t.Errorf("utilization-only draw/max not null: %s", out)
	}
	if strings.Contains(out, "uncertainty_percent") || strings.Contains(out, `"model"`) {
		t.Errorf("utilization-only must not carry uncertainty/model: %s", out)
	}
	if !strings.HasPrefix(out, `{"schema_version":2,`) {
		t.Errorf("missing schema version: %s", out)
	}
}

func TestEncodeNilComponentsIsArray(t *testing.T) {
	var buf bytes.Buffer
	err := Encode(&buf, Snapshot{
		Source: Source{
			Name: "Unknown", TotalDrawW: 0, BatteryPercentage: BatteryUnavailable,
			TotalDrawKind: TotalKindNone, PowerMode: PowerModeUnknown,
		},
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := `{"schema_version":2,"source":{"name":"Unknown","total_draw_w":0,"battery_percentage":-1,"total_draw_kind":"none","power_mode":"unknown"},"components":[]}` + "\n"
	if buf.String() != want {
		t.Fatalf("got %s, want %s", buf.String(), want)
	}
}

func TestEncodeDeterministicFieldOrder(t *testing.T) {
	var a, b bytes.Buffer
	if err := Encode(&a, sampleSnapshot()); err != nil {
		t.Fatal(err)
	}
	if err := Encode(&b, sampleSnapshot()); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatalf("output is not deterministic:\n%s\n%s", a.String(), b.String())
	}
	keys := []string{
		`"schema_version":`, `"source":`, `"name":`, `"total_draw_w":`,
		`"battery_percentage":`, `"total_draw_kind":`, `"power_mode":`,
		`"battery_status":`, `"components":`, `"id":`, `"label":`,
		`"draw_w":`, `"max_w":`, `"activity_percent":`, `"draw_kind":`,
		`"draw_source":`, `"utilization_percent":`, `"uncertainty_percent":`,
		`"model":`, `"assumptions":`,
	}
	for _, key := range keys {
		if !strings.Contains(a.String(), key) {
			t.Errorf("missing key %s in %s", key, a.String())
		}
	}
}

func TestNormalizeNonFiniteAndBounds(t *testing.T) {
	snap := Snapshot{
		Source: Source{
			Name:              "Unknown",
			TotalDrawW:        math.NaN(),
			BatteryPercentage: 200,
			TotalDrawKind:     "garbage",
			PowerMode:         "garbage",
			BatteryStatus:     "garbage",
		},
		Components: []Component{
			{
				ID: IDCPU, Label: "CPU",
				DrawW: Float64Ptr(math.Inf(1)), MaxW: Float64Ptr(math.NaN()),
				ActivityPercent: 150, DrawKind: DrawKindMeasured,
				DrawSource: "test", UtilizationPercent: IntPtr(-5),
				UncertaintyPercent: IntPtr(150),
				Model: &Model{ID: "test", Assumptions: map[string]any{
					"bad":    math.NaN(),
					"nested": map[string]any{"inf": math.Inf(1)},
					"slice":  []float64{math.Inf(-1), 1.5},
				}},
				TopProcess: &TopProcess{
					PID: 1, Name: "x", Metric: TopMetricCPU,
					Value: 500, Unit: "percent_of_machine",
					CorePercent: Float64Ptr(math.NaN()), WindowMS: -1,
					Scope: "visible_processes", Source: "proc:stat", OK: true,
				},
			},
			{
				ID: IDRAM, Label: "RAM",
				DrawW: Float64Ptr(1e9), MaxW: Float64Ptr(-5),
				ActivityPercent: -1, DrawKind: DrawKindMeasured,
				DrawSource: "test",
			},
		},
	}

	line, err := MarshalLine(snap)
	if err != nil {
		t.Fatalf("MarshalLine failed: %v", err)
	}
	if !json.Valid(line) {
		t.Fatalf("invalid JSON: %s", line)
	}
	if strings.Count(string(line), "\n") != 1 {
		t.Fatalf("expected one line, got %q", line)
	}

	// normalize is applied to a copy inside MarshalLine; normalize explicitly
	// to inspect the sanitized values.
	normalize(&snap)
	if snap.Source.TotalDrawW != 0 {
		t.Errorf("total = %v, want 0", snap.Source.TotalDrawW)
	}
	if snap.Source.BatteryPercentage != 100 {
		t.Errorf("battery = %d, want 100", snap.Source.BatteryPercentage)
	}
	if snap.Source.PowerMode != PowerModeUnknown || snap.Source.BatteryStatus != BatteryUnknown {
		t.Errorf("source state = %q/%q, want unknown/unknown", snap.Source.PowerMode, snap.Source.BatteryStatus)
	}
	if snap.Source.TotalDrawKind != TotalKindMeasured {
		t.Errorf("total kind = %q, want measured", snap.Source.TotalDrawKind)
	}

	cpu := snap.Components[0]
	if cpu.DrawW != nil || cpu.MaxW != nil {
		t.Errorf("non-finite draw/max not nulled: %+v", cpu)
	}
	if cpu.ActivityPercent != 100 || *cpu.UtilizationPercent != 0 || *cpu.UncertaintyPercent != 100 {
		t.Errorf("percent clamping failed: %+v", cpu)
	}
	if cpu.TopProcess.Value != 100 || cpu.TopProcess.CorePercent != nil || cpu.TopProcess.WindowMS != 0 {
		t.Errorf("top-process sanitize failed: %+v", cpu.TopProcess)
	}
	if got := cpu.Model.Assumptions["bad"]; got != 0.0 {
		t.Errorf("NaN assumption = %v, want 0", got)
	}
	nested := cpu.Model.Assumptions["nested"].(map[string]any)
	if nested["inf"] != 0.0 {
		t.Errorf("Inf assumption = %v, want 0", nested["inf"])
	}
	slice := cpu.Model.Assumptions["slice"].([]float64)
	if slice[0] != 0 || slice[1] != 1.5 {
		t.Errorf("slice assumptions = %v, want [0 1.5]", slice)
	}

	ram := snap.Components[1]
	if ram.DrawW == nil || *ram.DrawW != SafetyMaxDrawW {
		t.Errorf("absurd draw = %v, want %v", ram.DrawW, SafetyMaxDrawW)
	}
	if ram.MaxW == nil || *ram.MaxW != 0 || ram.ActivityPercent != 0 {
		t.Errorf("negative values not clamped: %+v", ram)
	}
}
