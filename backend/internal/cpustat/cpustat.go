// Package cpustat derives CPU utilization from /proc/stat.
//
// /proc/stat exposes cumulative jiffy counters.  Utilization is the delta of
// busy jiffies over the delta of all jiffies between two samples; idle and
// iowait are both treated as idle.  The first sample only primes the counter.
package cpustat

import (
	"math"
	"strconv"
	"strings"

	"uthpower/backend/internal/fsx"
	"uthpower/backend/internal/units"
)

// Result is one utilization sample.
type Result struct {
	Percent int
	OK      bool
	// TotalDelta is the aggregate machine tick delta for this interval.  It is
	// used to compute a process's share of all machine CPU time.
	TotalDelta uint64
}

// Meter tracks the aggregate "cpu" line between samples.
type Meter struct {
	prevTotal uint64
	prevBusy  uint64
	hasPrev   bool
}

// New returns an unprimed CPU meter.
func New() *Meter {
	return &Meter{}
}

// Parse returns total and busy jiffies from the aggregate cpu line.  Only the
// first four fields are required; optional fields are added when valid.
func Parse(data []byte) (total, busy uint64, ok bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}

		values := make([]uint64, 0, 8)
		limit := len(fields) - 1
		if limit > 8 {
			limit = 8
		}
		for i := 1; i <= limit; i++ {
			v, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				if i <= 4 {
					return 0, 0, false
				}
				break
			}
			values = append(values, v)
		}
		if len(values) < 4 {
			return 0, 0, false
		}

		for _, v := range values {
			total += v
		}
		idle := values[3]
		iowait := uint64(0)
		if len(values) > 4 {
			iowait = values[4]
		}
		if total == 0 {
			return 0, 0, false
		}
		if idle+iowait >= total {
			busy = 0
		} else {
			busy = total - idle - iowait
		}
		return total, busy, true
	}
	return 0, 0, false
}

// Sample reads /proc/stat and returns utilization since the previous call.
func (m *Meter) Sample(fs *fsx.Root) Result {
	if m == nil || fs == nil {
		return Result{}
	}
	data, err := fs.ReadFile("proc", "stat")
	if err != nil {
		return Result{}
	}
	total, busy, ok := Parse(data)
	if !ok {
		return Result{}
	}
	if !m.hasPrev {
		m.prevTotal = total
		m.prevBusy = busy
		m.hasPrev = true
		return Result{}
	}
	if total < m.prevTotal || busy < m.prevBusy {
		// Counters reset (for example after suspend or in a container).
		m.prevTotal = total
		m.prevBusy = busy
		return Result{}
	}

	totalDelta := total - m.prevTotal
	busyDelta := busy - m.prevBusy
	m.prevTotal = total
	m.prevBusy = busy
	if totalDelta == 0 {
		return Result{}
	}
	if busyDelta > totalDelta {
		busyDelta = totalDelta
	}

	pct := int(math.Round(units.Clamp(float64(busyDelta)/float64(totalDelta)*100, 0, 100)))
	return Result{Percent: pct, OK: true, TotalDelta: totalDelta}
}
