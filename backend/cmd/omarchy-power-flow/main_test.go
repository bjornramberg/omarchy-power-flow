package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/collector"
	"github.com/bjornramberg/omarchy-power-flow/backend/internal/report"
)

func TestRunOnceEmitsOneJSONLine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr,
		[]string{"--once", "--root", t.TempDir(), "--warmup", "1ms", "--nvidia", "off", "--processes", "off"})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected exactly one line, got %q", out)
	}

	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Source        struct {
			Name              string  `json:"name"`
			TotalDrawW        float64 `json:"total_draw_w"`
			BatteryPercentage int     `json:"battery_percentage"`
			TotalDrawKind     string  `json:"total_draw_kind"`
			PowerMode         string  `json:"power_mode"`
			BatteryStatus     string  `json:"battery_status"`
		} `json:"source"`
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if payload.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", payload.SchemaVersion)
	}
	if payload.Source.Name != "Unknown" {
		t.Errorf("source.name = %q, want Unknown", payload.Source.Name)
	}
	if payload.Source.PowerMode != "unknown" {
		t.Errorf("power_mode = %q, want unknown", payload.Source.PowerMode)
	}
	if payload.Source.BatteryStatus != "" {
		t.Errorf("battery_status = %q, want omitted", payload.Source.BatteryStatus)
	}
	if payload.Source.BatteryPercentage != -1 {
		t.Errorf("battery_percentage = %d, want -1", payload.Source.BatteryPercentage)
	}
	if payload.Source.TotalDrawKind != "none" {
		t.Errorf("total_draw_kind = %q, want none", payload.Source.TotalDrawKind)
	}
	if payload.Components == nil || len(payload.Components) != 0 {
		t.Errorf("components = %v, want empty array", payload.Components)
	}
}

func TestRunRejectsInvalidInterval(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr,
		[]string{"--once", "--interval", "0s", "--root", t.TempDir()})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "interval") {
		t.Errorf("stderr = %q, want interval error", stderr.String())
	}
}

func TestRunRejectsInvalidEstimates(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr,
		[]string{"--once", "--estimates", "sometimes", "--nvidia", "off", "--root", t.TempDir()})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "estimates") {
		t.Errorf("stderr = %q, want estimates error", stderr.String())
	}
}

func TestRunRejectsInvalidProcesses(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr,
		[]string{"--once", "--processes", "sometimes", "--nvidia", "off", "--root", t.TempDir()})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "processes") {
		t.Errorf("stderr = %q, want processes error", stderr.String())
	}
}

type panicSnapshot struct {
	value any
}

func (p panicSnapshot) Sample() report.Snapshot {
	panic(p.value)
}

func TestRunRecoversPanic(t *testing.T) {
	original := newSnapshotSource
	t.Cleanup(func() { newSnapshotSource = original })
	newSnapshotSource = func(collector.Options) snapshotSource {
		return panicSnapshot{value: strings.Repeat("x", 10000)}
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr,
		[]string{"--once", "--warmup", "0", "--nvidia", "off", "--processes", "off", "--root", t.TempDir()})
	if code != 70 {
		t.Fatalf("exit code = %d, want 70", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on panic, got %q", stdout.String())
	}
	errText := stderr.String()
	if !strings.Contains(errText, "omarchy-power-flow: panic:") || !strings.Contains(errText, "omarchy-power-flow: stack:") {
		t.Fatalf("stderr missing panic diagnostics: %q", errText)
	}
	if len(errText) > 7000 {
		t.Fatalf("panic diagnostic too large: %d bytes", len(errText))
	}
	if strings.Contains(errText, strings.Repeat("x", 2000)) {
		t.Fatal("panic value was not bounded")
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), &stdout, &stderr, []string{"--help"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "Usage: omarchy-power-flow") {
		t.Errorf("usage not printed: %q", stderr.String())
	}
}
