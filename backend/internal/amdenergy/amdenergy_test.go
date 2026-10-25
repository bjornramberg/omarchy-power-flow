package amdenergy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"uthpower/backend/internal/fsx"
)

type channelSpec struct {
	label  string
	energy string
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hwmonDir(root, name string) string {
	return filepath.Join(root, "sys", "class", "hwmon", name)
}

func addAMDEnergy(t *testing.T, root, hwmon string, channels []channelSpec, capUW string) {
	t.Helper()
	dir := hwmonDir(root, hwmon)
	writeFile(t, filepath.Join(dir, "name"), "amd_energy\n")
	for i, ch := range channels {
		writeFile(t, filepath.Join(dir, "energy"+itoa(i+1)+"_label"), ch.label+"\n")
		writeFile(t, filepath.Join(dir, "energy"+itoa(i+1)+"_input"), ch.energy+"\n")
	}
	if capUW != "" {
		writeFile(t, filepath.Join(dir, "power1_cap"), capUW+"\n")
	}
}

func itoa(i int) string {
	return string(rune('0' + i))
}

func updateChannel(t *testing.T, root, hwmon, file, value string) {
	t.Helper()
	writeFile(t, filepath.Join(hwmonDir(root, hwmon), file), value+"\n")
}

func TestPackageWinsOverCore(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addAMDEnergy(t, root, "hwmon0", []channelSpec{
		{label: "Package", energy: "1000000"},
		{label: "DRAM", energy: "1000000"},
		{label: "Core", energy: "1000000"},
	}, "105000000")

	m := Discover(fs)
	if len(m.channels) != 3 {
		t.Fatalf("channels = %d, want 3", len(m.channels))
	}
	t0 := time.Unix(1000, 0)
	first := m.Sample(t0)
	if first.CPU.OK || first.RAM.OK {
		t.Fatalf("first sample must only prime: %+v", first)
	}
	if first.CPU.MaxW != 105 {
		t.Errorf("MaxW = %v, want 105", first.CPU.MaxW)
	}

	updateChannel(t, root, "hwmon0", "energy1_input", "46500000")
	updateChannel(t, root, "hwmon0", "energy2_input", "3500000")
	updateChannel(t, root, "hwmon0", "energy3_input", "99999999")

	res := m.Sample(t0.Add(time.Second))
	if !res.CPU.OK || !res.RAM.OK {
		t.Fatalf("expected measurements: %+v", res)
	}
	if res.CPU.DrawW != 45.5 {
		t.Errorf("CPU draw = %v, want 45.5 (package only)", res.CPU.DrawW)
	}
	if res.RAM.DrawW != 2.5 {
		t.Errorf("RAM draw = %v, want 2.5", res.RAM.DrawW)
	}
}

func TestCoreChannelsSumWhenNoPackage(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addAMDEnergy(t, root, "hwmon0", []channelSpec{
		{label: "Core", energy: "0"},
		{label: "Ecore", energy: "0"},
		{label: "Fclk", energy: "0"},
	}, "")

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	m.Sample(t0)
	updateChannel(t, root, "hwmon0", "energy1_input", "10000000")
	updateChannel(t, root, "hwmon0", "energy2_input", "5000000")
	updateChannel(t, root, "hwmon0", "energy3_input", "2000000")

	res := m.Sample(t0.Add(time.Second))
	if !res.CPU.OK {
		t.Fatalf("expected CPU measurement: %+v", res)
	}
	if res.CPU.DrawW != 17 {
		t.Fatalf("CPU draw = %v, want 17", res.CPU.DrawW)
	}
}

func TestUnknownChannelsSumWhenNoPackage(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addAMDEnergy(t, root, "hwmon0", []channelSpec{
		{label: "energy1", energy: "0"},
		{label: "energy2", energy: "0"},
	}, "")

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	m.Sample(t0)
	updateChannel(t, root, "hwmon0", "energy1_input", "1000000")
	updateChannel(t, root, "hwmon0", "energy2_input", "2000000")
	res := m.Sample(t0.Add(time.Second))
	if !res.CPU.OK || res.CPU.DrawW != 3 {
		t.Fatalf("CPU = %+v, want 3W", res.CPU)
	}
}

func TestResetAndMalformedValues(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	addAMDEnergy(t, root, "hwmon0", []channelSpec{
		{label: "Package", energy: "1000000"},
		{label: "DRAM", energy: "not-a-number"},
	}, "")

	m := Discover(fs)
	t0 := time.Unix(1000, 0)
	m.Sample(t0)
	updateChannel(t, root, "hwmon0", "energy1_input", "100000")
	res := m.Sample(t0.Add(time.Second))
	if res.CPU.OK {
		t.Fatalf("counter reset must not produce a measurement: %+v", res.CPU)
	}
	if res.RAM.OK {
		t.Fatalf("malformed RAM energy produced a measurement: %+v", res.RAM)
	}

	updateChannel(t, root, "hwmon0", "energy1_input", "1100000")
	res = m.Sample(t0.Add(2 * time.Second))
	if !res.CPU.OK || res.CPU.DrawW != 1 {
		t.Fatalf("CPU = %+v, want 1W after reset", res.CPU)
	}
}

func TestIgnoresOtherHwmon(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	writeFile(t, filepath.Join(hwmonDir(root, "hwmon0"), "name"), "k10temp\n")
	writeFile(t, filepath.Join(hwmonDir(root, "hwmon0"), "energy1_input"), "1000000\n")

	m := Discover(fs)
	if len(m.channels) != 0 {
		t.Fatalf("non-amd_energy hwmon was discovered")
	}
	if res := m.Sample(time.Now()); res.CPU.OK {
		t.Fatalf("unexpected measurement: %+v", res)
	}
}
