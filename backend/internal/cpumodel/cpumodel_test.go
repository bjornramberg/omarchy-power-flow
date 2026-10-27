package cpumodel

import (
	"fmt"
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

func cpuinfo(vendor, model string, cores int, hypervisor bool) string {
	var b []byte
	for i := 0; i < cores; i++ {
		flags := "fpu vme"
		if hypervisor {
			flags += " hypervisor"
		}
		b = append(b, []byte(fmt.Sprintf(
			"processor : %d\nvendor_id : %s\nmodel name : %s\ncpu cores : %d\nphysical id : 0\ncore id : %d\nflags : %s\n\n",
			i, vendor, model, cores, i, flags))...)
	}
	return string(b)
}

func TestDetectCPUInfoAndDMI(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	writeFile(t, filepath.Join(root, "proc", "cpuinfo"),
		cpuinfo("AuthenticAMD", "AMD Ryzen 7 5800X 8-Core Processor", 8, false))
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "chassis_type"), "3\n")
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "product_name"), "Desktop\n")

	info := Detect(fs, "amd64")
	if info.Vendor != VendorAMD {
		t.Errorf("vendor = %q, want AMD", info.Vendor)
	}
	if info.Cores != 8 {
		t.Errorf("cores = %d, want 8", info.Cores)
	}
	if info.FormFactor != FormFactorDesktop {
		t.Errorf("form factor = %q, want desktop", info.FormFactor)
	}
	if info.VM {
		t.Error("physical host detected as VM")
	}
	if !info.X86 {
		t.Error("x86 host not detected as x86")
	}
	if info.Model == "" {
		t.Error("model is empty")
	}
}

func TestDetectVMMobileAndNonX86(t *testing.T) {
	root := t.TempDir()
	fs := fsx.New(root)
	writeFile(t, filepath.Join(root, "proc", "cpuinfo"),
		cpuinfo("GenuineIntel", "Intel Core", 4, false))
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "sys_vendor"), "QEMU\n")
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "chassis_type"), "10\n")

	info := Detect(fs, "amd64")
	if !info.VM {
		t.Error("QEMU sys_vendor not detected as VM")
	}
	if info.FormFactor != FormFactorMobile {
		t.Errorf("form factor = %q, want mobile", info.FormFactor)
	}

	arm := Detect(fs, "arm64")
	if arm.X86 {
		t.Error("arm64 detected as x86")
	}
}

func TestStandardPCNameIsNotVM(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "chassis_type"), "3\n")
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "product_name"), "Standard PC\n")
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "sys_vendor"), "ASUSTeK COMPUTER INC.\n")
	writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "bios_vendor"), "American Megatrends Inc.\n")

	if info := Detect(fsx.New(root), "amd64"); info.VM {
		t.Fatal(`physical DMI value "Standard PC" was misclassified as a VM`)
	}
}

func TestSpecificVMMarkersStillDetected(t *testing.T) {
	for _, vendor := range []string{"QEMU", "KVM", "VMware, Inc."} {
		t.Run(vendor, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "sys", "class", "dmi", "id", "sys_vendor"), vendor+"\n")
			if !Detect(fsx.New(root), "amd64").VM {
				t.Fatalf("specific VM marker %q was not detected", vendor)
			}
		})
	}
}

func TestDetectHypervisorFlag(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc", "cpuinfo"),
		cpuinfo("GenuineIntel", "Intel Core", 4, true))

	if !Detect(fsx.New(root), "amd64").VM {
		t.Error("hypervisor cpuinfo flag not detected")
	}
}

func TestFrequencyRatio(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "sys", "devices", "system", "cpu")
	writeFile(t, filepath.Join(base, "cpu0", "cpufreq", "cpuinfo_avg_freq"), "1500000\n")
	writeFile(t, filepath.Join(base, "cpu0", "cpufreq", "cpuinfo_max_freq"), "3000000\n")
	writeFile(t, filepath.Join(base, "cpu0", "cpufreq", "scaling_cur_freq"), "3000000\n")
	writeFile(t, filepath.Join(base, "cpu1", "cpufreq", "scaling_cur_freq"), "3000000\n")
	writeFile(t, filepath.Join(base, "cpu1", "cpufreq", "scaling_max_freq"), "3000000\n")

	ratio, ok := FrequencyRatio(fsx.New(root))
	if !ok || ratio != 0.75 {
		t.Fatalf("ratio = %v/%v, want 0.75/true", ratio, ok)
	}

	// cpuinfo_avg_freq must win over scaling_cur_freq on cpu0; if scaling had
	// been used the average would be 1.0.
	if ratio == 1 {
		t.Fatal("cpuinfo_avg_freq was not preferred")
	}

	if _, ok := FrequencyRatio(fsx.New(t.TempDir())); ok {
		t.Fatal("missing cpufreq produced a ratio")
	}
}

func TestEstimateCPU(t *testing.T) {
	ryzen := Info{
		Vendor: VendorAMD, Model: "AMD Ryzen 7 5800X 8-Core Processor",
		Cores: 8, FormFactor: FormFactorDesktop, X86: true,
	}

	idle, ok := EstimateCPU(ryzen, 0, 1, true)
	if !ok {
		t.Fatal("EstimateCPU returned false")
	}
	if idle.DrawW != 22 || idle.MaxW != 142 {
		t.Errorf("idle = %v/%v, want 22/142", idle.DrawW, idle.MaxW)
	}
	if idle.UncertaintyPercent != 35 {
		t.Errorf("idle uncertainty = %d, want 35", idle.UncertaintyPercent)
	}
	if idle.Class.ID != "ryzen-7-5800x" || idle.Class.TDPW != 105 {
		t.Errorf("class = %+v, want ryzen-7-5800x/105W", idle.Class)
	}

	full, _ := EstimateCPU(ryzen, 100, 1, true)
	if full.DrawW != 142 || full.LoadIndex != 1 {
		t.Errorf("full = %v (load %v), want 142/1", full.DrawW, full.LoadIndex)
	}

	halfFreq, _ := EstimateCPU(ryzen, 100, 0.5, true)
	if halfFreq.DrawW != 64.4 {
		t.Errorf("half-frequency draw = %v, want 64.4", halfFreq.DrawW)
	}
	if halfFreq.DrawW <= idle.DrawW || halfFreq.DrawW >= full.DrawW {
		t.Error("half-frequency draw is not bounded between idle and peak")
	}

	noFreq, _ := EstimateCPU(ryzen, 100, 0, false)
	if noFreq.DrawW != 142 || noFreq.UncertaintyPercent != 50 {
		t.Errorf("no-freq = %v/%d, want 142W/50%%", noFreq.DrawW, noFreq.UncertaintyPercent)
	}
}

func TestEstimateCPUKnown5700XAndGenericClasses(t *testing.T) {
	ryzen5700 := Info{
		Vendor: VendorAMD, Model: "AMD Ryzen 7 5700X 8-Core Processor",
		Cores: 8, FormFactor: FormFactorDesktop, X86: true,
	}
	est, ok := EstimateCPU(ryzen5700, 0, 1, true)
	if !ok || est.DrawW != 20 || est.MaxW != 76 || est.Class.ID != "ryzen-7-5700x" {
		t.Fatalf("5700X = %+v/%v, want idle 20, peak 76", est, ok)
	}

	genericAMD := Info{
		Vendor: VendorAMD, Model: "AMD Ryzen 7 3700X 8-Core Processor",
		Cores: 8, FormFactor: FormFactorDesktop, X86: true,
	}
	est, _ = EstimateCPU(genericAMD, 100, 1, true)
	if est.Class.ID != "amd-desktop-7-12" || est.Class.TDPW != 105 || est.MaxW != 142 {
		t.Errorf("generic AMD class = %+v", est.Class)
	}

	intel := Info{
		Vendor: VendorIntel, Model: "Intel Core i7", Cores: 8,
		FormFactor: FormFactorDesktop, X86: true,
	}
	est, _ = EstimateCPU(intel, 100, 1, true)
	if est.Class.ID != "intel-desktop-7-12" || est.MaxW != 188 || est.DrawW != 188 {
		t.Errorf("generic Intel = %+v, draw %v", est.Class, est.DrawW)
	}

	mobile := Info{
		Vendor: VendorAMD, Model: "AMD Ryzen 7 Mobile", Cores: 8,
		FormFactor: FormFactorMobile, X86: true,
	}
	est, _ = EstimateCPU(mobile, 0, 1, true)
	if est.Class.ID != "amd-mobile-7-12" || est.DrawW != 8 || est.MaxW != 65 {
		t.Errorf("mobile = %+v, draw %v", est.Class, est.DrawW)
	}
}

func TestEstimateCPUUnsupported(t *testing.T) {
	base := Info{
		Vendor: VendorAMD, Model: "AMD Ryzen 7 5800X", Cores: 8,
		FormFactor: FormFactorDesktop, X86: true,
	}
	vm := base
	vm.VM = true
	if _, ok := EstimateCPU(vm, 50, 1, true); ok {
		t.Error("VM produced a watt estimate")
	}

	arm := base
	arm.X86 = false
	if _, ok := EstimateCPU(arm, 50, 1, true); ok {
		t.Error("non-x86 produced a watt estimate")
	}

	noCores := base
	noCores.Cores = 0
	if _, ok := EstimateCPU(noCores, 50, 1, true); ok {
		t.Error("unknown topology produced a watt estimate")
	}
}
