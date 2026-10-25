// Package nvidia reads NVIDIA GPU power with nvidia-smi.
//
// nvidia-smi reports watts directly (with --format=csv,noheader,nounits), so
// no unit conversion is needed.  A short timeout prevents a wedged driver
// from stalling the collector.
package nvidia

import (
	"context"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Runner executes a command and returns stdout.  Tests inject fakes.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Result is one aggregated nvidia-smi sample.
type Result struct {
	DrawW       float64
	MaxW        float64 // sum of reported power limits
	Count       int     // GPUs with a valid power.draw
	Capped      int     // GPUs with both power.draw and power.limit
	UtilPercent float64 // mean utilization over GPUs that reported it
	HasUtil     bool
	OK          bool
}

// Reader queries nvidia-smi.
type Reader struct {
	Path    string
	Runner  Runner
	Timeout time.Duration
}

// Available reports whether nvidia-smi can be found on PATH.
func Available(path string) bool {
	if path == "" {
		path = "nvidia-smi"
	}
	_, err := exec.LookPath(path)
	return err == nil
}

// ExecRunner is the production Runner.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Sample runs nvidia-smi and parses its output only when the process exits
// successfully.  Any error yields an empty Result; callers omit the GPU
// component in that case.  This is important because nvidia-smi may print
// diagnostics to stdout on failure.
func (r *Reader) Sample(ctx context.Context) Result {
	if r == nil {
		return Result{}
	}
	path := r.Path
	if path == "" {
		path = "nvidia-smi"
	}
	runner := r.Runner
	if runner == nil {
		runner = ExecRunner
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := runner(ctx, path,
		"--query-gpu=power.draw,power.limit,utilization.gpu",
		"--format=csv,noheader,nounits")
	if err != nil {
		return Result{}
	}
	return Parse(out)
}

// Parse converts nvidia-smi CSV output into a Result.  Lines with no usable
// power.draw (for example "[N/A]") are ignored.
func Parse(out []byte) Result {
	var res Result
	utilCount := 0
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 1 {
			continue
		}
		drawW, ok := parseNumber(fields[0])
		if !ok {
			continue
		}
		res.DrawW += drawW
		res.Count++
		res.OK = true

		if len(fields) > 1 {
			if limitW, ok := parseNumber(fields[1]); ok {
				res.MaxW += limitW
				res.Capped++
			}
		}
		if len(fields) > 2 {
			if util, ok := parseNumber(fields[2]); ok {
				res.UtilPercent += util
				utilCount++
			}
		}
	}
	if utilCount > 0 {
		res.UtilPercent /= float64(utilCount)
		res.HasUtil = true
	}
	if res.Capped > res.Count {
		res.Capped = res.Count
	}
	return res
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "[") {
		return 0, false
	}
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(s, "W"), "%"))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}
