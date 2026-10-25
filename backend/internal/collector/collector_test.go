package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uthpower/backend/internal/report"
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

func addRAPLZone(t *testing.T, root, entry, name, energy, maxPower string) {
	t.Helper()
	dir := filepath.Join(root, "sys", "class", "powercap", entry)
	writeFile(t, filepath.Join(dir, "name"), name+"\n")
	writeFile(t, filepath.Join(dir, "energy_uj"), energy+"\n")
	if maxPower != "" {
		writeFile(t, filepath.Join(dir, "constraint_0_max_power_uw"), maxPower+"\n")
	}
}

func addHwmon(t *testing.T, root, name, driver string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "sys", "class", "hwmon", name)
	writeFile(t, filepath.Join(dir, "name"), driver+"\n")
	for file, value := range files {
		writeFile(t, filepath.Join(dir, file), value+"\n")
	}
}

func writeProc(t *testing.T, root, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "proc", name), content)
}

func addProcStat(t *testing.T, root string, pid int, comm string, utime, stime, starttime uint64, rssPages int64) {
	t.Helper()
	fields := []string{
		"S", "1", "1", "1", "0", "-1", "0",
		"0", "0", "0", "0", fmt.Sprint(utime), fmt.Sprint(stime),
		"0", "0", "20", "0", "1", "0", fmt.Sprint(starttime),
		"1000", fmt.Sprint(rssPages), "0",
	}
	content := fmt.Sprintf("%d (%s) %s\n", pid, comm, strings.Join(fields, " "))
	writeFile(t, filepath.Join(root, "proc", fmt.Sprint(pid), "stat"), content)
}

func diskLine(name string, ioTicks string) string {
	return "8 0 " + name + " 1000 0 10000 100 500 0 5000 50 0 " + ioTicks + " 0 0 0 0 0 0"
}

func addBlockDevice(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "sys", "block", name, "device"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeCPUInfo(t *testing.T, root, vendor, model string, cores int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < cores; i++ {
		fmt.Fprintf(&b,
			"processor : %d\nvendor_id : %s\nmodel name : %s\ncpu cores : %d\nphysical id : 0\ncore id : %d\nflags : fpu vme\n\n",
			i, vendor, model, cores, i)
	}
	writeProc(t, root, "cpuinfo", b.String())
}

func writeDMI(t *testing.T, root, chassis, sysVendor, product string) {
	t.Helper()
	base := filepath.Join(root, "sys", "class", "dmi", "id")
	if chassis != "" {
		writeFile(t, filepath.Join(base, "chassis_type"), chassis+"\n")
	}
	if sysVendor != "" {
		writeFile(t, filepath.Join(base, "sys_vendor"), sysVendor+"\n")
	}
	if product != "" {
		writeFile(t, filepath.Join(base, "product_name"), product+"\n")
	}
}

func writeMemInfo(t *testing.T, root string, totalKB, availableKB uint64) {
	t.Helper()
	writeProc(t, root, "meminfo", fmt.Sprintf(
		"MemTotal:       %d kB\nMemAvailable:   %d kB\n", totalKB, availableKB))
}

func writePressure(t *testing.T, root, avg10 string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "proc", "pressure", "memory"),
		"some avg10="+avg10+" avg60=0.00 avg300=0.00 total=0\n")
}

func addCPUFreq(t *testing.T, root, cpu string, currentKHz, maxKHz int) {
	t.Helper()
	dir := filepath.Join(root, "sys", "devices", "system", "cpu", cpu, "cpufreq")
	writeFile(t, filepath.Join(dir, "cpuinfo_avg_freq"), fmt.Sprintf("%d\n", currentKHz))
	writeFile(t, filepath.Join(dir, "cpuinfo_max_freq"), fmt.Sprintf("%d\n", maxKHz))
}

func findComponent(t *testing.T, snap report.Snapshot, id string) report.Component {
	t.Helper()
	for _, comp := range snap.Components {
		if comp.ID == id {
			return comp
		}
	}
	t.Fatalf("component %q not found in %+v", id, snap.Components)
	return report.Component{}
}

func requireDraw(t *testing.T, comp report.Component, want float64) {
	t.Helper()
	if comp.DrawW == nil {
		t.Fatalf("%s draw_w is null, want %v", comp.ID, want)
	}
	if *comp.DrawW != want {
		t.Fatalf("%s draw_w = %v, want %v", comp.ID, *comp.DrawW, want)
	}
}

func requireMax(t *testing.T, comp report.Component, want float64) {
	t.Helper()
	if comp.MaxW == nil {
		t.Fatalf("%s max_w is null, want %v", comp.ID, want)
	}
	if *comp.MaxW != want {
		t.Fatalf("%s max_w = %v, want %v", comp.ID, *comp.MaxW, want)
	}
}

func requireUtil(t *testing.T, comp report.Component, want int) {
	t.Helper()
	if comp.UtilizationPercent == nil {
		t.Fatalf("%s utilization_percent is absent, want %d", comp.ID, want)
	}
	if *comp.UtilizationPercent != want {
		t.Fatalf("%s utilization_percent = %d, want %d", comp.ID, *comp.UtilizationPercent, want)
	}
}

func requireUncertainty(t *testing.T, comp report.Component, want int) {
	t.Helper()
	if comp.UncertaintyPercent == nil {
		t.Fatalf("%s uncertainty_percent is absent, want %d", comp.ID, want)
	}
	if *comp.UncertaintyPercent != want {
		t.Fatalf("%s uncertainty_percent = %d, want %d", comp.ID, *comp.UncertaintyPercent, want)
	}
}

func requireNoEstimateMetadata(t *testing.T, comp report.Component) {
	t.Helper()
	if comp.UncertaintyPercent != nil || comp.Model != nil {
		t.Fatalf("%s unexpectedly has estimate metadata: %+v", comp.ID, comp)
	}
}

func TestCollectorSnapshotMeasuredAndDiskModeled(t *testing.T) {
	root := t.TempDir()
	addRAPLZone(t, root, "intel-rapl:0", "package-0", "1000000", "150000000")
	addRAPLZone(t, root, "intel-rapl:0:0", "dram", "1000000", "15000000")
	addHwmon(t, root, "hwmon0", "amdgpu", map[string]string{
		"power1_input": "85000000",
		"power1_cap":   "300000000",
	})
	writeProc(t, root, "stat", "cpu  100 0 50 850 0 0 0 0 0 0\n")
	writeProc(t, root, "diskstats", diskLine("sda", "1000")+"\n")
	addBlockDevice(t, root, "sda")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "AC", "type"), "Mains\n")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "AC", "online"), "1\n")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "BAT0", "type"), "Battery\n")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "BAT0", "capacity"), "87\n")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "BAT0", "status"), "Charging\n")

	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	first := c.SampleAt(t0)
	if len(first.Components) != 1 || first.Components[0].ID != report.IDGPU {
		t.Fatalf("warm-up components = %+v, want only GPU", first.Components)
	}

	addRAPLZone(t, root, "intel-rapl:0", "package-0", "46500000", "150000000")
	addRAPLZone(t, root, "intel-rapl:0:0", "dram", "3500000", "15000000")
	writeProc(t, root, "stat", "cpu  300 0 100 1600 0 0 0 0 0 0\n")
	writeProc(t, root, "diskstats", diskLine("sda", "1500")+"\n")

	snap := c.SampleAt(t0.Add(time.Second))
	if snap.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", snap.SchemaVersion)
	}
	if snap.Source.Name != "AC" || snap.Source.BatteryPercentage != 87 {
		t.Errorf("source = %+v, want AC/87", snap.Source)
	}
	if snap.Source.PowerMode != report.PowerModeAC || snap.Source.BatteryStatus != report.BatteryCharging {
		t.Errorf("source state = %q/%q, want ac/charging", snap.Source.PowerMode, snap.Source.BatteryStatus)
	}
	if snap.Source.TotalDrawW != 138.25 {
		t.Errorf("total = %v, want 138.25", snap.Source.TotalDrawW)
	}
	if snap.Source.TotalDrawKind != report.TotalKindMixed {
		t.Errorf("total kind = %q, want mixed", snap.Source.TotalDrawKind)
	}

	cpu := findComponent(t, snap, report.IDCPU)
	if cpu.DrawKind != report.DrawKindMeasured || cpu.DrawSource != "powercap:rapl" {
		t.Errorf("cpu metadata = %s/%s", cpu.DrawKind, cpu.DrawSource)
	}
	requireDraw(t, cpu, 45.5)
	requireMax(t, cpu, 150)
	requireUtil(t, cpu, 25)
	if cpu.ActivityPercent != 25 {
		t.Errorf("cpu activity = %d, want 25", cpu.ActivityPercent)
	}
	requireNoEstimateMetadata(t, cpu)

	gpu := findComponent(t, snap, report.IDGPU)
	if gpu.DrawKind != report.DrawKindMeasured || gpu.DrawSource != "hwmon:amdgpu" {
		t.Errorf("gpu metadata = %s/%s", gpu.DrawKind, gpu.DrawSource)
	}
	requireDraw(t, gpu, 85)
	requireMax(t, gpu, 300)
	if gpu.ActivityPercent != 28 || gpu.UtilizationPercent != nil {
		t.Errorf("gpu activity/util = %d/%v, want 28/nil", gpu.ActivityPercent, gpu.UtilizationPercent)
	}
	requireNoEstimateMetadata(t, gpu)

	ram := findComponent(t, snap, report.IDRAM)
	if ram.DrawKind != report.DrawKindMeasured || ram.DrawSource != "powercap:rapl" {
		t.Errorf("ram metadata = %s/%s", ram.DrawKind, ram.DrawSource)
	}
	requireDraw(t, ram, 2.5)
	requireMax(t, ram, 15)
	requireNoEstimateMetadata(t, ram)

	disk := findComponent(t, snap, report.IDDisk)
	if disk.DrawKind != report.DrawKindModeled || disk.DrawSource != "model:disk-busy/1" {
		t.Errorf("disk metadata = %s/%s", disk.DrawKind, disk.DrawSource)
	}
	requireDraw(t, disk, 5.25)
	requireMax(t, disk, 10)
	requireUncertainty(t, disk, 50)
	if disk.Model == nil || disk.Model.ID != "disk-busy/1" {
		t.Errorf("disk model = %+v", disk.Model)
	}
	if disk.ActivityPercent != 50 {
		t.Errorf("disk activity = %d, want 50", disk.ActivityPercent)
	}
}

func TestCollectorMeasuredWinsOverModels(t *testing.T) {
	root := t.TempDir()
	writeCPUInfo(t, root, "AuthenticAMD", "AMD Ryzen 7 5800X 8-Core Processor", 8)
	writeDMI(t, root, "3", "ASUS", "Desktop")
	addCPUFreq(t, root, "cpu0", 1500000, 3000000)
	writeMemInfo(t, root, 16777216, 8388608)
	writePressure(t, root, "90.00")
	addRAPLZone(t, root, "intel-rapl:0", "package-0", "1000000", "150000000")
	addRAPLZone(t, root, "intel-rapl:0:0", "dram", "1000000", "15000000")
	writeProc(t, root, "stat", "cpu  100 0 50 850 0 0 0 0 0 0\n")

	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)
	writeProc(t, root, "stat", "cpu  300 0 100 1600 0 0 0 0 0 0\n")
	addRAPLZone(t, root, "intel-rapl:0", "package-0", "46500000", "150000000")
	addRAPLZone(t, root, "intel-rapl:0:0", "dram", "3500000", "15000000")

	snap := c.SampleAt(t0.Add(time.Second))
	cpu := findComponent(t, snap, report.IDCPU)
	if cpu.DrawKind != report.DrawKindMeasured || cpu.DrawSource != "powercap:rapl" {
		t.Fatalf("cpu did not stay measured: %+v", cpu)
	}
	requireDraw(t, cpu, 45.5)
	uncertainty := cpu.UncertaintyPercent
	if uncertainty != nil || cpu.Model != nil {
		t.Fatalf("measured CPU carries model metadata: %+v", cpu)
	}

	ram := findComponent(t, snap, report.IDRAM)
	if ram.DrawKind != report.DrawKindMeasured || ram.DrawSource != "powercap:rapl" {
		t.Fatalf("ram did not stay measured: %+v", ram)
	}
	requireDraw(t, ram, 2.5)
	requireUtil(t, ram, 50)
	if ram.Model != nil || ram.UncertaintyPercent != nil {
		t.Fatalf("measured RAM carries model metadata: %+v", ram)
	}
	if snap.Source.TotalDrawKind != report.TotalKindMeasured {
		t.Errorf("total kind = %q, want measured", snap.Source.TotalDrawKind)
	}
}

func TestCollectorCPUModeledWithFrequency(t *testing.T) {
	root := t.TempDir()
	writeCPUInfo(t, root, "AuthenticAMD", "AMD Ryzen 7 5800X 8-Core Processor", 8)
	writeDMI(t, root, "3", "ASUS", "Desktop")
	addCPUFreq(t, root, "cpu0", 1500000, 3000000)
	writeProc(t, root, "stat", "cpu  0 0 0 1000 0 0 0 0 0 0\n")

	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)
	writeProc(t, root, "stat", "cpu  1000 0 0 1000 0 0 0 0 0 0\n")
	snap := c.SampleAt(t0.Add(time.Second))

	cpu := findComponent(t, snap, report.IDCPU)
	if cpu.DrawKind != report.DrawKindModeled || cpu.DrawSource != "model:cpu-load/1" {
		t.Fatalf("cpu metadata = %s/%s", cpu.DrawKind, cpu.DrawSource)
	}
	requireDraw(t, cpu, 64.4)
	requireMax(t, cpu, 142)
	requireUncertainty(t, cpu, 35)
	requireUtil(t, cpu, 100)
	if cpu.ActivityPercent != 100 {
		t.Errorf("activity = %d, want 100", cpu.ActivityPercent)
	}
	if cpu.Model == nil || cpu.Model.ID != "cpu-load/1" {
		t.Fatalf("model = %+v", cpu.Model)
	}
	if got := cpu.Model.Assumptions["class"]; got != "ryzen-7-5800x" {
		t.Errorf("model class = %v, want ryzen-7-5800x", got)
	}
	if snap.Source.TotalDrawKind != report.TotalKindModeled {
		t.Errorf("total kind = %q, want modeled", snap.Source.TotalDrawKind)
	}
	if snap.Source.TotalDrawW != 64.4 {
		t.Errorf("total = %v, want 64.4", snap.Source.TotalDrawW)
	}
}

func TestCollectorCPUUtilizationOnly(t *testing.T) {
	tests := []struct {
		name string
		arch string
		vm   bool
	}{
		{name: "vm", arch: "amd64", vm: true},
		{name: "non-x86", arch: "arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeCPUInfo(t, root, "GenuineIntel", "Intel Core", 4)
			if tt.vm {
				writeDMI(t, root, "3", "QEMU", "Standard PC")
			}
			writeProc(t, root, "stat", "cpu  100 0 50 850 0 0 0 0 0 0\n")

			c := New(Options{Root: root, NvidiaMode: NvidiaOff, Arch: tt.arch})
			t0 := time.Unix(1000, 0)
			c.SampleAt(t0)
			writeProc(t, root, "stat", "cpu  300 0 100 1600 0 0 0 0 0 0\n")
			snap := c.SampleAt(t0.Add(time.Second))

			cpu := findComponent(t, snap, report.IDCPU)
			if cpu.DrawKind != report.DrawKindUtilization || cpu.DrawSource != "proc:stat" {
				t.Fatalf("cpu metadata = %s/%s", cpu.DrawKind, cpu.DrawSource)
			}
			if cpu.DrawW != nil || cpu.MaxW != nil {
				t.Fatalf("utilization-only draw/max must be null: %+v", cpu)
			}
			requireUtil(t, cpu, 25)
			if cpu.ActivityPercent != 25 {
				t.Errorf("activity = %d, want 25", cpu.ActivityPercent)
			}
			requireNoEstimateMetadata(t, cpu)
			if snap.Source.TotalDrawKind != report.TotalKindMixed {
				t.Errorf("total kind = %q, want mixed", snap.Source.TotalDrawKind)
			}
			if snap.Source.TotalDrawW != 0 {
				t.Errorf("total = %v, want 0", snap.Source.TotalDrawW)
			}
		})
	}
}

func TestCollectorMissingProcStatOmitsCPU(t *testing.T) {
	root := t.TempDir()
	writeCPUInfo(t, root, "AuthenticAMD", "AMD Ryzen 7 5800X 8-Core Processor", 8)
	writeDMI(t, root, "3", "ASUS", "Desktop")

	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	snap := c.SampleAt(time.Unix(1000, 0))
	for _, comp := range snap.Components {
		if comp.ID == report.IDCPU {
			t.Fatalf("CPU emitted without /proc/stat: %+v", comp)
		}
	}
	if snap.Source.TotalDrawKind != report.TotalKindNone {
		t.Errorf("total kind = %q, want none", snap.Source.TotalDrawKind)
	}
}

func TestCollectorRAMModeled(t *testing.T) {
	root := t.TempDir()
	writeMemInfo(t, root, 16777216, 16777216)
	writePressure(t, root, "0.00")

	snap := New(Options{Root: root, NvidiaMode: NvidiaOff}).SampleAt(time.Unix(1000, 0))
	ram := findComponent(t, snap, report.IDRAM)
	if ram.DrawKind != report.DrawKindModeled || ram.DrawSource != "model:ram-capacity/1" {
		t.Fatalf("ram metadata = %s/%s", ram.DrawKind, ram.DrawSource)
	}
	requireDraw(t, ram, 2.4)
	requireMax(t, ram, 5.6)
	requireUtil(t, ram, 0)
	requireUncertainty(t, ram, 55) // CPU utilisation unavailable in this fixture
	if ram.ActivityPercent != 0 {
		t.Errorf("activity = %d, want 0", ram.ActivityPercent)
	}
	if ram.Model == nil || ram.Model.ID != "ram-capacity/1" {
		t.Fatalf("model = %+v", ram.Model)
	}
	if snap.Source.TotalDrawKind != report.TotalKindModeled {
		t.Errorf("total kind = %q, want modeled", snap.Source.TotalDrawKind)
	}
}

func TestCollectorRAMModeledBusyAndOccupancy(t *testing.T) {
	root := t.TempDir()
	writeMemInfo(t, root, 16777216, 1677721) // about 90% used
	writePressure(t, root, "0.00")
	snap := New(Options{Root: root, NvidiaMode: NvidiaOff}).SampleAt(time.Unix(1000, 0))
	ram := findComponent(t, snap, report.IDRAM)
	requireDraw(t, ram, 2.4) // occupancy does not scale watts
	requireUtil(t, ram, 90)

	root = t.TempDir()
	writeMemInfo(t, root, 16777216, 16777216)
	writePressure(t, root, "100.00")
	writeProc(t, root, "stat", "cpu  0 0 0 1000 0 0 0 0 0 0\n")
	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)
	writeProc(t, root, "stat", "cpu  1000 0 0 1000 0 0 0 0 0 0\n")
	snap = c.SampleAt(t0.Add(time.Second))
	ram = findComponent(t, snap, report.IDRAM)
	requireDraw(t, ram, 5.6)
	requireMax(t, ram, 5.6)
	if ram.ActivityPercent != 100 {
		t.Errorf("activity = %d, want 100", ram.ActivityPercent)
	}
}

func TestCollectorRAMMissingMemTotal(t *testing.T) {
	root := t.TempDir()
	writeProc(t, root, "meminfo", "MemAvailable: 1000 kB\n")
	snap := New(Options{Root: root, NvidiaMode: NvidiaOff}).SampleAt(time.Unix(1000, 0))
	for _, comp := range snap.Components {
		if comp.ID == report.IDRAM {
			t.Fatalf("RAM emitted without MemTotal: %+v", comp)
		}
	}
}

func TestCollectorDiskModeledNumbersUnchanged(t *testing.T) {
	root := t.TempDir()
	addBlockDevice(t, root, "sda")
	writeProc(t, root, "diskstats", diskLine("sda", "1000")+"\n")

	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)
	writeProc(t, root, "diskstats", diskLine("sda", "1500")+"\n")
	snap := c.SampleAt(t0.Add(time.Second))

	disk := findComponent(t, snap, report.IDDisk)
	if disk.DrawKind != report.DrawKindModeled || disk.DrawSource != "model:disk-busy/1" {
		t.Fatalf("disk metadata = %s/%s", disk.DrawKind, disk.DrawSource)
	}
	requireDraw(t, disk, 5.25)
	requireMax(t, disk, 10)
	requireUncertainty(t, disk, 50)
	if disk.ActivityPercent != 50 {
		t.Errorf("activity = %d, want 50", disk.ActivityPercent)
	}
	if snap.Source.TotalDrawKind != report.TotalKindModeled || snap.Source.TotalDrawW != 5.25 {
		t.Errorf("source = %+v, want modeled/5.25", snap.Source)
	}
}

func TestCollectorEstimatesOff(t *testing.T) {
	root := t.TempDir()
	writeCPUInfo(t, root, "AuthenticAMD", "AMD Ryzen 7 5800X 8-Core Processor", 8)
	writeDMI(t, root, "3", "ASUS", "Desktop")
	addCPUFreq(t, root, "cpu0", 3000000, 3000000)
	writeMemInfo(t, root, 16777216, 16777216)
	writePressure(t, root, "50.00")
	writeProc(t, root, "stat", "cpu  0 0 0 1000 0 0 0 0 0 0\n")

	t0 := time.Unix(1000, 0)
	auto := New(Options{Root: root, NvidiaMode: NvidiaOff, Estimates: EstimatesAuto})
	auto.SampleAt(t0)
	writeProc(t, root, "stat", "cpu  1000 0 0 1000 0 0 0 0 0 0\n")
	autoSnap := auto.SampleAt(t0.Add(time.Second))
	if len(autoSnap.Components) != 2 {
		t.Fatalf("auto components = %+v, want CPU+RAM models", autoSnap.Components)
	}

	off := New(Options{Root: root, NvidiaMode: NvidiaOff, Estimates: EstimatesOff})
	off.SampleAt(t0)
	offSnap := off.SampleAt(t0.Add(time.Second))
	if len(offSnap.Components) != 0 {
		t.Fatalf("estimates=off components = %+v, want none", offSnap.Components)
	}
	if offSnap.Source.TotalDrawKind != report.TotalKindNone {
		t.Errorf("off total kind = %q, want none", offSnap.Source.TotalDrawKind)
	}
}

func TestCollectorEstimatesOffKeepsMeasured(t *testing.T) {
	root := t.TempDir()
	addRAPLZone(t, root, "intel-rapl:0", "package-0", "1000000", "150000000")
	writeProc(t, root, "stat", "cpu  0 0 0 1000 0 0 0 0 0 0\n")

	c := New(Options{Root: root, NvidiaMode: NvidiaOff, Estimates: EstimatesOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)
	addRAPLZone(t, root, "intel-rapl:0", "package-0", "46500000", "150000000")
	writeProc(t, root, "stat", "cpu  1000 0 0 1000 0 0 0 0 0 0\n")
	snap := c.SampleAt(t0.Add(time.Second))

	cpu := findComponent(t, snap, report.IDCPU)
	if cpu.DrawKind != report.DrawKindMeasured || cpu.DrawSource != "powercap:rapl" {
		t.Fatalf("estimated=off CPU = %s/%s, want measured/powercap:rapl", cpu.DrawKind, cpu.DrawSource)
	}
	requireDraw(t, cpu, 45.5)
	requireUtil(t, cpu, 100)
	if snap.Source.TotalDrawKind != report.TotalKindMeasured {
		t.Errorf("total kind = %q, want measured", snap.Source.TotalDrawKind)
	}
}

func setupProcessRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeCPUInfo(t, root, "AuthenticAMD", "AMD Ryzen 7 5800X 8-Core Processor", 8)
	writeDMI(t, root, "3", "ASUS", "Desktop")
	addCPUFreq(t, root, "cpu0", 3000000, 3000000)
	writeMemInfo(t, root, 16777216, 16777216)
	writePressure(t, root, "0.00")
	writeProc(t, root, "stat", "cpu  0 0 0 1000 0 0 0 0 0 0\n")
	addProcStat(t, root, 100, "busy", 0, 0, 100, 20)
	addProcStat(t, root, 101, "sleep", 0, 0, 100, 100)
	return root
}

func TestCollectorTopProcesses(t *testing.T) {
	root := setupProcessRoot(t)
	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)

	writeProc(t, root, "stat", "cpu  1000 0 0 1000 0 0 0 0 0 0\n")
	addProcStat(t, root, 100, "busy", 60, 40, 100, 20)
	addProcStat(t, root, 101, "sleep", 0, 0, 100, 100)
	snap := c.SampleAt(t0.Add(time.Second))

	cpu := findComponent(t, snap, report.IDCPU)
	if cpu.TopProcess == nil {
		t.Fatal("CPU top_process missing")
	}
	topCPU := cpu.TopProcess
	if topCPU.PID != 100 || topCPU.Name != "busy" || topCPU.Metric != report.TopMetricCPU {
		t.Errorf("CPU top process = %+v", topCPU)
	}
	if topCPU.Value != 10 || topCPU.Unit != "percent_of_machine" {
		t.Errorf("CPU top value = %v %s, want 10 percent_of_machine", topCPU.Value, topCPU.Unit)
	}
	if topCPU.CorePercent == nil || *topCPU.CorePercent != 100 {
		t.Errorf("CPU core percent = %v, want 100", topCPU.CorePercent)
	}
	if topCPU.WindowMS != 1000 || topCPU.Scope != "all_processes" || topCPU.Source != "proc:stat" || !topCPU.OK {
		t.Errorf("CPU top metadata = %+v", topCPU)
	}

	ram := findComponent(t, snap, report.IDRAM)
	if ram.TopProcess == nil {
		t.Fatal("RAM top_process missing")
	}
	topRAM := ram.TopProcess
	if topRAM.PID != 101 || topRAM.Name != "sleep" || topRAM.Metric != report.TopMetricRSS {
		t.Errorf("RAM top process = %+v", topRAM)
	}
	if topRAM.Value != 100*4096 || topRAM.Unit != "bytes" || topRAM.WindowMS != 0 {
		t.Errorf("RAM top metadata = %+v", topRAM)
	}
	if topRAM.CorePercent != nil {
		t.Errorf("RAM top core_percent = %v, want nil", *topRAM.CorePercent)
	}
}

func TestCollectorProcessesOff(t *testing.T) {
	root := setupProcessRoot(t)
	c := New(Options{Root: root, NvidiaMode: NvidiaOff, Processes: ProcessesOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)
	writeProc(t, root, "stat", "cpu  1000 0 0 1000 0 0 0 0 0 0\n")
	addProcStat(t, root, 100, "busy", 60, 40, 100, 20)
	snap := c.SampleAt(t0.Add(time.Second))

	for _, id := range []string{report.IDCPU, report.IDRAM} {
		comp := findComponent(t, snap, id)
		if comp.TopProcess != nil {
			t.Errorf("%s has top_process with --processes off: %+v", id, comp.TopProcess)
		}
	}
}

func TestCollectorSourceUnknown(t *testing.T) {
	snap := New(Options{Root: t.TempDir(), NvidiaMode: NvidiaOff}).SampleAt(time.Unix(1000, 0))
	if snap.Source.Name != "Unknown" || snap.Source.PowerMode != report.PowerModeUnknown {
		t.Errorf("source = %+v, want Unknown/unknown", snap.Source)
	}
	if snap.Source.BatteryStatus != "" || snap.Source.BatteryPercentage != report.BatteryUnavailable {
		t.Errorf("source battery = %+v, want no status and -1", snap.Source)
	}
}

func TestZeroValueCollectorSample(t *testing.T) {
	var zero Collector
	snap := zero.Sample()
	if snap.SchemaVersion != report.SchemaVersion {
		t.Errorf("schema = %d, want %d", snap.SchemaVersion, report.SchemaVersion)
	}
	if snap.Source.PowerMode != report.PowerModeUnknown || len(snap.Components) != 0 {
		t.Errorf("zero-value snapshot = %+v", snap)
	}
	if snapAt := zero.SampleAt(time.Unix(1000, 0)); snapAt.Source.PowerMode != report.PowerModeUnknown {
		t.Errorf("zero-value SampleAt = %+v", snapAt)
	}
}

func TestUncappedCount(t *testing.T) {
	tests := []struct {
		count, capped, want int
	}{
		{0, 0, 0},
		{2, 0, 2},
		{2, 1, 1},
		{2, 3, 0},
		{-1, 2, 0},
		{2, -1, 2},
	}
	for _, tt := range tests {
		if got := uncappedCount(tt.count, tt.capped); got != tt.want {
			t.Errorf("uncappedCount(%d,%d) = %d, want %d", tt.count, tt.capped, got, tt.want)
		}
	}
}

func TestCollectorNvidia(t *testing.T) {
	root := t.TempDir()
	var gotArgs []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("85.0, 300.0, 40\n"), nil
	}

	c := New(Options{Root: root, NvidiaMode: NvidiaOn, NvidiaRunner: runner})
	snap := c.SampleAt(time.Unix(1000, 0))
	gpu := findComponent(t, snap, report.IDGPU)
	if gpu.DrawKind != report.DrawKindMeasured || gpu.DrawSource != "nvidia-smi" {
		t.Fatalf("gpu metadata = %s/%s", gpu.DrawKind, gpu.DrawSource)
	}
	requireDraw(t, gpu, 85)
	requireMax(t, gpu, 300)
	requireUtil(t, gpu, 40)
	if gpu.ActivityPercent != 40 {
		t.Errorf("activity = %d, want 40", gpu.ActivityPercent)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "--query-gpu=power.draw,power.limit,utilization.gpu" {
		t.Errorf("unexpected nvidia-smi args: %v", gotArgs)
	}
}

func TestCollectorSkipsDuplicateNvidiaHwmon(t *testing.T) {
	root := t.TempDir()
	addHwmon(t, root, "hwmon0", "nouveau", map[string]string{
		"power1_input": "10000000",
	})
	runner := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("85.0, 300.0, 40\n"), nil
	}

	c := New(Options{Root: root, NvidiaMode: NvidiaOn, NvidiaRunner: runner})
	snap := c.SampleAt(time.Unix(1000, 0))
	gpu := findComponent(t, snap, report.IDGPU)
	requireDraw(t, gpu, 85)
	if gpu.DrawSource != "nvidia-smi" {
		t.Errorf("draw_source = %q, want nvidia-smi", gpu.DrawSource)
	}
}

func TestCollectorAutoIsHermeticWithRootOverride(t *testing.T) {
	root := t.TempDir()
	called := false
	runner := func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return []byte("85.0, 300.0, 40\n"), nil
	}

	c := New(Options{Root: root, NvidiaMode: NvidiaAuto, NvidiaRunner: runner})
	c.SampleAt(time.Unix(1000, 0))
	if called {
		t.Fatal("auto mode executed nvidia-smi with a non-default root")
	}
}

func TestCollectorIntelEnergyGPU(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sys", "class", "hwmon", "hwmon0")
	addHwmon(t, root, "hwmon0", "i915", map[string]string{"energy1_input": "0"})

	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	first := c.SampleAt(t0)
	if len(first.Components) != 0 {
		t.Fatalf("energy GPU must be primed first: %+v", first.Components)
	}

	writeFile(t, filepath.Join(dir, "energy1_input"), "45500000\n")
	snap := c.SampleAt(t0.Add(time.Second))
	gpu := findComponent(t, snap, report.IDGPU)
	if gpu.DrawKind != report.DrawKindMeasured || gpu.DrawSource != "hwmon:i915" {
		t.Fatalf("gpu metadata = %s/%s", gpu.DrawKind, gpu.DrawSource)
	}
	requireDraw(t, gpu, 45.5)
	requireMax(t, gpu, 300)
	if gpu.ActivityPercent != 15 || gpu.UtilizationPercent != nil {
		t.Fatalf("gpu activity/util = %d/%v, want 15/nil", gpu.ActivityPercent, gpu.UtilizationPercent)
	}
}

func TestCollectorMalformedSensorsDoNotPanic(t *testing.T) {
	root := t.TempDir()
	addRAPLZone(t, root, "intel-rapl:0", "package-0", "garbage", "garbage")
	addHwmon(t, root, "hwmon0", "amdgpu", map[string]string{"power1_input": "garbage"})
	writeProc(t, root, "stat", "not a stat file\n")
	writeProc(t, root, "diskstats", "not diskstats\n")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "BAT0", "type"), "Battery\n")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "BAT0", "status"), "Discharging\n")
	writeFile(t, filepath.Join(root, "sys", "class", "power_supply", "BAT0", "capacity"), "garbage\n")

	c := New(Options{Root: root, NvidiaMode: NvidiaOff})
	t0 := time.Unix(1000, 0)
	c.SampleAt(t0)
	snap := c.SampleAt(t0.Add(time.Second))
	if len(snap.Components) != 0 {
		t.Fatalf("malformed sensors produced components: %+v", snap.Components)
	}
	if snap.Source.Name != "Battery" || snap.Source.BatteryPercentage != report.BatteryUnavailable {
		t.Errorf("source = %+v, want Battery/-1", snap.Source)
	}
	if snap.Source.PowerMode != report.PowerModeBattery || snap.Source.BatteryStatus != report.BatteryDischarging {
		t.Errorf("source state = %q/%q, want battery/discharging", snap.Source.PowerMode, snap.Source.BatteryStatus)
	}
	if snap.Source.TotalDrawKind != report.TotalKindNone {
		t.Errorf("total kind = %q, want none", snap.Source.TotalDrawKind)
	}
}

func TestTotalDrawKind(t *testing.T) {
	mk := func(kinds ...string) []report.Component {
		out := make([]report.Component, 0, len(kinds))
		for _, kind := range kinds {
			out = append(out, report.Component{DrawKind: kind})
		}
		return out
	}
	tests := []struct {
		name string
		kind []string
		want string
	}{
		{name: "none", want: report.TotalKindNone},
		{name: "measured", kind: []string{report.DrawKindMeasured}, want: report.TotalKindMeasured},
		{name: "modeled", kind: []string{report.DrawKindModeled}, want: report.TotalKindModeled},
		{name: "mixed", kind: []string{report.DrawKindMeasured, report.DrawKindModeled}, want: report.TotalKindMixed},
		{name: "utilization only", kind: []string{report.DrawKindUtilization}, want: report.TotalKindMixed},
		{name: "measured plus utilization", kind: []string{report.DrawKindMeasured, report.DrawKindUtilization}, want: report.TotalKindMixed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := totalDrawKind(mk(tt.kind...)); got != tt.want {
				t.Fatalf("totalDrawKind(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestParseNvidiaMode(t *testing.T) {
	tests := map[string]NvidiaMode{
		"":      NvidiaAuto,
		"auto":  NvidiaAuto,
		"AUTO":  NvidiaAuto,
		"on":    NvidiaOn,
		"off":   NvidiaOff,
		" off ": NvidiaOff,
	}
	for input, want := range tests {
		got, err := ParseNvidiaMode(input)
		if err != nil {
			t.Errorf("ParseNvidiaMode(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseNvidiaMode(%q) = %v, want %v", input, got, want)
		}
	}
	if _, err := ParseNvidiaMode("sometimes"); err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestParseEstimatesMode(t *testing.T) {
	tests := map[string]EstimatesMode{
		"":      EstimatesAuto,
		"auto":  EstimatesAuto,
		"AUTO":  EstimatesAuto,
		"off":   EstimatesOff,
		" OFF ": EstimatesOff,
	}
	for input, want := range tests {
		got, err := ParseEstimatesMode(input)
		if err != nil {
			t.Errorf("ParseEstimatesMode(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseEstimatesMode(%q) = %v, want %v", input, got, want)
		}
	}
	if _, err := ParseEstimatesMode("sometimes"); err == nil {
		t.Error("expected error for invalid estimates mode")
	}
}
