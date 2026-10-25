// Command uth-power collects Linux power telemetry and emits one compact JSON
// object per line on stdout.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"uthpower/backend/internal/collector"
	"uthpower/backend/internal/report"
)

const exitSoftware = 70 // EX_SOFTWARE

// snapshotSource is the small seam used by run.  Tests can substitute a
// panicking implementation without touching the collector package.
type snapshotSource interface {
	Sample() report.Snapshot
}

var newSnapshotSource = func(opts collector.Options) snapshotSource {
	return collector.New(opts)
}

func main() {
	// Ignore SIGPIPE so a closed stdout becomes an ordinary write error and
	// the normal error path, rather than killing the process mid-line.
	signal.Ignore(syscall.SIGPIPE)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Stdout, os.Stderr, os.Args[1:])
	stop()
	os.Exit(code)
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) (exitCode int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Fprintf(stderr, "uth-power: panic: %s\n", boundedPanicText(recovered))
			fmt.Fprintf(stderr, "uth-power: stack:\n%s\n", boundedStack())
			exitCode = exitSoftware
		}
	}()

	fs := flag.NewFlagSet("uth-power", flag.ContinueOnError)
	fs.SetOutput(stderr)

	rootDefault := os.Getenv("UTH_POWER_ROOT")
	if rootDefault == "" {
		rootDefault = "/"
	}
	nvidiaDefault := os.Getenv("UTH_POWER_NVIDIA")
	if nvidiaDefault == "" {
		nvidiaDefault = "auto"
	}
	estimatesDefault := os.Getenv("UTH_POWER_ESTIMATES")
	if estimatesDefault == "" {
		estimatesDefault = "auto"
	}
	processesDefault := os.Getenv("UTH_POWER_PROCESSES")
	if processesDefault == "" {
		processesDefault = "auto"
	}

	root := fs.String("root", rootDefault,
		"filesystem root containing sys/ and proc/ (env UTH_POWER_ROOT)")
	interval := fs.Duration("interval", time.Second,
		"sampling and output interval in streaming mode")
	once := fs.Bool("once", false,
		"emit one JSON object and exit (for verification)")
	warmup := fs.Duration("warmup", 250*time.Millisecond,
		"prime delta-based sensors for this long before emitting")
	nvidiaMode := fs.String("nvidia", nvidiaDefault,
		"nvidia-smi mode: auto, on or off (env UTH_POWER_NVIDIA)")
	nvidiaSmi := fs.String("nvidia-smi", "nvidia-smi",
		"path to nvidia-smi")
	estimates := fs.String("estimates", estimatesDefault,
		"CPU/RAM model mode: auto or off (env UTH_POWER_ESTIMATES)")
	processes := fs.String("processes", processesDefault,
		"top-process metadata mode: auto or off (env UTH_POWER_PROCESSES)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: uth-power [flags]\n\n")
		fmt.Fprintf(stderr, "Emits one compact JSON object per line to stdout. "+
			"battery_percentage is -1 when no battery\ncapacity is available; "+
			"unreadable measured components are omitted and modeled values are marked.\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "uth-power: unexpected arguments: %v\n", fs.Args())
		return 2
	}
	if *interval <= 0 {
		fmt.Fprintln(stderr, "uth-power: --interval must be positive")
		return 2
	}
	if *warmup < 0 {
		fmt.Fprintln(stderr, "uth-power: --warmup must not be negative")
		return 2
	}
	mode, err := collector.ParseNvidiaMode(*nvidiaMode)
	if err != nil {
		fmt.Fprintf(stderr, "uth-power: %v\n", err)
		return 2
	}
	estMode, err := collector.ParseEstimatesMode(*estimates)
	if err != nil {
		fmt.Fprintf(stderr, "uth-power: %v\n", err)
		return 2
	}
	procMode, err := collector.ParseProcessesMode(*processes)
	if err != nil {
		fmt.Fprintf(stderr, "uth-power: %v\n", err)
		return 2
	}

	c := newSnapshotSource(collector.Options{
		Root:          *root,
		NvidiaMode:    mode,
		NvidiaSmiPath: *nvidiaSmi,
		Estimates:     estMode,
		Processes:     procMode,
	})

	// Prime delta sources.  The resulting snapshot is intentionally
	// discarded: it can only contain instantaneous sensors such as GPU
	// power.
	c.Sample()

	if *warmup > 0 {
		timer := time.NewTimer(*warmup)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}

	emit := func() bool {
		// Marshal the complete line before touching stdout.  A panic or
		// encoding error therefore cannot leave a partial JSON object behind.
		line, err := report.MarshalLine(c.Sample())
		if err != nil {
			fmt.Fprintf(stderr, "uth-power: encoding output: %v\n", err)
			return false
		}
		n, err := stdout.Write(line)
		if err != nil {
			fmt.Fprintf(stderr, "uth-power: writing output: %v\n", err)
			return false
		}
		if n != len(line) {
			fmt.Fprintf(stderr, "uth-power: short write: %d of %d bytes\n", n, len(line))
			return false
		}
		return true
	}

	if *once {
		if !emit() {
			return 1
		}
		return 0
	}

	if !emit() {
		return 1
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			if !emit() {
				return 1
			}
		}
	}
}

func boundedPanicText(value any) (text string) {
	defer func() {
		if recover() != nil {
			text = "<unprintable panic value>"
		}
	}()
	return truncate(fmt.Sprint(value), 1024)
}

func boundedStack() string {
	return truncate(string(debug.Stack()), 4096)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}
