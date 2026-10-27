// Package procmetrics reads process CPU ticks and RSS from /proc/<pid>/stat.
//
// One scan implementation serves both top-process metrics: each stat file is
// read once, yielding CPU ticks and RSS together.  CPU share is expressed as a
// percentage of all machine CPU time (using the caller's aggregate /proc/stat
// delta), not as power.  Scanning is bounded by a PID cap and a wall-clock
// budget; incomplete scans omit their metrics rather than claiming full
// coverage.
package procmetrics

import (
	"bytes"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
	"github.com/bjornramberg/omarchy-power-flow/backend/internal/units"
)

const (
	// DefaultMaxPIDs bounds a single scan.
	DefaultMaxPIDs = 1200
	// DefaultBudget bounds a single scan's wall time.
	DefaultBudget = 10 * time.Millisecond
	// DefaultClockTicks is Linux USER_HZ, used for core_percent.
	DefaultClockTicks = 100.0

	pfKthread = 0x00200000
)

// Metric vocabulary used by the collector.
const (
	MetricCPU = "cpu"
	MetricRSS = "rss"

	UnitPercentOfMachine = "percent_of_machine"
	UnitBytes            = "bytes"

	ScopeVisibleProcesses = "visible_processes"
	SourceProcStat        = "proc:stat"
)

// Options configures a Scanner.  Zero values select defaults.
type Options struct {
	MaxPIDs    int
	Budget     time.Duration
	PageSize   int64
	ClockTicks float64
}

// Metric is one top-process measurement.  Value is CPU percent-of-machine or
// RSS bytes depending on Metric/Unit.
type Metric struct {
	PID         int
	Name        string
	Metric      string
	Value       float64
	Unit        string
	CorePercent *float64
	WindowMS    int
	Scope       string
	Source      string
	OK          bool
}

// Result is one bounded scan.
type Result struct {
	CPU      *Metric
	RSS      *Metric
	Scanned  int
	Complete bool
	Primed   bool
	Duration time.Duration
}

type procState struct {
	startTime uint64
	ticks     uint64
}

// Scanner tracks process ticks between samples.
type Scanner struct {
	fs         *fsx.Root
	maxPIDs    int
	budget     time.Duration
	pageSize   int64
	clockTicks float64
	prev       map[int]procState
	last       time.Time
	primed     bool
}

// New returns a Scanner tied to a filesystem root.
func New(fs *fsx.Root, opts Options) *Scanner {
	if opts.MaxPIDs <= 0 {
		opts.MaxPIDs = DefaultMaxPIDs
	}
	if opts.Budget <= 0 {
		opts.Budget = DefaultBudget
	}
	if opts.PageSize <= 0 {
		opts.PageSize = int64(os.Getpagesize())
	}
	if opts.ClockTicks <= 0 {
		opts.ClockTicks = DefaultClockTicks
	}
	return &Scanner{
		fs:         fs,
		maxPIDs:    opts.MaxPIDs,
		budget:     opts.Budget,
		pageSize:   opts.PageSize,
		clockTicks: opts.ClockTicks,
		prev:       make(map[int]procState),
	}
}

// Sample scans /proc once.  machineDeltaTicks is the aggregate machine tick
// delta over the same interval (from cpustat).  The first scan only primes
// process state; both metrics are omitted until a later complete scan.
func (s *Scanner) Sample(now time.Time, machineDeltaTicks uint64) Result {
	res := Result{}
	if s == nil || s.fs == nil {
		return res
	}

	start := time.Now()
	entries, err := s.fs.ReadDir("proc")
	res.Duration = time.Since(start)
	if err != nil {
		s.prev = make(map[int]procState)
		s.last = time.Time{}
		s.primed = false
		return res
	}

	elapsed := 0.0
	if !s.last.IsZero() {
		elapsed = now.Sub(s.last).Seconds()
	}
	if elapsed < 0 {
		elapsed = 0
	}

	next := make(map[int]procState, len(entries))
	complete := true
	pidCount := 0
	scanned := 0

	var (
		cpuBest      *Metric
		cpuBestTicks uint64
		rssBest      *Metric
		rssBestBytes int64
	)

	for _, entry := range entries {
		if time.Since(start) > s.budget {
			complete = false
			break
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 2 {
			continue
		}
		if pidCount >= s.maxPIDs {
			complete = false
			break
		}
		pidCount++

		data, err := s.fs.ReadFile("proc", entry.Name(), "stat")
		if err != nil {
			continue
		}
		state, name, rss, ok := parseStat(data, s.pageSize)
		if !ok {
			continue
		}
		scanned++
		next[pid] = state

		if rss > 0 && (rssBest == nil || rss > rssBestBytes ||
			(rss == rssBestBytes && pid < rssBest.PID)) {
			rssBestBytes = rss
			rssBest = &Metric{
				PID: pid, Name: name, Metric: MetricRSS,
				Value: float64(rss), Unit: UnitBytes,
				WindowMS: 0, Scope: ScopeVisibleProcesses,
				Source: SourceProcStat, OK: true,
			}
		}

		if old, ok := s.prev[pid]; ok && old.startTime == state.startTime &&
			state.ticks >= old.ticks {
			delta := state.ticks - old.ticks
			if delta > 0 && (cpuBest == nil || delta > cpuBestTicks ||
				(delta == cpuBestTicks && pid < cpuBest.PID)) {
				cpuBestTicks = delta
				cpuBest = &Metric{
					PID: pid, Name: name, Metric: MetricCPU,
					Unit:     UnitPercentOfMachine,
					WindowMS: int(math.Round(elapsed * 1000)),
					Scope:    ScopeVisibleProcesses, Source: SourceProcStat, OK: true,
				}
			}
		}
	}

	res.Duration = time.Since(start)
	if time.Since(start) > s.budget {
		complete = false
	}

	wasPrimed := s.primed
	res.Scanned = scanned
	res.Complete = complete
	res.Primed = wasPrimed

	if complete && wasPrimed {
		if cpuBest != nil && machineDeltaTicks > 0 && elapsed > 0 {
			cpuBest.Value = units.Round(clampFloat(float64(cpuBestTicks)/float64(machineDeltaTicks)*100), 2)
			core := units.Round(float64(cpuBestTicks)/s.clockTicks/elapsed*100, 2)
			cpuBest.CorePercent = &core
			res.CPU = cpuBest
		}
		if rssBest != nil {
			res.RSS = rssBest
		}
	}

	s.prev = next
	s.last = now
	s.primed = true
	return res
}

// parseStat parses the fields needed from one /proc/<pid>/stat file.  The comm
// field is located by the last ')' so spaces, parentheses and newlines inside
// comm are preserved.
func parseStat(data []byte, pageSize int64) (procState, string, int64, bool) {
	open := bytes.IndexByte(data, '(')
	close := bytes.LastIndexByte(data, ')')
	if open < 0 || close <= open {
		return procState{}, "", 0, false
	}
	name := string(data[open+1 : close])
	fields := strings.Fields(string(data[close+1:]))
	if len(fields) < 22 {
		return procState{}, "", 0, false
	}

	state := fields[0]
	if state == "Z" || state == "X" {
		return procState{}, "", 0, false
	}

	flags, err := strconv.ParseUint(fields[6], 10, 64)
	if err != nil || flags&pfKthread != 0 {
		return procState{}, "", 0, false
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return procState{}, "", 0, false
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return procState{}, "", 0, false
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return procState{}, "", 0, false
	}
	rssPages, err := strconv.ParseInt(fields[21], 10, 64)
	if err != nil {
		return procState{}, "", 0, false
	}
	rss := int64(0)
	if rssPages > 0 {
		rss = rssPages * pageSize
	}

	return procState{startTime: startTime, ticks: utime + stime}, name, rss, true
}

func clampFloat(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
