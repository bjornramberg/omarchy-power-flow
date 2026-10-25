package diskstats

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"uthpower/backend/internal/fsx"
)

func diskLine(name string, ioTicks uint64) string {
	return fmt.Sprintf("8 0 %s 1000 0 10000 100 500 0 5000 50 0 %d 0 0 0 0 0 0",
		name, ioTicks)
}

func writeDiskstats(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, "proc", "diskstats")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func addBlockDevice(t *testing.T, root, name string, device, partition bool) {
	t.Helper()
	dir := filepath.Join(root, "sys", "block", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if device {
		if err := os.MkdirAll(filepath.Join(dir, "device"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if partition {
		if err := os.WriteFile(filepath.Join(dir, "partition"), []byte("1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseReadsAllIoTicks(t *testing.T) {
	data := []byte(
		diskLine("sda", 1234) + "\n" +
			diskLine("sda1", 9999) + "\n" +
			diskLine("nvme0n1", 42) + "\n" +
			"malformed line\n" +
			"8 0 sdb 1 2 3\n")
	got := Parse(data)
	if len(got) != 3 {
		t.Fatalf("parsed %d devices, want 3: %#v", len(got), got)
	}
	if got["sda"] != 1234 || got["sda1"] != 9999 || got["nvme0n1"] != 42 {
		t.Fatalf("unexpected io_ticks map: %#v", got)
	}
}

func TestRealDeviceSysfsFiltering(t *testing.T) {
	root := t.TempDir()
	addBlockDevice(t, root, "sda", true, false)
	addBlockDevice(t, root, "nvme0n1", true, false)
	addBlockDevice(t, root, "mmcblk0", true, false)
	addBlockDevice(t, root, "sda1", true, true)
	addBlockDevice(t, root, "nvme0n1p1", true, true)
	addBlockDevice(t, root, "loop0", true, false)
	addBlockDevice(t, root, "dm-0", true, false)
	addBlockDevice(t, root, "zram0", true, false)
	addBlockDevice(t, root, "md0", true, false)
	addBlockDevice(t, root, "sr0", true, false)
	addBlockDevice(t, root, "sdc", false, false) // no device attribute

	m := New(fsx.New(root))
	for _, name := range []string{"sda", "nvme0n1", "mmcblk0"} {
		if !m.realDevice(name) {
			t.Errorf("realDevice(%q) = false, want true", name)
		}
	}
	for _, name := range []string{
		"sda1", "nvme0n1p1", "loop0", "dm-0", "zram0", "md0", "sr0", "sdc", "sdb",
	} {
		if m.realDevice(name) {
			t.Errorf("realDevice(%q) = true, want false", name)
		}
	}
}

func TestSampleDeltaAndEstimate(t *testing.T) {
	root := t.TempDir()
	addBlockDevice(t, root, "sda", true, false)
	addBlockDevice(t, root, "sda1", true, true)
	addBlockDevice(t, root, "nvme0n1", true, false)
	addBlockDevice(t, root, "nvme0n1p1", true, true)
	addBlockDevice(t, root, "loop0", true, false)

	writeDiskstats(t, root,
		diskLine("sda", 1000)+"\n"+
			diskLine("sda1", 5000)+"\n"+
			diskLine("nvme0n1", 2000)+"\n"+
			diskLine("nvme0n1p1", 5000)+"\n"+
			diskLine("loop0", 5000)+"\n")

	m := New(fsx.New(root))
	t0 := time.Unix(1000, 0)
	first := m.Sample(t0)
	if first.OK {
		t.Fatalf("first sample must only prime: %+v", first)
	}

	writeDiskstats(t, root,
		diskLine("sda", 1500)+"\n"+
			diskLine("sda1", 9000)+"\n"+
			diskLine("nvme0n1", 2200)+"\n"+
			diskLine("nvme0n1p1", 9000)+"\n"+
			diskLine("loop0", 9000)+"\n")

	res := m.Sample(t0.Add(time.Second))
	if !res.OK {
		t.Fatalf("expected disk measurement")
	}
	// 500 ms from sda plus 200 ms from nvme0n1 over 1000 ms = 70%.
	if res.ActivityPercent != 70 {
		t.Errorf("activity = %d, want 70", res.ActivityPercent)
	}
	if res.DrawW != 7.15 {
		t.Errorf("draw = %v, want 7.15", res.DrawW)
	}
	if res.MaxW != DefaultMaxW {
		t.Errorf("max = %v, want %v", res.MaxW, DefaultMaxW)
	}
}

func TestSampleIgnoresDevicesWithoutSysfs(t *testing.T) {
	root := t.TempDir()
	addBlockDevice(t, root, "sda", true, false)
	writeDiskstats(t, root,
		diskLine("sda", 1000)+"\n"+
			diskLine("sdb", 1000)+"\n"+
			diskLine("dm-0", 1000)+"\n")

	m := New(fsx.New(root))
	t0 := time.Unix(1000, 0)
	m.Sample(t0)
	writeDiskstats(t, root,
		diskLine("sda", 1500)+"\n"+
			diskLine("sdb", 9000)+"\n"+
			diskLine("dm-0", 9000)+"\n")
	res := m.Sample(t0.Add(time.Second))
	if !res.OK || res.ActivityPercent != 50 {
		t.Fatalf("result = %+v, want 50%% from sda only", res)
	}
}

func TestSampleIdleAndReset(t *testing.T) {
	root := t.TempDir()
	addBlockDevice(t, root, "sda", true, false)
	writeDiskstats(t, root, diskLine("sda", 1000)+"\n")
	m := New(fsx.New(root))
	t0 := time.Unix(1000, 0)
	m.Sample(t0)

	writeDiskstats(t, root, diskLine("sda", 1000)+"\n")
	res := m.Sample(t0.Add(time.Second))
	if !res.OK || res.ActivityPercent != 0 || res.DrawW != IdleDrawW {
		t.Fatalf("idle result = %+v, want activity 0 and draw %v", res, IdleDrawW)
	}

	// A decreasing counter is clamped rather than producing negative power.
	writeDiskstats(t, root, diskLine("sda", 500)+"\n")
	res = m.Sample(t0.Add(2 * time.Second))
	if !res.OK || res.ActivityPercent != 0 {
		t.Fatalf("reset result = %+v, want activity 0", res)
	}
}

func TestSampleNoRealDevicesAndMissingFile(t *testing.T) {
	root := t.TempDir()
	m := New(fsx.New(root))
	if res := m.Sample(time.Now()); res.OK {
		t.Fatalf("missing diskstats produced data: %+v", res)
	}

	addBlockDevice(t, root, "loop0", true, false)
	addBlockDevice(t, root, "dm-0", true, false)
	addBlockDevice(t, root, "zram0", true, false)
	writeDiskstats(t, root,
		diskLine("loop0", 10)+"\n"+
			diskLine("dm-0", 10)+"\n"+
			diskLine("zram0", 10)+"\n")
	if res := m.Sample(time.Now()); res.OK {
		t.Fatalf("virtual-only diskstats produced data: %+v", res)
	}
}
