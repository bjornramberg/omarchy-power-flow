package cpustat

import (
	"os"
	"path/filepath"
	"testing"

	"uthpower/backend/internal/fsx"
)

func writeStat(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, "proc", "stat")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseAggregateCPU(t *testing.T) {
	total, busy, ok := Parse([]byte(
		"cpu  100 0 50 850 0 0 0 0 0 0\n" +
			"cpu0 50 0 25 425 0 0 0 0 0 0\n"))
	if !ok {
		t.Fatal("Parse returned !ok")
	}
	if total != 1000 || busy != 150 {
		t.Fatalf("total/busy = %d/%d, want 1000/150", total, busy)
	}
}

func TestParseIowaitIsIdle(t *testing.T) {
	total, busy, ok := Parse([]byte("cpu 100 0 100 700 100 0 0 0\n"))
	if !ok {
		t.Fatal("Parse returned !ok")
	}
	if total != 1000 || busy != 200 {
		t.Fatalf("total/busy = %d/%d, want 1000/200", total, busy)
	}

	total, busy, ok = Parse([]byte("cpu 0 0 0 100 0 0 0 0\n"))
	if !ok || total != 100 || busy != 0 {
		t.Fatalf("fully idle = %d/%d/%v, want 100/0/true", total, busy, ok)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	inputs := []string{
		"",
		"cpu",
		"cpu 1 2 3",
		"cpu0 100 0 50 850\n",
		"cpu a 0 50 850\n",
		"cpu 0 0 0 0\n",
	}
	for _, input := range inputs {
		if _, _, ok := Parse([]byte(input)); ok {
			t.Errorf("Parse(%q) returned ok", input)
		}
	}
}

func TestSampleDelta(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	writeStat(t, root, "cpu  100 0 50 850 0 0 0 0 0 0\ncpu0 1 2 3 4\n")

	m := New()
	if res := m.Sample(fs); res.OK {
		t.Fatalf("first sample must only prime: %+v", res)
	}

	writeStat(t, root, "cpu  300 0 100 1600 0 0 0 0 0 0\ncpu0 1 2 3 4\n")
	res := m.Sample(fs)
	if !res.OK {
		t.Fatal("expected CPU utilization")
	}
	if res.Percent != 25 {
		t.Fatalf("utilization = %d, want 25", res.Percent)
	}
}

func TestSampleResetAndMissing(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	writeStat(t, root, "cpu  100 0 50 850\n")
	m := New()
	m.Sample(fs)

	writeStat(t, root, "cpu  1 0 0 1\n")
	if res := m.Sample(fs); res.OK {
		t.Fatalf("counter reset produced data: %+v", res)
	}

	writeStat(t, root, "cpu  1 0 0 100\n")
	res := m.Sample(fs)
	if !res.OK || res.Percent != 0 {
		t.Fatalf("post-reset sample = %+v, want 0%%", res)
	}

	if res := New().Sample(fsx.New(t.TempDir())); res.OK {
		t.Fatalf("missing /proc/stat produced data: %+v", res)
	}
}
