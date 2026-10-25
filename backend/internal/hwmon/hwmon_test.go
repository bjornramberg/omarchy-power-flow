package hwmon

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

func addHwmon(t *testing.T, root, hwmon, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "sys", "class", "hwmon", hwmon)
	writeFile(t, filepath.Join(dir, "name"), name+"\n")
	for file, value := range files {
		writeFile(t, filepath.Join(dir, file), value+"\n")
	}
}

func TestDiscoverAMDGpu(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addHwmon(t, root, "hwmon0", "amdgpu", map[string]string{
		"power1_input": "85000000",
		"power1_cap":   "300000000",
	})

	readings := Discover(fs).ReadingsAt(time.Unix(1000, 0))
	if len(readings) != 1 {
		t.Fatalf("readings = %d, want 1", len(readings))
	}
	r := readings[0]
	if !r.OK || r.Name != "amdgpu" {
		t.Fatalf("unexpected reading: %+v", r)
	}
	if r.DrawW != 85 {
		t.Errorf("DrawW = %v, want 85", r.DrawW)
	}
	if r.MaxW != 300 {
		t.Errorf("MaxW = %v, want 300", r.MaxW)
	}
}

func TestIgnoresNonGPUDevices(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addHwmon(t, root, "hwmon0", "coretemp", map[string]string{"power1_input": "1000000"})
	addHwmon(t, root, "hwmon1", "nvme", map[string]string{"power1_input": "1000000"})

	if m := Discover(fs); len(m.devices) != 0 {
		t.Fatalf("non-GPU hwmon devices were discovered")
	}
}

func TestAverageFallbackAndMultipleGPUs(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addHwmon(t, root, "hwmon0", "i915", map[string]string{
		"power1_input":   "bad",
		"power1_average": "1000000",
	})
	addHwmon(t, root, "hwmon1", "amdgpu", map[string]string{
		"power1_input": "2000000",
	})

	res := Discover(fs).Sample(time.Unix(1000, 0))
	if !res.OK || res.Count != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.DrawW != 3 {
		t.Errorf("DrawW = %v, want 3", res.DrawW)
	}
	if res.MaxW != 0 {
		t.Errorf("MaxW = %v, want 0 without caps", res.MaxW)
	}
}

func TestEnergyDeltaForIntelGPU(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	dir := filepath.Join(root, "sys", "class", "hwmon", "hwmon0")
	addHwmon(t, root, "hwmon0", "i915", map[string]string{
		"energy1_input": "1000000",
		"power1_cap":    "150000000",
	})

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	first := m.ReadingsAt(t0)
	if len(first) != 1 || first[0].OK {
		t.Fatalf("first energy sample must only prime: %+v", first)
	}

	writeFile(t, filepath.Join(dir, "energy1_input"), "46500000\n")
	second := m.ReadingsAt(t0.Add(time.Second))
	if len(second) != 1 || !second[0].OK {
		t.Fatalf("expected energy measurement: %+v", second)
	}
	if second[0].DrawW != 45.5 {
		t.Errorf("DrawW = %v, want 45.5", second[0].DrawW)
	}
	if second[0].MaxW != 150 {
		t.Errorf("MaxW = %v, want 150", second[0].MaxW)
	}
}

func TestEnergyCounterResetDoesNotInventPower(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	dir := filepath.Join(root, "sys", "class", "hwmon", "hwmon0")
	addHwmon(t, root, "hwmon0", "xe", map[string]string{"energy1_input": "10000000"})

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	m.ReadingsAt(t0)
	writeFile(t, filepath.Join(dir, "energy1_input"), "5000000\n")
	reset := m.ReadingsAt(t0.Add(time.Second))
	if reset[0].OK {
		t.Fatalf("counter reset produced power: %+v", reset[0])
	}
	writeFile(t, filepath.Join(dir, "energy1_input"), "6000000\n")
	after := m.ReadingsAt(t0.Add(2 * time.Second))
	if !after[0].OK || after[0].DrawW != 1 {
		t.Fatalf("post-reset reading = %+v, want 1W", after[0])
	}
}

func TestPowerPreferredOverEnergy(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	dir := filepath.Join(root, "sys", "class", "hwmon", "hwmon0")
	addHwmon(t, root, "hwmon0", "i915", map[string]string{
		"power1_input":  "5000000",
		"energy1_input": "1000000",
	})

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	first := m.ReadingsAt(t0)
	if !first[0].OK || first[0].DrawW != 5 {
		t.Fatalf("first reading = %+v, want 5W power", first[0])
	}
	writeFile(t, filepath.Join(dir, "energy1_input"), "100000000\n")
	second := m.ReadingsAt(t0.Add(time.Second))
	if !second[0].OK || second[0].DrawW != 5 {
		t.Fatalf("second reading = %+v, want 5W power", second[0])
	}
}

func TestMalformedPower(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addHwmon(t, root, "hwmon0", "amdgpu", map[string]string{
		"power1_input": "NaN",
		"power1_cap":   "300000000",
	})

	readings := Discover(fs).ReadingsAt(time.Unix(1000, 0))
	if len(readings) != 1 {
		t.Fatalf("readings = %d, want 1", len(readings))
	}
	if readings[0].OK {
		t.Fatalf("malformed power produced a reading: %+v", readings[0])
	}
	if res := Discover(fs).Sample(time.Unix(1000, 0)); res.OK {
		t.Fatalf("malformed power produced a sample: %+v", res)
	}
}

func TestIsNVIDIA(t *testing.T) {
	for _, name := range []string{"nouveau", "nvidia", "nvidia-gpu", "NVIDIA-GPU"} {
		if !IsNVIDIA(name) {
			t.Errorf("IsNVIDIA(%q) = false", name)
		}
	}
	for _, name := range []string{"amdgpu", "i915", ""} {
		if IsNVIDIA(name) {
			t.Errorf("IsNVIDIA(%q) = true", name)
		}
	}
}
