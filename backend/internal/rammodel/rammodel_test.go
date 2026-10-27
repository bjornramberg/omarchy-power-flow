package rammodel

import (
	"os"
	"path/filepath"
	"testing"

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

func meminfo(totalKB, availableKB uint64) string {
	return "MemTotal:       " + itoa(totalKB) + " kB\n" +
		"MemAvailable:   " + itoa(availableKB) + " kB\n"
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func pressure(avg10 string) string {
	return "some avg10=" + avg10 + " avg60=0.00 avg300=0.00 total=0\n" +
		"full avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"
}

func readInfo(t *testing.T, root string) Info {
	t.Helper()
	return ReadInfo(fsx.New(root))
}

func TestReadInfo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"), meminfo(16777216, 8388608))
	writeFile(t, filepath.Join(root, "proc", "pressure", "memory"), pressure("12.50"))

	info := readInfo(t, root)
	if !info.TotalOK || !info.AvailableOK || info.TotalBytes != 16*1024*1024*1024 {
		t.Fatalf("unexpected total/available: %+v", info)
	}
	if !info.PressureOK || info.PressureAvg10 != 12.5 {
		t.Fatalf("pressure = %v/%v, want 12.5/true", info.PressureAvg10, info.PressureOK)
	}
}

func TestEstimateIdle16GiB(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"), meminfo(16777216, 16777216))
	writeFile(t, filepath.Join(root, "proc", "pressure", "memory"), pressure("0.00"))

	est, ok := EstimateWatts(readInfo(t, root), 0, true)
	if !ok {
		t.Fatal("EstimateWatts returned false")
	}
	if est.DrawW != 2.4 || est.MaxW != 5.6 {
		t.Errorf("draw/max = %v/%v, want 2.4/5.6", est.DrawW, est.MaxW)
	}
	if est.UtilizationPercent != 0 || est.ActivityPercent != 0 {
		t.Errorf("util/activity = %d/%d, want 0/0", est.UtilizationPercent, est.ActivityPercent)
	}
	if est.UncertaintyPercent != 50 {
		t.Errorf("uncertainty = %d, want 50", est.UncertaintyPercent)
	}
}

func TestEstimateBusy16GiB(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"), meminfo(16777216, 16777216))
	writeFile(t, filepath.Join(root, "proc", "pressure", "memory"), pressure("100.00"))

	est, ok := EstimateWatts(readInfo(t, root), 100, true)
	if !ok {
		t.Fatal("EstimateWatts returned false")
	}
	if est.DrawW != 5.6 || est.MaxW != 5.6 {
		t.Errorf("draw/max = %v/%v, want 5.6/5.6", est.DrawW, est.MaxW)
	}
	if est.ActivityPercent != 100 {
		t.Errorf("activity = %d, want 100", est.ActivityPercent)
	}
}

func TestOccupancyDoesNotScaleWatts(t *testing.T) {
	root := t.TempDir()
	// 90% used, but no memory pressure and no CPU activity.
	writeFile(t, filepath.Join(root, "proc", "meminfo"), meminfo(16777216, 1677721))
	writeFile(t, filepath.Join(root, "proc", "pressure", "memory"), pressure("0.00"))

	est, ok := EstimateWatts(readInfo(t, root), 0, true)
	if !ok {
		t.Fatal("EstimateWatts returned false")
	}
	if est.UtilizationPercent < 89 || est.UtilizationPercent > 91 {
		t.Errorf("occupancy = %d, want about 90", est.UtilizationPercent)
	}
	if est.DrawW != 2.4 {
		t.Errorf("draw = %v, want idle 2.4 despite high occupancy", est.DrawW)
	}
}

func TestPressureContributesWeakly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"), meminfo(16777216, 16777216))
	writeFile(t, filepath.Join(root, "proc", "pressure", "memory"), pressure("50.00"))

	est, ok := EstimateWatts(readInfo(t, root), 0, true)
	if !ok {
		t.Fatal("EstimateWatts returned false")
	}
	if est.ActivityPercent != 38 {
		t.Errorf("activity = %d, want 38 from 75%% * 50%% pressure", est.ActivityPercent)
	}
	if est.DrawW != 3.6 {
		t.Errorf("draw = %v, want 3.6", est.DrawW)
	}
}

func TestMissingMemTotalOmitsRAM(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"), meminfo(0, 0))
	writeFile(t, filepath.Join(root, "proc", "pressure", "memory"), pressure("10.00"))

	if _, ok := EstimateWatts(readInfo(t, root), 50, true); ok {
		t.Fatal("missing MemTotal produced a model")
	}
	if _, ok := UtilizationPercent(readInfo(t, root)); ok {
		t.Fatal("missing MemTotal produced utilization")
	}

	// MemAvailable is required for an honest occupancy reading too.
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"), "MemTotal: 16777216 kB\n")
	if _, ok := EstimateWatts(readInfo(t, root), 50, true); ok {
		t.Fatal("missing MemAvailable produced a model")
	}
}

func TestMemInfoOverflowIsRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"),
		"MemTotal: 18446744073709551615 kB\n"+
			"MemAvailable: 16777216 kB\n")
	info := readInfo(t, root)
	if info.TotalOK || info.TotalBytes != 0 {
		t.Fatalf("overflowing MemTotal was accepted: %+v", info)
	}
	if _, ok := EstimateWatts(info, 50, true); ok {
		t.Fatal("overflowing MemTotal produced a model")
	}

	root = t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"),
		"MemTotal: 16777216 kB\n"+
			"MemAvailable: 18446744073709551615 kB\n")
	info = readInfo(t, root)
	if info.AvailableOK {
		t.Fatalf("overflowing MemAvailable was accepted: %+v", info)
	}
	if _, ok := EstimateWatts(info, 50, true); ok {
		t.Fatal("overflowing MemAvailable produced a model")
	}
}

func TestNoPressureUsesWeakCPUActivity(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "meminfo"), meminfo(16777216, 16777216))

	est, ok := EstimateWatts(readInfo(t, root), 100, true)
	if !ok {
		t.Fatal("EstimateWatts returned false")
	}
	if est.ActivityPercent != 25 || est.DrawW != 3.2 {
		t.Errorf("activity/draw = %d/%v, want 25/3.2", est.ActivityPercent, est.DrawW)
	}
	if est.UncertaintyPercent != 55 {
		t.Errorf("uncertainty = %d, want 55 without pressure", est.UncertaintyPercent)
	}
}
