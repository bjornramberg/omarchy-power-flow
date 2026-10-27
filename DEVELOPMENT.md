# Development

This guide covers local development, building, validation, and implementation
details for Omarchy Power Flow. For the security model and reporting process,
see [SECURITY.md](SECURITY.md); for published binary hashes, see
[SHA256SUMS](SHA256SUMS).

## Local development

Validate the plugin before installing it:

```sh
omarchy plugin validate .
```

For a local checkout, place the repository at the plugin ID path and rescan the
shell:

```sh
git clone <url> ~/.config/omarchy/plugins/io.github.bjornramberg.omarchy-power-flow
omarchy-shell shell rescanPlugins
omarchy plugin enable io.github.bjornramberg.omarchy-power-flow --section right
```

The bolt icon is placed in the bar's right section. Reposition it with, for
example:

```sh
omarchy bar move io.github.bjornramberg.omarchy-power-flow --section right --after omarchy.power
```

If the plugin was previously enabled only through the `plugins[]` list, run
`omarchy plugin disable io.github.bjornramberg.omarchy-power-flow` before
enabling it as a bar widget.

Saving a file below
`~/.config/omarchy/plugins/io.github.bjornramberg.omarchy-power-flow/` reloads
the plugin. The currently open panel closes during reload and must be summoned
again.

## Build and verify binaries

The repository ships static Go binaries for amd64 and arm64 Linux.
`bin/omarchy-power-flow` is a small architecture-aware launcher used by the QML
panel.

```sh
./scripts/build-binaries.sh
./scripts/verify-binaries.sh
sha256sum -c SHA256SUMS
```

`build-binaries.sh` builds both architectures with `CGO_ENABLED=0`, `-trimpath`,
`-buildvcs=false`, and stripped symbols. `verify-binaries.sh` rebuilds both
architectures with the pinned Go toolchain and compares them byte-for-byte with
the tracked binaries. A mismatch means the tracked binary must be reviewed and
rebuilt rather than installed blindly.

## Backend checks

Run these from the backend module:

```sh
cd backend
gofmt -l .        # should print nothing
go test -count=1 ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench '^BenchmarkHostScan$' -benchtime=20x ./internal/procmetrics
```

Tests use temporary fake roots and never require real hardware. See
[backend/README.md](backend/README.md) for module layout and per-package
details.

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
kernels. The plugin itself needs no elevated privileges, so the CPU fallback is
a load/frequency model rather than a hardware measurement.

## Process insights

Hovering a component exposes the most useful process-level signal Linux can
provide without privileges:

- **CPU:** the leading process by machine CPU share, with both percentage of
  the machine and percentage of one CPU core.
- **RAM:** the process with the largest resident set (RSS).

These are activity indicators, not power attribution. Linux does not expose
per-process DRAM or GPU watts, and process I/O permissions are incomplete, so
the plugin intentionally does not make disk/GPU process-power claims.

The collector performs one bounded `/proc/<pid>/stat` scan per second, capped at
about 1200 PIDs and a 10 ms wall-clock budget. It measures about 5 ms per scan
on a roughly 460-process host. Disable it with:

```sh
./bin/omarchy-power-flow --processes off
```

## Reliability and diagnostics

The collector and panel fail independently and visibly:

- Backend panics are recovered, reported with a bounded stderr diagnostic, and
  exit with code 70; stdout is never allowed to contain partial JSON.
- The panel validates schema-v2 samples before replacing good telemetry.
- Missing, crashing, or unresponsive collectors use bounded exponential
  backoff, a four-second data watchdog, and a user-visible retry/error state.
- The last good reading remains visible and is marked stale rather than being
  silently replaced with zeros.

Useful checks:

```sh
omarchy plugin validate .
./bin/omarchy-power-flow --once
journalctl --user -t omarchy-shell -f
```

Collector exit codes are `0` for clean shutdown, `1` for output failure, `2`
for invalid arguments, and `70` for an internal panic.

## JSON contract (schema v2)

Each stdout line is one JSON object. A compact example:

```json
{"schema_version":2,"source":{"name":"AC","total_draw_w":153.89,"battery_percentage":87,"total_draw_kind":"mixed","power_mode":"ac","battery_status":"charging"},"components":[{"id":"cpu","label":"CPU","draw_w":27.0,"max_w":142.0,"activity_percent":8,"draw_kind":"modeled","draw_source":"model:cpu-load/1","utilization_percent":8,"uncertainty_percent":35,"model":{"id":"cpu-load/1","assumptions":{}},"top_process":{"pid":1234,"name":"firefox","metric":"cpu","value":6.25,"unit":"percent_of_machine","core_percent":100.0,"window_ms":1000,"scope":"visible_processes","source":"proc:stat","ok":true}},{"id":"gpu","label":"GPU","draw_w":124.0,"max_w":135.0,"activity_percent":92,"draw_kind":"measured","draw_source":"hwmon:amdgpu"}]}
```

`battery_percentage` is `-1` when no system battery level is available.
`power_mode` explicitly reports `ac`, `battery`, or `unknown`, and the UI
chooses the plug, battery, or generic power glyph from that state. Stable
component IDs are `cpu`, `gpu`, `ram`, and `disk`. `draw_kind` distinguishes
hardware measurements from estimates and utilization-only values; modeled
components also include uncertainty and model provenance. `top_process` is
optional activity metadata and never represents watts.

The UI parses complete lines and ignores malformed or partial output. Collector
logs go to stderr so they never corrupt the data stream.
