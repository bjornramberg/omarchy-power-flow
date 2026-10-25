package report

import (
	"bytes"
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
	want := `{"schema_version":2,"source":{"name":"AC","total_draw_w":138.2,"battery_percentage":87,"total_draw_kind":"mixed"},"components":[{"id":"cpu","label":"CPU","draw_w":45.5,"max_w":150,"activity_percent":40,"draw_kind":"measured","draw_source":"powercap:rapl","utilization_percent":40},{"id":"gpu","label":"GPU","draw_w":85,"max_w":300,"activity_percent":80,"draw_kind":"measured","draw_source":"nvidia-smi","utilization_percent":80},{"id":"ram","label":"RAM","draw_w":5.2,"max_w":15,"activity_percent":30,"draw_kind":"modeled","draw_source":"model:ram-capacity/1","utilization_percent":60,"uncertainty_percent":50,"model":{"id":"ram-capacity/1","assumptions":{"active_w_per_gib":0.35,"idle_w_per_gib":0.15,"pressure_available":true}}},{"id":"disk","label":"DISK","draw_w":2.5,"max_w":10,"activity_percent":15,"draw_kind":"modeled","draw_source":"model:disk-busy/1","uncertainty_percent":50,"model":{"id":"disk-busy/1","assumptions":{"full_w":10,"idle_w":0.5}}}]}` + "\n"
	if string(got) != want {
		t.Fatalf("unexpected JSON:\n got: %s\nwant: %s", got, want)
	}
	if strings.Count(string(got), "\n") != 1 || !strings.HasSuffix(string(got), "\n") {
		t.Fatalf("expected exactly one trailing newline, got %q", got)
	}
}

func TestUtilizationOnlyNulls(t *testing.T) {
	got, err := MarshalLine(Snapshot{
		Source: Source{
			Name: "PSU / Battery", BatteryPercentage: BatteryUnavailable,
			TotalDrawKind: TotalKindMixed,
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
		Source: Source{Name: "PSU / Battery", TotalDrawW: 0, BatteryPercentage: BatteryUnavailable, TotalDrawKind: TotalKindNone},
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := `{"schema_version":2,"source":{"name":"PSU / Battery","total_draw_w":0,"battery_percentage":-1,"total_draw_kind":"none"},"components":[]}` + "\n"
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
		`"battery_percentage":`, `"total_draw_kind":`, `"components":`,
		`"id":`, `"label":`, `"draw_w":`, `"max_w":`, `"activity_percent":`,
		`"draw_kind":`, `"draw_source":`, `"utilization_percent":`,
		`"uncertainty_percent":`, `"model":`, `"assumptions":`,
	}
	for _, key := range keys {
		if !strings.Contains(a.String(), key) {
			t.Errorf("missing key %s in %s", key, a.String())
		}
	}
}
