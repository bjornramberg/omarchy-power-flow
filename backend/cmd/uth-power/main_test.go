package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunOnceEmitsOneJSONLine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr,
		[]string{"--once", "--root", t.TempDir(), "--warmup", "1ms", "--nvidia", "off"})
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
		} `json:"source"`
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if payload.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", payload.SchemaVersion)
	}
	if payload.Source.Name == "" {
		t.Error("source.name is empty")
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

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), &stdout, &stderr, []string{"--help"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "Usage: uth-power") {
		t.Errorf("usage not printed: %q", stderr.String())
	}
}
