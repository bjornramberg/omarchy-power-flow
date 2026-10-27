// Package nvidia reads NVIDIA GPU power with nvidia-smi.
//
// nvidia-smi reports watts directly (with --format=csv,noheader,nounits), so
// no unit conversion is needed.  A short timeout prevents a wedged driver from
// stalling the collector.  Implicit discovery is limited to trusted absolute
// system paths; PATH is never searched implicitly.
package nvidia

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultOutputLimit bounds captured stdout and stderr independently.
const DefaultOutputLimit = 64 * 1024

// ErrOutputLimit reports that nvidia-smi exceeded the captured output bound.
var ErrOutputLimit = errors.New("nvidia-smi output exceeded limit")

// trustedDefaultPaths are the only implicit locations.  Do not use
// exec.LookPath for the default path: an attacker-controlled PATH must not be
// able to substitute the binary.
var trustedDefaultPaths = []string{
	"/usr/bin/nvidia-smi",
	"/usr/local/bin/nvidia-smi",
}

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

// TrustedDefaultPath returns the first executable trusted system path.
func TrustedDefaultPath() (string, bool) {
	return findTrustedDefault(trustedDefaultPaths)
}

// Resolve returns the binary to execute.  An explicit non-empty path is used
// verbatim for testing/user control; otherwise only trusted absolute system
// paths are considered.  It never consults PATH.
func Resolve(explicit string) (string, bool) {
	if explicit != "" {
		return explicit, true
	}
	return TrustedDefaultPath()
}

func findTrustedDefault(paths []string) (string, bool) {
	for _, path := range paths {
		if !strings.HasPrefix(path, "/") {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return path, true
	}
	return "", false
}

// ExecRunner is the production Runner.  It uses direct exec with an argument
// array, captures bounded stdout/stderr, terminates the child on overflow, and
// waits for it before returning.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "" {
		return nil, errors.New("nvidia-smi path is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.WaitDelay = time.Second
	stdout := newLimitedWriter(DefaultOutputLimit, cancel)
	stderr := newLimitedWriter(DefaultOutputLimit, cancel)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if stdout.overflow || stderr.overflow {
		return nil, ErrOutputLimit
	}
	if err != nil {
		return nil, err
	}
	return stdout.buf.Bytes(), nil
}

type limitedWriter struct {
	limit    int
	buf      bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func newLimitedWriter(limit int, cancel context.CancelFunc) *limitedWriter {
	return &limitedWriter{limit: limit, cancel: cancel}
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.overflow {
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.markOverflow()
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = w.buf.Write(p[:remaining])
		w.markOverflow()
		return len(p), nil
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

func (w *limitedWriter) markOverflow() {
	if w.overflow {
		return
	}
	w.overflow = true
	if w.cancel != nil {
		w.cancel()
	}
}

// Sample runs nvidia-smi and parses its output only when the process exits
// successfully.  Any error, timeout or output-limit violation yields an empty
// Result; callers omit the GPU component in that case.
func (r *Reader) Sample(ctx context.Context) Result {
	if r == nil || r.Path == "" {
		return Result{}
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

	out, err := runner(ctx, r.Path,
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
