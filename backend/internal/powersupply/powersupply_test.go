package powersupply

import (
	"os"
	"path/filepath"
	"testing"

	"uthpower/backend/internal/fsx"
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
	if got.BatteryPercent != 87 {
		t.Errorf("battery = %d, want 87", got.BatteryPercent)
	}
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
	if got.BatteryPercent != 42 {
		t.Errorf("battery = %d, want 42", got.BatteryPercent)
	}
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
	if got.BatteryPercent != -1 {
		t.Errorf("battery = %d, want -1 sentinel", got.BatteryPercent)
	}
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
	if got.Name != NameFallback || got.BatteryPercent != -1 {
		t.Fatalf("got %+v, want fallback/-1", got)
	}

	empty := Read(fsx.New(t.TempDir()))
	if empty.Name != NameFallback || empty.BatteryPercent != -1 {
		t.Fatalf("missing power_supply = %+v, want fallback/-1", empty)
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

func TestFullWithoutOnlineMeansAC(t *testing.T) {
	root := t.TempDir()
	addSupply(t, root, "BAT0", map[string]string{
		"type": "Battery", "capacity": "100", "status": "Full",
	})

	got := Read(fsx.New(root))
	if got.Name != NameAC {
		t.Errorf("name = %q, want %q", got.Name, NameAC)
	}
}
