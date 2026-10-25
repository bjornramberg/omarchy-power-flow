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
	"syscall"
	"time"

	"uthpower/backend/internal/collector"
	"uthpower/backend/internal/report"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Stdout, os.Stderr, os.Args[1:])
	stop()
	os.Exit(code)
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) int {
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

	c := collector.New(collector.Options{
		Root:          *root,
		NvidiaMode:    mode,
		NvidiaSmiPath: *nvidiaSmi,
		Estimates:     estMode,
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
		if err := report.Encode(stdout, c.Sample()); err != nil {
			fmt.Fprintf(stderr, "uth-power: writing output: %v\n", err)
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
