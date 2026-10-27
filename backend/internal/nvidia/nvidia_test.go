package nvidia

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSingleGPU(t *testing.T) {
	res := Parse([]byte("85.0, 300.00, 40\n"))
	if !res.OK || res.Count != 1 || res.Capped != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.DrawW != 85 || res.MaxW != 300 {
		t.Errorf("draw/max = %v/%v, want 85/300", res.DrawW, res.MaxW)
	}
	if !res.HasUtil || res.UtilPercent != 40 {
		t.Errorf("util = %v/%v, want 40", res.UtilPercent, res.HasUtil)
	}
}

func TestParseMultipleGPUs(t *testing.T) {
	res := Parse([]byte("85.0, 300.00, 40\n10.0, 150.00, 60\n"))
	if res.Count != 2 || res.Capped != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.DrawW != 95 || res.MaxW != 450 {
		t.Errorf("draw/max = %v/%v, want 95/450", res.DrawW, res.MaxW)
	}
	if !res.HasUtil || res.UtilPercent != 50 {
		t.Errorf("util = %v/%v, want 50", res.UtilPercent, res.HasUtil)
	}
}

func TestParseNAAndMissingFields(t *testing.T) {
	res := Parse([]byte("[N/A], [N/A], [N/A]\n50.0, [N/A], 20\n"))
	if res.Count != 1 || res.Capped != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.DrawW != 50 || res.MaxW != 0 {
		t.Errorf("draw/max = %v/%v, want 50/0", res.DrawW, res.MaxW)
	}
	if !res.HasUtil || res.UtilPercent != 20 {
		t.Errorf("util = %v/%v, want 20", res.UtilPercent, res.HasUtil)
	}
}

func TestParseMalformed(t *testing.T) {
	for _, input := range []string{
		"",
		"\n\n",
		"not-a-number, 300, 40\n",
		"NaN, 300, 40\n",
		"+Inf, 300, 40\n",
	} {
		res := Parse([]byte(input))
		if res.OK {
			t.Errorf("Parse(%q) returned OK: %+v", input, res)
		}
	}
}

func TestParseToleratesUnitSuffix(t *testing.T) {
	res := Parse([]byte("85 W, 300 W, 40 %\n"))
	if !res.OK || res.DrawW != 85 || res.MaxW != 300 || res.UtilPercent != 40 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestSampleRunnerError(t *testing.T) {
	r := Reader{
		Path: "nvidia-smi-test",
		Runner: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("no gpu")
		},
	}
	if res := r.Sample(context.Background()); res.OK {
		t.Fatalf("runner error produced data: %+v", res)
	}
}

func TestSampleNonZeroExitIgnoresStdout(t *testing.T) {
	r := Reader{
		Path: "nvidia-smi-test",
		Runner: func(context.Context, string, ...string) ([]byte, error) {
			// nvidia-smi can print diagnostics to stdout; only exit code 0
			// makes that output eligible for parsing.
			return []byte("85.0, 300.0, 40\nNVIDIA-SMI has failed\n"),
				errors.New("exit status 9")
		},
	}
	if res := r.Sample(context.Background()); res.OK {
		t.Fatalf("non-zero exit produced data: %+v", res)
	}
}

func TestSampleTimeout(t *testing.T) {
	r := Reader{
		Path:    "nvidia-smi-test",
		Timeout: 10 * time.Millisecond,
		Runner: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	start := time.Now()
	res := r.Sample(context.Background())
	if res.OK {
		t.Fatalf("timed out runner produced data: %+v", res)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Sample took %v, expected to stop at the timeout", elapsed)
	}
}

func TestExplicitPathOverride(t *testing.T) {
	var got string
	r := Reader{
		Path: "/custom/nvidia-smi",
		Runner: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			got = name
			return []byte("85, 300, 40\n"), nil
		},
	}
	if res := r.Sample(context.Background()); !res.OK {
		t.Fatalf("explicit path sample failed: %+v", res)
	}
	if got != "/custom/nvidia-smi" {
		t.Fatalf("runner received %q, want explicit override", got)
	}
}

func TestTrustedDefaultPathSelection(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first-nvidia-smi")
	second := filepath.Join(dir, "second-nvidia-smi")
	if err := os.WriteFile(first, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := findTrustedDefault([]string{"/definitely/missing", first, second})
	if !ok || got != first {
		t.Fatalf("selected %q/%v, want first path", got, ok)
	}
	got, ok = findTrustedDefault([]string{"/definitely/missing", second})
	if !ok || got != second {
		t.Fatalf("selected %q/%v, want second path", got, ok)
	}
	if _, ok := findTrustedDefault([]string{"/definitely/missing", dir}); ok {
		t.Fatal("directory selected as executable")
	}
}

func TestResolveDoesNotUsePath(t *testing.T) {
	old := trustedDefaultPaths
	trustedDefaultPaths = []string{filepath.Join(t.TempDir(), "missing-nvidia-smi")}
	t.Cleanup(func() { trustedDefaultPaths = old })

	pathDir := t.TempDir()
	fake := filepath.Join(pathDir, "nvidia-smi")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	if got, ok := Resolve(""); ok {
		t.Fatalf("implicit Resolve used PATH: %q", got)
	}
	if got, ok := Resolve("/custom/nvidia-smi"); !ok || got != "/custom/nvidia-smi" {
		t.Fatalf("explicit Resolve = %q/%v", got, ok)
	}
}

func TestNoImplicitPathWithoutReaderPath(t *testing.T) {
	called := false
	r := Reader{Runner: func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return []byte("85, 300, 40\n"), nil
	}}
	if res := r.Sample(context.Background()); res.OK {
		t.Fatalf("empty path produced data: %+v", res)
	}
	if called {
		t.Fatal("empty path attempted an implicit command")
	}
}

func TestExecRunnerOutputLimit(t *testing.T) {
	for _, mode := range []string{"flood-stdout", "flood-stderr"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("NVIDIA_HELPER_MODE", mode)
			start := time.Now()
			_, err := ExecRunner(context.Background(), os.Args[0],
				"-test.run=^TestNvidiaHelperProcess$")
			if !errors.Is(err, ErrOutputLimit) {
				t.Fatalf("err = %v, want ErrOutputLimit", err)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("output-limit child was not terminated promptly: %v", elapsed)
			}
		})
	}
}

func TestExecRunnerTimeout(t *testing.T) {
	t.Setenv("NVIDIA_HELPER_MODE", "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := ExecRunner(ctx, os.Args[0], "-test.run=^TestNvidiaHelperProcess$")
	if err == nil {
		t.Fatal("timed-out subprocess returned no error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

// TestNvidiaHelperProcess is a subprocess helper.  It is inert unless the
// parent test sets NVIDIA_HELPER_MODE.
func TestNvidiaHelperProcess(t *testing.T) {
	mode := os.Getenv("NVIDIA_HELPER_MODE")
	if mode == "" {
		return
	}
	chunk := []byte(strings.Repeat("x", 4096))
	switch mode {
	case "flood-stdout":
		for i := 0; i < 40; i++ {
			_, _ = os.Stdout.Write(chunk)
		}
		time.Sleep(2 * time.Second)
	case "flood-stderr":
		for i := 0; i < 40; i++ {
			_, _ = os.Stderr.Write(chunk)
		}
		time.Sleep(2 * time.Second)
	case "sleep":
		time.Sleep(2 * time.Second)
	}
	os.Exit(0)
}
