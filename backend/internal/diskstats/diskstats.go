// Package diskstats estimates disk activity from /proc/diskstats.
//
// Exact disk wattage is not exposed by the kernel.  This package measures
// real per-device busy time (io_ticks, field 13) as the activity percentage
// and maps it onto a documented linear estimate between an idle floor and the
// design's 10 W full-activity budget.
//
// Only real whole block devices are counted: a candidate must exist under
// /sys/block/<name>, expose a "device" attribute and must not have a
// "partition" attribute.  Loop, zram, device-mapper, software-RAID, optical
// and similar virtual devices are excluded by name as well.  If no real
// device is present the disk component is omitted.
package diskstats

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
	"github.com/bjornramberg/omarchy-power-flow/backend/internal/units"
)

const (
	// DefaultMaxW is the design's assumed 10 W budget for a fully busy disk.
	DefaultMaxW = 10.0
	// IdleDrawW is the documented estimate for an idle physical disk.
	IdleDrawW = 0.5
)

var virtualPrefixes = []string{
	"loop", "zram", "dm-", "md", "ram", "sr", "fd", "nbd",
}

// Parse extracts io_ticks (milliseconds spent doing I/O) for every device in
// /proc/diskstats.  Device eligibility is decided by Meter against /sys/block,
// because only sysfs can reliably distinguish physical disks from virtual
// devices and partitions.  Malformed lines are skipped.
func Parse(data []byte) map[string]uint64 {
	out := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 13 {
			continue
		}
		name := fields[2]
		ticks, err := strconv.ParseUint(fields[12], 10, 64)
		if err != nil {
			continue
		}
		out[name] = ticks
	}
	return out
}

// Result is one disk sample.
type Result struct {
	ActivityPercent int
	DrawW           float64
	MaxW            float64
	OK              bool
}

// Meter tracks device io_ticks between samples.
type Meter struct {
	fs   *fsx.Root
	prev map[string]uint64
	last time.Time
}

// New returns a disk meter bound to a filesystem root.
func New(fs *fsx.Root) *Meter {
	return &Meter{fs: fs}
}

// Sample computes activity from the change in io_ticks since the previous
// call.  The first call only primes the counters.
func (m *Meter) Sample(now time.Time) Result {
	res := Result{MaxW: DefaultMaxW}
	if m == nil || m.fs == nil {
		return res
	}

	data, err := m.fs.ReadFile("proc", "diskstats")
	if err != nil {
		// Keep the previous sample so the next successful read still covers
		// the elapsed interval.
		return res
	}

	cur := make(map[string]uint64)
	for name, ticks := range Parse(data) {
		if m.realDevice(name) {
			cur[name] = ticks
		}
	}
	if len(cur) == 0 {
		m.prev = cur
		m.last = now
		return res
	}

	if m.last.IsZero() {
		m.prev = cur
		m.last = now
		return res
	}

	elapsed := now.Sub(m.last).Seconds()
	matched := 0
	var busyMS float64
	for name, ticks := range cur {
		old, ok := m.prev[name]
		if !ok {
			continue
		}
		matched++
		if ticks > old {
			busyMS += float64(ticks - old)
		}
	}
	m.prev = cur
	m.last = now

	if matched == 0 || elapsed <= 0 {
		return res
	}

	activity := int(math.Round(units.Clamp(busyMS/(elapsed*1000)*100, 0, 100)))
	draw := IdleDrawW + (DefaultMaxW-IdleDrawW)*float64(activity)/100
	res.ActivityPercent = activity
	res.DrawW = units.Round2(draw)
	res.OK = true
	return res
}

// realDevice reports whether name is a whole physical block device according
// to sysfs.  Partitions and virtual devices are rejected.
func (m *Meter) realDevice(name string) bool {
	if name == "" || virtualDevice(name) {
		return false
	}
	dir := m.fs.Sys("block", name)
	if _, err := os.Stat(filepath.Join(dir, "device")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "partition")); err == nil {
		return false
	}
	return true
}

func virtualDevice(name string) bool {
	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
