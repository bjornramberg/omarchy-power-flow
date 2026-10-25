package cpumodel

import (
	"math"
	"strings"
)

// Class is a conservative CPU power class from the model table.
type Class struct {
	ID         string
	Vendor     Vendor
	FormFactor FormFactor
	Cores      int
	TDPW       float64
	IdleW      float64
	PeakW      float64
	Exact      bool
}

// Estimate is a modeled CPU power reading.
type Estimate struct {
	DrawW              float64
	MaxW               float64
	UncertaintyPercent int
	Class              Class
	LoadIndex          float64
	Assumptions        map[string]any
}

// EstimateCPU models package power from measured busy utilization and an
// optional frequency ratio.  It returns false for VMs, non-x86 hosts or
// systems where no CPU topology could be read; callers should fall back to
// utilization-only output in that case.
func EstimateCPU(info Info, utilPercent int, freqRatio float64, freqOK bool) (Estimate, bool) {
	if info.VM || !info.X86 || info.Cores <= 0 {
		return Estimate{}, false
	}

	vendor := info.Vendor
	unknownVendor := false
	if vendor != VendorAMD && vendor != VendorIntel {
		vendor = VendorIntel
		unknownVendor = true
	}
	form := info.FormFactor
	if form != FormFactorMobile {
		form = FormFactorDesktop
	}

	class := exactClass(vendor, form, info.Model)
	if class.ID == "" {
		class = genericClass(vendor, form, info.Cores)
	}
	class.Cores = info.Cores

	util := clamp01(float64(utilPercent) / 100)
	load := util
	if freqOK {
		ratio := clamp01(freqRatio)
		load = util * math.Pow(ratio, 1.5)
	}
	load = clamp01(load)

	draw := class.IdleW + (class.PeakW-class.IdleW)*load
	draw = math.Max(class.IdleW, math.Min(class.PeakW, draw))

	uncertainty := 50
	if class.Exact {
		uncertainty = 35
	}
	if unknownVendor {
		uncertainty = 60
	}
	if !freqOK {
		uncertainty += 15
	}
	if uncertainty > 80 {
		uncertainty = 80
	}

	formula := "idle + (peak-idle) * util"
	assumptions := map[string]any{
		"vendor":         string(info.Vendor),
		"form_factor":    string(form),
		"class":          class.ID,
		"cores":          info.Cores,
		"tdp_w":          class.TDPW,
		"idle_w":         class.IdleW,
		"peak_w":         class.PeakW,
		"exact_model":    class.Exact,
		"frequency_used": freqOK,
		"formula":        formula,
	}
	if unknownVendor {
		assumptions["unknown_vendor"] = true
		assumptions["class_vendor"] = string(VendorIntel)
	}
	if freqOK {
		ratio := clamp01(freqRatio)
		assumptions["frequency_ratio"] = round3(ratio)
		assumptions["formula"] = "idle + (peak-idle) * clamp(util * freq_ratio^1.5)"
	}

	return Estimate{
		DrawW:              round1(draw),
		MaxW:               round1(class.PeakW),
		UncertaintyPercent: uncertainty,
		Class:              class,
		LoadIndex:          load,
		Assumptions:        assumptions,
	}, true
}

func exactClass(vendor Vendor, form FormFactor, modelName string) Class {
	if vendor != VendorAMD || form != FormFactorDesktop {
		return Class{}
	}
	model := strings.ToLower(modelName)
	switch {
	case strings.Contains(model, "5800x") && !strings.Contains(model, "5800x3d"):
		return Class{
			ID: "ryzen-7-5800x", Vendor: vendor, FormFactor: form,
			TDPW: 105, IdleW: 22, PeakW: 142, Exact: true,
		}
	case strings.Contains(model, "5700x") && !strings.Contains(model, "5700x3d"):
		return Class{
			ID: "ryzen-7-5700x", Vendor: vendor, FormFactor: form,
			TDPW: 65, IdleW: 20, PeakW: 76, Exact: true,
		}
	}
	return Class{}
}

func genericClass(vendor Vendor, form FormFactor, cores int) Class {
	bucket := "7-12"
	switch {
	case cores <= 6:
		bucket = "le6"
	case cores > 12:
		bucket = "gt12"
	}
	prefix := string(vendor)
	if form == FormFactorMobile {
		prefix += "-mobile-"
	} else {
		prefix += "-desktop-"
	}

	t := table(vendor, form, cores)
	t.ID = prefix + bucket
	t.Vendor = vendor
	t.FormFactor = form
	return t
}

func table(vendor Vendor, form FormFactor, cores int) Class {
	if form == FormFactorMobile {
		if vendor == VendorAMD {
			switch {
			case cores <= 6:
				return Class{TDPW: 35, IdleW: 6, PeakW: 55}
			case cores <= 12:
				return Class{TDPW: 45, IdleW: 8, PeakW: 65}
			default:
				return Class{TDPW: 55, IdleW: 10, PeakW: 80}
			}
		}
		switch {
		case cores <= 6:
			return Class{TDPW: 35, IdleW: 5, PeakW: 60}
		case cores <= 12:
			return Class{TDPW: 45, IdleW: 7, PeakW: 70}
		default:
			return Class{TDPW: 55, IdleW: 9, PeakW: 85}
		}
	}

	if vendor == VendorAMD {
		switch {
		case cores <= 6:
			return Class{TDPW: 65, IdleW: 15, PeakW: 88}
		case cores <= 12:
			return Class{TDPW: 105, IdleW: 22, PeakW: 142}
		default:
			return Class{TDPW: 170, IdleW: 30, PeakW: 230}
		}
	}
	switch {
	case cores <= 6:
		return Class{TDPW: 65, IdleW: 9, PeakW: 90}
	case cores <= 12:
		return Class{TDPW: 125, IdleW: 11, PeakW: 188}
	default:
		return Class{TDPW: 150, IdleW: 15, PeakW: 220}
	}
}

func clamp01(v float64) float64 {
	if !math.IsNaN(v) && v > 0 {
		if v > 1 {
			return 1
		}
		return v
	}
	return 0
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
