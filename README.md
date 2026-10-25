# UTH Power

UTH Power is an Omarchy shell plugin that turns live component power readings
into an animated power-flow panel. A source node feeds CPU, GPU, RAM, and disk
nodes through Bezier cords; faster flow represents a larger power draw.

The plugin adds a compact bolt icon to the right side of the Omarchy bar. Activate it to open a bar-anchored power-flow dashboard; dismiss it with Escape, an outside click, or the same bar location. Directional energy packets move from the source toward each component.

## Requirements

- Omarchy with the current Quickshell shell
- Linux with supported hardware telemetry (support is auto-detected)
- `nvidia-smi` is optional and used only for NVIDIA GPUs

No elevated privileges are required. When CPU or RAM watt counters are not
readable, UTH Power uses clearly marked estimates together with exact load and
usage percentages.

## Install

From a published repository:

```sh
omarchy plugin add https://github.com/<owner>/uth-power.git --enable
```

Then open or toggle the panel:

```sh
omarchy-shell shell summon uth.power '{}'
omarchy-shell shell toggle uth.power '{}'
```

Hide it again with:

```sh
omarchy-shell shell hide uth.power
```

A Hyprland binding can summon it directly:

```ini
bind = SUPER, P, exec, omarchy-shell shell toggle uth.power '{}'
```

### Super-key menu entry

Omarchy plugin installation does not copy menu extensions automatically. Add
the optional UTH Power row to the Super-key menu with the idempotent helper:

```sh
./scripts/install-menu-entry.sh
```

The helper updates only `~/.config/omarchy/extensions/omarchy-menu.jsonc`,
backs up the existing file, and refreshes the menu. The mergeable source
fragment is in `extensions/omarchy-menu.jsonc`.

## Local development

Validate the plugin before installing it:

```sh
omarchy plugin validate .
```

For a local checkout, place the repository at the plugin ID path and rescan the
shell:

```sh
git clone <url> ~/.config/omarchy/plugins/uth.power
omarchy-shell shell rescanPlugins
omarchy plugin enable uth.power --section right
```

The bolt icon is placed in the bar's right section. Reposition it with, for
example:

```sh
omarchy bar move uth.power --section right --after omarchy.power
```

If the plugin was previously enabled only through the `plugins[]` list, run
`omarchy plugin disable uth.power` before enabling it as a bar widget.

Saving a file below `~/.config/omarchy/plugins/uth.power/` reloads the plugin.
The currently open panel closes during reload and must be summoned again.

## Build

The repository ships static Go binaries for amd64 and arm64 Linux. `bin/uth-power`
is a small architecture-aware launcher used by the QML panel.

```sh
./scripts/build-binaries.sh
```

Run the backend checks with:

```sh
cd backend
gofmt -l .
go test ./...
go vet ./...
```

## Architecture

```text
manifest.json              Omarchy bar-widget plugin manifest
qml/BarTrigger.qml         Bar icon, lifecycle, process bridge, anchored popup
qml/UthPowerWidget.qml     Dashboard, source hub, consumer tiles, cord layer
qml/AnimatedCord.qml       Directional base path, dash flow, energy packets
backend/                   Go sensor collector and tests
bin/                       Static amd64/arm64 collector binaries and launcher
extensions/                Optional Super-key menu row
scripts/                   Binary build and menu-entry installer
TODO.md                    Optional one-time calibration plan
```

The Go collector emits one compact JSON object per line on standard output.
The QML side parses complete lines and ignores malformed or partial output.
Logs are kept on standard error so they never corrupt the data stream.

## Sensor behavior

| Component | Primary source | Fallback |
| --- | --- | --- |
| CPU | Linux powercap energy zones | A marked estimate from measured CPU load, CPU class, and frequency ratio |
| GPU | DRM hwmon power/energy sensors | `nvidia-smi` for NVIDIA; omitted if no rail is readable |
| RAM | Powercap `dram` energy zones | A marked estimate from installed capacity, memory pressure, and CPU activity |
| Disk | `/proc/diskstats` busy time | A marked estimate from real block-device I/O activity; partitions and virtual devices are excluded |

Measured values always win over estimates. Estimated values are prefixed with
`~` and carry source/uncertainty metadata; utilization-only values are shown
as load/usage without claiming watts.

The source total is the sum of the visible component readings and is therefore
`~`-marked whenever any estimate contributes. It is not a whole-system
wall-plug measurement and does not include unmeasured components.

CPU package energy counters are commonly restricted to root on current Linux
kernels. UTH Power itself needs no elevated privileges, so the CPU fallback is
a load/frequency model rather than a hardware measurement.

### Optional calibration

A future optional calibration workflow may capture machine-specific CPU power
coefficients in a separate, explicitly authorized step while the application
itself continues to run unprivileged. The design constraints are tracked in
`TODO.md`; no calibration or privilege escalation is performed automatically.

## JSON contract

Each stdout line has this shape:

```json
{
  "schema_version": 2,
  "source": {
    "name": "AC",
    "total_draw_w": 153.89,
    "battery_percentage": 87,
    "total_draw_kind": "mixed"
  },
  "components": [
    {
      "id": "cpu",
      "label": "CPU",
      "draw_w": 27.0,
      "max_w": 142.0,
      "activity_percent": 8,
      "draw_kind": "modeled",
      "draw_source": "model:cpu-load/1",
      "utilization_percent": 8,
      "uncertainty_percent": 35,
      "model": {
        "id": "cpu-load/1",
        "assumptions": {}
      }
    },
    {
      "id": "gpu",
      "label": "GPU",
      "draw_w": 124.0,
      "max_w": 135.0,
      "activity_percent": 92,
      "draw_kind": "measured",
      "draw_source": "hwmon:amdgpu"
    }
  ]
}
```

`battery_percentage` is `-1` when no system battery level is available. Stable
component IDs are `cpu`, `gpu`, `ram`, and `disk`. `draw_kind` distinguishes
hardware measurements from estimates and utilization-only values; modeled
components also include uncertainty and model provenance.
