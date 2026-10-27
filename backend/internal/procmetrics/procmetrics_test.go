package procmetrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statLine(pid int, comm, state string, flags uint64, utime, stime, starttime uint64, rss int64) string {
	fields := []string{
		state, "1", "1", "1", "0", "-1", fmt.Sprint(flags),
		"0", "0", "0", "0", fmt.Sprint(utime), fmt.Sprint(stime),
		"0", "0", "20", "0", "1", "0", fmt.Sprint(starttime),
		"1000", fmt.Sprint(rss), "0",
	}
	return fmt.Sprintf("%d (%s) %s\n", pid, comm, strings.Join(fields, " "))
}

func putProc(t *testing.T, root string, pid int, content string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "proc", fmt.Sprint(pid), "stat"), content)
}

func testScanner(root string, opts Options) *Scanner {
	if opts.MaxPIDs == 0 {
		opts.MaxPIDs = 1200
	}
	if opts.Budget == 0 {
		opts.Budget = time.Second
	}
	if opts.PageSize == 0 {
		opts.PageSize = 4096
	}
	if opts.ClockTicks == 0 {
		opts.ClockTicks = 100
	}
	return New(fsx.New(root), opts)
}

func TestParseStatCommAndFields(t *testing.T) {
	comm := "na)me with space\nand newline"
	line := statLine(123, comm, "S", 0, 11, 7, 900, 12)
	state, name, rss, ok := parseStat([]byte(line), 4096)
	if !ok {
		t.Fatal("parseStat returned false")
	}
	if name != comm {
		t.Fatalf("name = %q, want %q", name, comm)
	}
	if state.ticks != 18 || state.startTime != 900 {
		t.Fatalf("state = %+v, want ticks 18/start 900", state)
	}
	if rss != 12*4096 {
		t.Fatalf("rss = %d, want %d", rss, 12*4096)
	}
}

func TestScanCPUAndRSSInOnePass(t *testing.T) {
	root := t.TempDir()
	putProc(t, root, 10, statLine(10, "sleeper", "S", 0, 0, 0, 100, 20))
	putProc(t, root, 11, statLine(11, "busy", "S", 0, 0, 0, 100, 10))

	s := testScanner(root, Options{})
	t0 := time.Unix(1000, 0)
	first := s.Sample(t0, 1000)
	if first.CPU != nil || first.RSS != nil {
		t.Fatalf("unprimed scan reported metrics: %+v", first)
	}
	if first.Primed {
		t.Fatal("first scan reported Primed")
	}

	putProc(t, root, 10, statLine(10, "sleeper", "S", 0, 0, 0, 100, 20))
	putProc(t, root, 11, statLine(11, "busy", "S", 0, 60, 40, 100, 10))
	res := s.Sample(t0.Add(time.Second), 1000)
	if !res.Complete {
		t.Fatalf("scan incomplete: %+v", res)
	}
	if res.CPU == nil || res.RSS == nil {
		t.Fatalf("expected CPU and RSS metrics: %+v", res)
	}
	if res.CPU.PID != 11 || res.CPU.Metric != MetricCPU {
		t.Fatalf("CPU = %+v, want pid 11", res.CPU)
	}
	if res.CPU.Value != 10 {
		t.Errorf("CPU share = %v, want 10", res.CPU.Value)
	}
	if res.CPU.CorePercent == nil || *res.CPU.CorePercent != 100 {
		t.Errorf("core percent = %v, want 100", res.CPU.CorePercent)
	}
	if res.CPU.WindowMS != 1000 || res.CPU.Scope != ScopeVisibleProcesses || res.CPU.Source != SourceProcStat {
		t.Errorf("CPU metadata = %+v", res.CPU)
	}
	if res.RSS.PID != 10 || res.RSS.Value != 20*4096 || res.RSS.Unit != UnitBytes {
		t.Errorf("RSS = %+v, want pid 10, 20 pages", res.RSS)
	}
}

func TestMultiThreadCPUCanExceedOneCore(t *testing.T) {
	root := t.TempDir()
	putProc(t, root, 20, statLine(20, "threaded", "S", 0, 0, 0, 100, 1))
	s := testScanner(root, Options{})
	t0 := time.Unix(1000, 0)
	s.Sample(t0, 1000)
	putProc(t, root, 20, statLine(20, "threaded", "S", 0, 120, 80, 100, 1))
	res := s.Sample(t0.Add(time.Second), 1000)
	if res.CPU == nil {
		t.Fatalf("missing CPU metric: %+v", res)
	}
	if res.CPU.Value != 20 {
		t.Errorf("share = %v, want 20", res.CPU.Value)
	}
	if res.CPU.CorePercent == nil || *res.CPU.CorePercent != 200 {
		t.Errorf("core percent = %v, want 200", res.CPU.CorePercent)
	}
}

func TestPIDReuseViaStartTime(t *testing.T) {
	root := t.TempDir()
	s := testScanner(root, Options{})
	t0 := time.Unix(1000, 0)
	putProc(t, root, 30, statLine(30, "old", "S", 0, 10, 0, 100, 1))
	s.Sample(t0, 1000)

	// Same PID, new starttime: this is a different process, not a delta.
	putProc(t, root, 30, statLine(30, "new", "S", 0, 20, 0, 200, 1))
	res := s.Sample(t0.Add(time.Second), 1000)
	if res.CPU != nil {
		t.Fatalf("PID reuse produced a CPU delta: %+v", res.CPU)
	}

	putProc(t, root, 30, statLine(30, "new", "S", 0, 30, 0, 200, 1))
	res = s.Sample(t0.Add(2*time.Second), 1000)
	if res.CPU == nil || res.CPU.PID != 30 || res.CPU.Value != 1 {
		t.Fatalf("post-reuse CPU = %+v, want pid 30 at 1%%", res.CPU)
	}
}

func TestSleepingProcessHasNoCPUButHasRSS(t *testing.T) {
	root := t.TempDir()
	putProc(t, root, 40, statLine(40, "sleep", "S", 0, 10, 5, 100, 25))
	s := testScanner(root, Options{})
	t0 := time.Unix(1000, 0)
	s.Sample(t0, 1000)
	res := s.Sample(t0.Add(time.Second), 1000)
	if res.CPU != nil {
		t.Fatalf("sleeping process produced CPU: %+v", res.CPU)
	}
	if res.RSS == nil || res.RSS.PID != 40 {
		t.Fatalf("sleeping process RSS missing: %+v", res.RSS)
	}
}

func TestFilterZombieKernelThreadAndLowPIDs(t *testing.T) {
	root := t.TempDir()
	putProc(t, root, 1, statLine(1, "init", "S", 0, 100, 100, 1, 100))
	putProc(t, root, 2, statLine(2, "kthreadd", "S", 0, 100, 100, 1, 100))
	putProc(t, root, 10, statLine(10, "zombie", "Z", 0, 100, 100, 100, 100))
	putProc(t, root, 11, statLine(11, "kernel", "S", pfKthread, 100, 100, 100, 100))
	putProc(t, root, 12, statLine(12, "normal", "S", 0, 0, 0, 100, 40))

	s := testScanner(root, Options{})
	t0 := time.Unix(1000, 0)
	s.Sample(t0, 1000)
	putProc(t, root, 12, statLine(12, "normal", "S", 0, 1, 0, 100, 40))
	res := s.Sample(t0.Add(time.Second), 1000)
	if res.RSS == nil || res.RSS.PID != 12 {
		t.Fatalf("RSS = %+v, want pid 12", res.RSS)
	}
	if res.CPU == nil || res.CPU.PID != 12 {
		t.Fatalf("CPU = %+v, want pid 12", res.CPU)
	}
}

func TestProcessChurnDoesNotSuppressTopProcess(t *testing.T) {
	root := t.TempDir()
	putProc(t, root, 60, statLine(60, "alive", "S", 0, 0, 0, 100, 5))
	putProc(t, root, 61, statLine(61, "exiting", "S", 0, 0, 0, 100, 5))
	s := testScanner(root, Options{})
	t0 := time.Unix(1000, 0)
	s.Sample(t0, 1000)

	// The second process exits between readdir and stat read.
	if err := os.Remove(filepath.Join(root, "proc", "61", "stat")); err != nil {
		t.Fatal(err)
	}
	putProc(t, root, 60, statLine(60, "alive", "S", 0, 50, 0, 100, 5))
	res := s.Sample(t0.Add(time.Second), 1000)
	if !res.Complete {
		t.Fatalf("normal churn made scan incomplete: %+v", res)
	}
	if res.CPU == nil || res.CPU.PID != 60 || res.CPU.Scope != ScopeVisibleProcesses {
		t.Fatalf("top process suppressed by churn: %+v", res.CPU)
	}
	if res.RSS == nil || res.RSS.PID != 60 {
		t.Fatalf("RSS suppressed by churn: %+v", res.RSS)
	}
}

func TestMissingProcRoot(t *testing.T) {
	s := testScanner(t.TempDir(), Options{})
	res := s.Sample(time.Unix(1000, 0), 1000)
	if res.Complete || res.CPU != nil || res.RSS != nil {
		t.Fatalf("missing /proc produced data: %+v", res)
	}
}

func TestBudgetAndPIDCapOmitMetrics(t *testing.T) {
	root := t.TempDir()
	putProc(t, root, 50, statLine(50, "one", "S", 0, 10, 0, 100, 1))
	putProc(t, root, 51, statLine(51, "two", "S", 0, 10, 0, 100, 1))

	budget := testScanner(root, Options{Budget: time.Nanosecond})
	t0 := time.Unix(1000, 0)
	budget.Sample(t0, 1000)
	res := budget.Sample(t0.Add(time.Second), 1000)
	if res.Complete || res.CPU != nil || res.RSS != nil {
		t.Fatalf("budget-exceeded scan produced data: %+v", res)
	}

	capped := testScanner(root, Options{MaxPIDs: 1})
	capped.Sample(t0, 1000)
	res = capped.Sample(t0.Add(time.Second), 1000)
	if res.Complete || res.CPU != nil || res.RSS != nil {
		t.Fatalf("PID-capped scan produced data: %+v", res)
	}
}

func BenchmarkHostScan(b *testing.B) {
	s := New(fsx.New("/"), Options{Budget: 100 * time.Millisecond})
	s.Sample(time.Now(), 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := s.Sample(time.Now(), 1000)
		if !res.Complete {
			b.Fatalf("host scan exceeded benchmark budget: %+v", res)
		}
	}
}
