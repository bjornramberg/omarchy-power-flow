package powercap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"uthpower/backend/internal/fsx"
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

func zoneDir(root, name string) string {
	return filepath.Join(root, "sys", "class", "powercap", name)
}

func addZone(t *testing.T, root, entry, name, energy, maxPower, maxRange string) {
	t.Helper()
	dir := zoneDir(root, entry)
	writeFile(t, filepath.Join(dir, "name"), name+"\n")
	writeFile(t, filepath.Join(dir, "energy_uj"), energy+"\n")
	if maxPower != "" {
		writeFile(t, filepath.Join(dir, "constraint_0_max_power_uw"), maxPower+"\n")
	}
	if maxRange != "" {
		writeFile(t, filepath.Join(dir, "max_energy_range_uj"), maxRange+"\n")
	}
}

func TestDiscoverAndSampleRAPL(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)

	addZone(t, root, "intel-rapl:0", "package-0", "1000000", "150000000", "262143328850")
	addZone(t, root, "intel-rapl:0:0", "dram", "1000000", "15000000", "262143328850")
	// These must not contribute: core is already in the package, psys is the
	// whole platform.
	addZone(t, root, "intel-rapl:0:1", "core", "1000000", "50000000", "262143328850")
	addZone(t, root, "intel-rapl:0:2", "psys", "1000000", "", "262143328850")

	m := Discover(fs)
	if len(m.zones) != 2 {
		t.Fatalf("zones = %d, want 2", len(m.zones))
	}

	t0 := time.Unix(1000, 0)
	first := m.Sample(t0)
	if first.CPU.OK || first.RAM.OK {
		t.Fatalf("first sample must only prime counters: %+v", first)
	}
	if first.CPU.MaxW != 150 || first.RAM.MaxW != 15 {
		t.Fatalf("max watts = %v/%v, want 150/15", first.CPU.MaxW, first.RAM.MaxW)
	}

	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0"), "energy_uj"), "46500000\n")
	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0:0"), "energy_uj"), "3500000\n")
	// Core/psys energy changes are ignored.
	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0:1"), "energy_uj"), "99999999\n")

	second := m.Sample(t0.Add(time.Second))
	if !second.CPU.OK || !second.RAM.OK {
		t.Fatalf("expected measurements: %+v", second)
	}
	if second.CPU.DrawW != 45.5 {
		t.Errorf("CPU draw = %v, want 45.5", second.CPU.DrawW)
	}
	if second.RAM.DrawW != 2.5 {
		t.Errorf("RAM draw = %v, want 2.5", second.RAM.DrawW)
	}
	if second.CPU.MaxW != 150 || second.RAM.MaxW != 15 {
		t.Errorf("max watts = %v/%v, want 150/15", second.CPU.MaxW, second.RAM.MaxW)
	}
}

func TestSampleUsesElapsedTime(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addZone(t, root, "intel-rapl:0", "package-0", "0", "", "")

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	m.Sample(t0)
	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0"), "energy_uj"), "91000000\n")
	res := m.Sample(t0.Add(2 * time.Second))
	if !res.CPU.OK {
		t.Fatal("expected CPU measurement")
	}
	if res.CPU.DrawW != 45.5 {
		t.Fatalf("CPU draw = %v, want 45.5 over 2s", res.CPU.DrawW)
	}
}

func TestCounterWrap(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addZone(t, root, "intel-rapl:0", "package-0", "9900000", "", "10000000")

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	m.Sample(t0)
	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0"), "energy_uj"), "50000\n")
	res := m.Sample(t0.Add(time.Second))
	if !res.CPU.OK {
		t.Fatal("expected CPU measurement after wrap")
	}
	if res.CPU.DrawW != 0.15 {
		t.Fatalf("CPU draw = %v, want 0.15", res.CPU.DrawW)
	}
}

func TestMalformedAndMissingValues(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addZone(t, root, "intel-rapl:0", "package-0", "not-a-number", "bad", "")
	addZone(t, root, "intel-rapl:1", "package-1", "", "", "")

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	first := m.Sample(t0)
	second := m.Sample(t0.Add(time.Second))
	if first.CPU.OK || second.CPU.OK {
		t.Fatalf("malformed energy must not produce a measurement: %+v %+v", first, second)
	}
	if second.CPU.MaxW != 0 {
		t.Fatalf("malformed max power = %v, want 0", second.CPU.MaxW)
	}

	// A missing /sys/class/powercap is not an error.
	empty := Discover(fsx.New(t.TempDir()))
	if res := empty.Sample(time.Now()); res.CPU.OK || res.RAM.OK {
		t.Fatalf("empty meter produced data: %+v", res)
	}
}

func TestZoneClassificationUsesName(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)

	addZone(t, root, "intel-rapl:9", "package-0", "0", "", "")
	addZone(t, root, "amd-rapl:0", "package-0", "0", "", "")
	addZone(t, root, "intel-rapl:0:0", "dram", "0", "", "")
	addZone(t, root, "intel-rapl:0:1", "psys", "0", "", "")
	addZone(t, root, "intel-rapl:0:2", "core", "0", "", "")
	// A missing/empty name is never classified from the numeric path name.
	addZone(t, root, "intel-rapl:0:3", "", "0", "", "")

	m := Discover(fs)
	if len(m.zones) != 3 {
		t.Fatalf("zones = %d, want 3 (two packages and one dram): %+v", len(m.zones), m.zones)
	}
	cpu, ram := 0, 0
	for _, z := range m.zones {
		switch z.kind {
		case KindCPU:
			cpu++
		case KindRAM:
			ram++
		}
	}
	if cpu != 2 || ram != 1 {
		t.Fatalf("zone kinds = %d CPU/%d RAM, want 2/1: %+v", cpu, ram, m.zones)
	}
}

func TestUnreadableEnergyIsUnavailable(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	dir := zoneDir(root, "intel-rapl:0")
	writeFile(t, filepath.Join(dir, "name"), "package-0\n")
	if err := os.MkdirAll(filepath.Join(dir, "energy_uj"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := Discover(fs)
	if len(m.zones) != 1 {
		t.Fatalf("zones = %d, want 1", len(m.zones))
	}
	t0 := time.Unix(1000, 0)
	if first := m.Sample(t0); first.CPU.OK {
		t.Fatalf("unreadable energy produced data: %+v", first)
	}
	if second := m.Sample(t0.Add(time.Second)); second.CPU.OK {
		t.Fatalf("unreadable energy produced data: %+v", second)
	}
}

func TestPermissionDeniedIsUnavailable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode-000 files")
	}
	root := t.TempDir()
	fs := fsx.New(root)
	addZone(t, root, "intel-rapl:0", "package-0", "1000000", "", "")
	energyPath := filepath.Join(zoneDir(root, "intel-rapl:0"), "energy_uj")
	if err := os.Chmod(energyPath, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(energyPath, 0o644)

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	if first := m.Sample(t0); first.CPU.OK {
		t.Fatalf("permission-denied energy produced data: %+v", first)
	}
	if second := m.Sample(t0.Add(time.Second)); second.CPU.OK {
		t.Fatalf("permission-denied energy produced data: %+v", second)
	}
}

func TestPowerLimitFallback(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addZone(t, root, "intel-rapl:0", "package-0", "0", "", "")
	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0"), "constraint_0_power_limit_uw"),
		"25000000\n")

	m := Discover(fs)
	res := m.Sample(time.Unix(1000, 0))
	if res.CPU.MaxW != 25 {
		t.Fatalf("MaxW = %v, want 25 from power_limit fallback", res.CPU.MaxW)
	}
}

func TestDisabledZoneStillMonitorsEnergy(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addZone(t, root, "intel-rapl:0", "package-0", "1000000", "", "")
	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0"), "enabled"), "0\n")

	m := Discover(fs)
	if len(m.zones) != 1 {
		t.Fatalf("enabled=0 suppressed energy monitoring: %+v", m.zones)
	}
	t0 := time.Unix(1000, 0)
	m.Sample(t0)
	writeFile(t, filepath.Join(zoneDir(root, "intel-rapl:0"), "energy_uj"), "2000000\n")
	res := m.Sample(t0.Add(time.Second))
	if !res.CPU.OK || res.CPU.DrawW != 1 {
		t.Fatalf("enabled=0 zone measurement = %+v, want 1W", res.CPU)
	}
}
