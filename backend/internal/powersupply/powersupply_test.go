package powersupply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bjornramberg/omarchy-power-flow/backend/internal/fsx"
)

func addSupply(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "sys", "class", "power_supply", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, value := range files {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func requireStatus(t *testing.T, got Status, mode, batteryStatus string, percent int) {
	t.Helper()
	if got.PowerMode != mode {
		t.Errorf("power mode = %q, want %q", got.PowerMode, mode)
	}
	if got.BatteryStatus != batteryStatus {
		t.Errorf("battery status = %q, want %q", got.BatteryStatus, batteryStatus)
	}
	if got.BatteryPercent != percent {
		t.Errorf("battery = %d, want %d", got.BatteryPercent, percent)
	}
}

func TestACOnline(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "AC", map[string]string{"type": "Mains", "online": "1"})
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "87", "status": "Charging",
	})

	got := Read(fsx.New(root))
	if got.Name != NameAC {
		t.Errorf("name = %q, want %q", got.Name, NameAC)
	}
	requireStatus(t, got, ModeAC, BatteryCharging, 87)
}

func TestACWithoutBattery(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "ADP1", map[string]string{"type": "Mains", "online": "1"})

	got := Read(fsx.New(root))
	if got.Name != NameAC {
		t.Errorf("name = %q, want %q", got.Name, NameAC)
	}
	requireStatus(t, got, ModeAC, "", -1)
}

func TestOnBattery(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "AC", map[string]string{"type": "Mains", "online": "0"})
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "42", "status": "Discharging",
	})

	got := Read(fsx.New(root))
	if got.Name != NameBattery {
		t.Errorf("name = %q, want %q", got.Name, NameBattery)
	}
	requireStatus(t, got, ModeBattery, BatteryDischarging, 42)
}

func TestDischargingWinsOverOnlineSupply(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "ADP1", map[string]string{"type": "Mains", "online": "1"})
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "50", "status": "Discharging",
	})

	got := Read(fsx.New(root))
	requireStatus(t, got, ModeBattery, BatteryDischarging, 50)
	if got.Name != NameBattery {
		t.Errorf("name = %q, want %q", got.Name, NameBattery)
	}
}

func TestChargingWithoutACSensorImpliesAC(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "55", "status": "Charging",
	})

	got := Read(fsx.New(root))
	// Implied AC must not rewrite the battery's charging status.
	requireStatus(t, got, ModeAC, BatteryCharging, 55)
	if got.Name != NameAC {
		t.Errorf("name = %q, want %q", got.Name, NameAC)
	}
}

func TestFullWithoutOnlineSupplyIsUnknown(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "100", "status": "Full",
	})

	got := Read(fsx.New(root))
	requireStatus(t, got, ModeUnknown, BatteryFull, 100)
	if got.Name != NameUnknown {
		t.Errorf("name = %q, want %q", got.Name, NameUnknown)
	}
}

func TestNotChargingWithoutOnlineSupplyIsUnknown(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "80", "status": "Not charging",
	})

	got := Read(fsx.New(root))
	requireStatus(t, got, ModeUnknown, BatteryNotCharging, 80)
}

func TestUnknownBatteryStatus(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "50", "status": "Mystery",
	})

	got := Read(fsx.New(root))
	requireStatus(t, got, ModeUnknown, BatteryUnknown, 50)
}

func TestWeightedAverage(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "80", "energy_full": "100", "status": "Discharging",
	})
	addSupply(t, root, "BAT1", map[string]string{
		"type": "Battery", "capacity": "40", "energy_full": "300", "status": "Discharging",
	})

	got := Read(fsx.New(root))
	if got.BatteryPercent != 50 {
		t.Errorf("battery = %d, want 50", got.BatteryPercent)
	}
	requireStatus(t, got, ModeBattery, BatteryDischarging, 50)
}

func TestEnergyFallback(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "energy_now": "37000000", "energy_full": "50000000",
		"status": "Discharging",
	})

	got := Read(fsx.New(root))
	if got.BatteryPercent != 74 {
		t.Errorf("battery = %d, want 74", got.BatteryPercent)
	}
}

func TestUnknownBatteryCapacity(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "status": "Discharging",
	})

	got := Read(fsx.New(root))
	if got.Name != NameBattery {
		t.Errorf("name = %q, want %q", got.Name, NameBattery)
	}
	requireStatus(t, got, ModeBattery, BatteryDischarging, -1)
}

func TestDoesNotInferACFromDeviceName(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "AC", map[string]string{"type": "Mains", "online": "0"})
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "30", "status": "Discharging",
	})

	got := Read(fsx.New(root))
	requireStatus(t, got, ModeBattery, BatteryDischarging, 30)
}

func TestIgnoresPeripheralBatteryAndMissing(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "hidpp_battery_0", map[string]string{
		"type": "Battery", "scope": "Device", "capacity": "90",
	})
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "present": "0", "capacity": "90",
	})

	got := Read(fsx.New(root))
	if got.Name != NameUnknown || got.PowerMode != ModeUnknown || got.BatteryStatus != "" || got.BatteryPercent != -1 {
		t.Fatalf("got %+v, want unknown/-1 with no battery status", got)
	}

	empty := Read(fsx.New(t.TempDir()))
	if empty.Name != NameUnknown || empty.PowerMode != ModeUnknown || empty.BatteryStatus != "" || empty.BatteryPercent != -1 {
		t.Fatalf("missing power_supply = %+v, want unknown/-1", empty)
	}
}

func TestCapacityClamped(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "105", "status": "Discharging",
	})

	got := Read(fsx.New(root))
	if got.BatteryPercent != 100 {
		t.Errorf("battery = %d, want 100", got.BatteryPercent)
	}
}

func TestMultipleBatteryStatusAggregation(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "50", "status": "Charging",
	})
	addSupply(t, root, "BAT1", map[string]string{
		"type": "Battery", "capacity": "50", "status": "Discharging",
	})

	got := Read(fsx.New(root))
	requireStatus(t, got, ModeBattery, BatteryDischarging, 50)
}
