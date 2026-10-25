package nvidia

import (
	"context"
	"errors"
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

func TestAvailable(t *testing.T) {
	if Available("definitely-not-a-real-nvidia-smi-binary") {
		t.Fatal("unexpected binary found")
	}
}
