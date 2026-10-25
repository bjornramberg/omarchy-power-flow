// Package cpumodel detects CPU identity/topology and produces an explicitly
// modeled CPU power estimate when no hardware watt sensor is available.
//
// The model is intentionally coarse.  It uses measured busy utilization, a
// frequency ratio where cpufreq exposes it, and a small table of conservative
// class defaults.  It is not a replacement for RAPL/amd_energy measurements.
package cpumodel

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"uthpower/backend/internal/fsx"
)

// Vendor identifies the x86 CPU vendor.
type Vendor string

const (
	VendorAMD     Vendor = "amd"
	VendorIntel   Vendor = "intel"
	VendorUnknown Vendor = "unknown"
)

// FormFactor distinguishes desktop/server-ish chassis from portable systems.
type FormFactor string

const (
	FormFactorDesktop FormFactor = "desktop"
	FormFactorMobile  FormFactor = "mobile"
)

// Info is static CPU identity/topology information gathered once.
type Info struct {
	Vendor     Vendor
	Model      string
	Cores      int
	FormFactor FormFactor
	VM         bool
	X86        bool
}

var portableChassis = map[int]bool{
	8: true, 9: true, 10: true, 11: true, 14: true,
	30: true, 31: true, 32: true,
}

var desktopChassis = map[int]bool{
	3: true, 4: true, 5: true, 6: true, 7: true, 13: true, 15: true,
	16: true, 17: true, 23: true, 24: true, 25: true, 28: true,
	29: true, 33: true, 34: true, 35: true, 36: true,
}

var vmMarkers = []string{
	"qemu", "kvm", "virtualbox", "innotek", "vmware", "xen", "hyper-v",
	"bochs", "bhyve", "parallels", "openstack", "amazon ec2",
	"google compute", "digitalocean", "linode", "nutanix", "q35",
}

// Detect reads static CPU identity, DMI chassis/VM information and the host
// architecture.  arch is normally runtime.GOARCH; passing it in keeps tests
// independent of the build host.
func Detect(fs *fsx.Root, arch string) Info {
	info := Info{
		Vendor:     VendorUnknown,
		FormFactor: FormFactorDesktop,
		X86:        arch == "amd64" || arch == "386",
	}
	if fs == nil {
		return info
	}

	vendor, modelName, cores, hypervisor := parseCPUInfo(read(fs, "proc", "cpuinfo"))
	info.Vendor = vendor
	info.Model = modelName
	info.Cores = cores
	info.VM = hypervisor || detectVM(fs)
	info.FormFactor = detectFormFactor(fs)
	return info
}

func parseCPUInfo(data string) (Vendor, string, int, bool) {
	blocks := splitCPUBlocks(data)
	if len(blocks) == 0 {
		return VendorUnknown, "", 0, false
	}

	vendor := VendorUnknown
	modelName := ""
	hypervisor := false
	maxCPUCores := 0
	processors := 0
	physicalIDs := map[string]bool{}
	physicalCores := map[string]bool{}

	for _, block := range blocks {
		if block["processor"] != "" {
			processors++
		}
		if flags := strings.ToLower(block["flags"]); strings.Contains(flags, "hypervisor") {
			hypervisor = true
		}
		if vendor == VendorUnknown {
			switch strings.ToLower(block["vendor_id"]) {
			case "authenticamd":
				vendor = VendorAMD
			case "genuineintel":
				vendor = VendorIntel
			}
		}
		if modelName == "" {
			modelName = block["model name"]
		}
		if v, err := strconv.Atoi(block["cpu cores"]); err == nil && v > maxCPUCores {
			maxCPUCores = v
		}
		physicalID := block["physical id"]
		coreID := block["core id"]
		if physicalID != "" {
			physicalIDs[physicalID] = true
		}
		if physicalID != "" && coreID != "" {
			physicalCores[physicalID+"/"+coreID] = true
		}
	}

	cores := 0
	switch {
	case len(physicalCores) > 0:
		cores = len(physicalCores)
	case maxCPUCores > 0:
		cores = maxCPUCores
		if len(physicalIDs) > 1 {
			cores *= len(physicalIDs)
		}
	case processors > 0:
		cores = processors
	}
	return vendor, modelName, cores, hypervisor
}

func splitCPUBlocks(data string) []map[string]string {
	var out []map[string]string
	current := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(current) > 0 {
				out = append(out, current)
				current = make(map[string]string)
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, exists := current[key]; !exists {
			current[key] = strings.TrimSpace(value)
		}
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

func detectVM(fs *fsx.Root) bool {
	for _, part := range [][]string{
		{"sys", "class", "dmi", "id", "sys_vendor"},
		{"sys", "class", "dmi", "id", "product_name"},
		{"sys", "class", "dmi", "id", "board_vendor"},
		{"sys", "class", "dmi", "id", "bios_vendor"},
		{"sys", "hypervisor", "type"},
	} {
		value := strings.ToLower(read(fs, part...))
		for _, marker := range vmMarkers {
			if strings.Contains(value, marker) {
				return true
			}
		}
	}
	return false
}

func detectFormFactor(fs *fsx.Root) FormFactor {
	chassis, _ := strconv.Atoi(strings.TrimSpace(read(fs, "sys", "class", "dmi", "id", "chassis_type")))
	switch {
	case portableChassis[chassis]:
		return FormFactorMobile
	case desktopChassis[chassis]:
		return FormFactorDesktop
	}

	product := strings.ToLower(read(fs, "sys", "class", "dmi", "id", "product_name"))
	for _, marker := range []string{
		"laptop", "notebook", "thinkpad", "ideapad", "macbook", "portable", "ultrabook",
	} {
		if strings.Contains(product, marker) {
			return FormFactorMobile
		}
	}
	return FormFactorDesktop
}

// FrequencyRatio returns the mean current/max cpufreq ratio in [0, 1].
// cpuinfo_avg_freq is preferred; scaling_cur_freq is the fallback.
func FrequencyRatio(fs *fsx.Root) (float64, bool) {
	if fs == nil {
		return 0, false
	}
	entries, err := fs.ReadDir("sys", "devices", "system", "cpu")
	if err != nil {
		return 0, false
	}

	sum := 0.0
	count := 0
	for _, entry := range entries {
		if !isCPUName(entry.Name()) {
			continue
		}
		dir := fs.Sys("devices", "system", "cpu", entry.Name(), "cpufreq")
		current, ok := readKHz(filepath.Join(dir, "cpuinfo_avg_freq"))
		if !ok || current <= 0 {
			current, ok = readKHz(filepath.Join(dir, "scaling_cur_freq"))
		}
		if !ok || current <= 0 {
			continue
		}
		maximum, ok := readKHz(filepath.Join(dir, "cpuinfo_max_freq"))
		if !ok || maximum <= 0 {
			maximum, ok = readKHz(filepath.Join(dir, "scaling_max_freq"))
		}
		if !ok || maximum <= 0 {
			continue
		}
		ratio := current / maximum
		if ratio < 0 {
			ratio = 0
		} else if ratio > 1 {
			ratio = 1
		}
		sum += ratio
		count++
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}

func isCPUName(name string) bool {
	if !strings.HasPrefix(name, "cpu") || len(name) == 3 {
		return false
	}
	for _, r := range name[3:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func readKHz(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	return v, true
}

func read(fs *fsx.Root, parts ...string) string {
	data, err := fs.ReadFile(parts...)
	if err != nil {
		return ""
	}
	return string(data)
}
