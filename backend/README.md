# UTH Power backend

A small, Linux-only, standard-library Go collector for **UTH Power**. It reads
hardware power/activity sensors and writes one compact NDJSON object per line to
stdout, one line per interval (1 second by default). The Quickshell frontend
consumes those lines directly.

No third-party modules, no network access, and no configuration files are
required.

## Build

```sh
cd backend
go build -o uth-power ./cmd/uth-power
```

The binary is self-contained. Install it anywhere on `PATH`, for example:

```sh
install -Dm755 uth-power ~/.local/bin/uth-power
```

## Usage

```sh
uth-power                 # stream one JSON line per second
uth-power --once          # one line and exit (verification / scripting)
uth-power --interval 2s   # slower output
uth-power --estimates off # measured values only (no CPU/RAM fallback)
uth-power --root /tmp/fake-root --once   # hermetic sensor test
```

Flags and environment:

| Flag | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `--root` | `UTH_POWER_ROOT` | `/` | Root containing `sys/` and `proc/` |
| `--interval` | - | `1s` | Streaming sample/output interval |
| `--once` | - | `false` | Emit one object and exit |
| `--warmup` | - | `250ms` | Prime delta sensors before the first output |
| `--nvidia` | `UTH_POWER_NVIDIA` | `auto` | `auto`, `on`, or `off` |
| `--nvidia-smi` | - | `nvidia-smi` | Path to the NVIDIA tool |
| `--estimates` | `UTH_POWER_ESTIMATES` | `auto` | `auto` or `off` |

`--root` (or `UTH_POWER_ROOT`) prefixes both `/sys` and `/proc` lookups. This is
intended for tests and verification against a fake sysfs/procfs tree. In
`auto` mode, `--root` also disables `nvidia-smi`, keeping verification
hermetic. Use `--nvidia on` explicitly if a fake-root run should execute the
real tool.

The collector takes an initial warm-up sample and discards it. Delta-based
sources (RAPL/AMD energy counters, GPU energy counters, `/proc/diskstats` io
ticks, `/proc/stat` jiffies) need two samples, so the first emitted line is
already complete. With `--warmup 0`, `--once` can only report instantaneous
sensors.

## Schema v2

Schema v2 is additive: the v1 field names and measured numeric semantics are
preserved. `schema_version` is always first, and existing source/component
fields keep their order. New metadata fields are appended.

```json
{"schema_version":2,"source":{"name":"AC","total_draw_w":157.05,"battery_percentage":87,"total_draw_kind":"mixed"},"components":[{"id":"cpu","label":"CPU","draw_w":64.4,"max_w":142,"activity_percent":100,"draw_kind":"modeled","draw_source":"model:cpu-load/1","utilization_percent":100,"uncertainty_percent":35,"model":{"id":"cpu-load/1","assumptions":{"class":"ryzen-7-5800x","cores":8,"exact_model":true,"form_factor":"desktop","formula":"idle + (peak-idle) * clamp(util * freq_ratio^1.5)","frequency_ratio":0.5,"frequency_used":true,"idle_w":22,"peak_w":142,"tdp_w":105,"vendor":"amd"}}},{"id":"gpu","label":"GPU","draw_w":85,"max_w":300,"activity_percent":28,"draw_kind":"measured","draw_source":"hwmon:amdgpu"},{"id":"ram","label":"RAM","draw_w":2.4,"max_w":5.6,"activity_percent":0,"draw_kind":"modeled","draw_source":"model:ram-capacity/1","utilization_percent":50,"uncertainty_percent":50,"model":{"id":"ram-capacity/1","assumptions":{"active_w_per_gib":0.35,"capacity_gib":16,"cpu_available":true,"formula":"idle + (max-idle) * clamp(0.75 * mem_pressure_avg10 + 0.25 * cpu_util)","idle_w_per_gib":0.15,"pressure_available":true}}},{"id":"disk","label":"DISK","draw_w":5.25,"max_w":10,"activity_percent":50,"draw_kind":"modeled","draw_source":"model:disk-busy/1","uncertainty_percent":50,"model":{"id":"disk-busy/1","assumptions":{"activity":"sum(io_ticks delta) / elapsed","full_w":10,"idle_w":0.5}}}]}
```

Top level contains exactly `schema_version`, `source` and `components`.

`source`:

- `name` — display label. `AC` when external power is online (or a battery
  reports Charging/Full), `Battery` when running from a battery, and
  `PSU / Battery` when no power-supply information is available.
- `total_draw_w` — arithmetic sum of all emitted **numeric** component
  `draw_w` values (measured plus modeled), rounded to two decimals.
  Utilization-only components contribute nothing. It is **not** whole-system
  wall power, **not** battery/AC input power, and does not include omitted
  components. The `name`/`battery_percentage` fields are independent
  power-supply telemetry and are never mixed into this total.
- `battery_percentage` — integer `0..100`, or **`-1` when no usable battery
  capacity can be read**. `-1` is the sentinel, never a real level.
- `total_draw_kind` — `measured` when every component is measured, `modeled`
  when every component is modeled, `none` when there are no components, and
  `mixed` otherwise. Utilization-only components therefore make the total
  `mixed`.

`components` is always an array, ordered `cpu`, `gpu`, `ram`, `disk`. Each
object contains:

| Field | Meaning |
| --- | --- |
| `id`, `label` | Stable id and display label |
| `draw_w` | Watts, or `null` only for utilization-only components |
| `max_w` | Scaling value, or `null` only for utilization-only components |
| `activity_percent` | Legacy design field, integer `0..100` |
| `draw_kind` | `measured`, `modeled`, or `utilization` |
| `draw_source` | Stable short provenance, e.g. `powercap:rapl`, `amd_energy`, `hwmon:amdgpu`, `nvidia-smi`, `proc:stat`, `model:cpu-load/1` |
| `utilization_percent` | Optional measured load/occupancy, integer `0..100` |
| `uncertainty_percent` | Mandatory for modeled components; omitted otherwise |
| `model` | Optional stable model object (`id` + concise `assumptions`) |

Measured and modeled components always carry numeric `draw_w`/`max_w`.
Utilization-only components are the only ones allowed to emit JSON `null` for
both. A modeled component must carry `uncertainty_percent` and a `model`.

## Measured values always win

Selection order per component:

1. Powercap/`amd_energy` CPU watts.
2. Powercap DRAM or `amd_energy` DRAM RAM watts.
3. GPU hwmon or `nvidia-smi` watts.
4. Disk activity estimate (always modeled).
5. CPU/RAM models only when no measured watts exist and
   `--estimates auto` is active.

A measured component never carries model metadata. `--estimates off` suppresses
CPU/RAM modeled and utilization-only fallbacks, while measured components and
the existing disk estimate keep working. CPU utilization from `/proc/stat` is
still used for measured CPU activity when available.

## Sensor sources

| Component | Source | Units / conversion |
| --- | --- | --- |
| CPU | `/sys/class/powercap/*` zones named `package-*` (Intel RAPL and modern AMD), fallback `amd_energy` hwmon | energy delta in µJ / 1e6 / seconds = W |
| RAM | powercap `dram` zones or `amd_energy` DRAM channels | same µJ delta conversion |
| GPU | `/sys/class/hwmon/hwmon*/power1_input` or `power1_average` for known GPU drivers | µW / 1e6 = W |
| GPU | Intel `i915`/`xe` `energy1_input` when no power attribute is usable | µJ delta / 1e6 / seconds = W |
| GPU | `nvidia-smi --query-gpu=power.draw,power.limit,utilization.gpu --format=csv,noheader,nounits` | already W |
| Disk | `/proc/diskstats` field 13 (`io_ticks`, milliseconds), gated by `/sys/block/<name>` | busy ms / elapsed ms = activity |
| CPU activity/utilization | `/proc/stat` aggregate `cpu` line | busy jiffy delta / total jiffy delta |
| RAM usage | `/proc/meminfo` `MemTotal`, `MemAvailable` | (total-available)/total |
| RAM activity hint | `/proc/pressure/memory` `some avg10` | percent, combined weakly |
| Battery / AC | `/sys/class/power_supply/*` (`type`, `capacity`, `status`, `online`, `present`) | percent |

Details and edge handling:

- **RAPL/powercap** — zones are classified by their `name` attribute, never by
  numeric directory names: `package-*` counts as CPU, `dram` as RAM, and
  `psys` is skipped because it covers the whole machine. `core`/`uncore`/`gt`
  and unknown names are skipped to avoid double counting package energy. This
  same path works for modern AMD RAPL drivers. Counter wrap at
  `max_energy_range_uj` is reconstructed.
- **RAPL permissions** — `energy_uj` is commonly readable only by root. A
  permission error (like any read error) is a clean "unavailable" state: the
  measured component is omitted, nothing is fabricated, and the process keeps
  running.
- **RAPL `enabled`** — the zone `enabled` file controls power capping, not
  energy monitoring. `enabled=0` does not suppress discovery or measurement.
- **AMD `amd_energy`** — package channels win when present, otherwise
  core/unknown channels are summed. DRAM channels are always RAM. A decreasing
  counter is treated as a reset and that interval is skipped.
- **GPU hwmon** — both `power1_input` and `power1_average` are probed (µW).
  Intel `i915`/`xe` may expose only `energy1_input` (µJ); that is sampled as a
  delta and converted to average watts. Only known GPU drivers are accepted.
  Multiple GPUs are aggregated.
- **NVIDIA** — `nvidia-smi` output is parsed only when the process exits `0`
  and numeric CSV fields parse; diagnostics printed to stdout on a failed run
  are ignored. When it succeeds, NVIDIA hwmon entries (`nouveau`/`nvidia`) are
  skipped to avoid double counting. It has a 2 second timeout and failures are
  silent.
- **Battery** — multiple system batteries are averaged, weighted by
  `energy_full`/`charge_full`. Peripheral batteries (`scope=Device`) and
  non-present batteries are ignored. If `capacity` is absent,
  `energy_now/energy_full` or `charge_now/charge_full` is used.
- **CPU activity** — idle and iowait both count as idle. If `/proc/stat` is
  unavailable, measured CPU activity is derived from `draw_w / max_w`.
- **GPU activity** — uses `utilization.gpu` from `nvidia-smi` when available;
  otherwise derived from `draw_w / max_w`.

Every sensor read is best-effort. Missing directories, permission errors,
malformed numbers, NaN/Inf, short lines, counter resets and disappearing
devices are ignored rather than terminating the process.

## CPU model (`model:cpu-load/1`)

When no measured CPU watts are available and estimates are enabled, the
collector uses:

```
util_fraction = busy_percent / 100
frequency_ratio = clamp(mean(current_freq / max_freq), 0, 1)
load_index = clamp(util_fraction * frequency_ratio^1.5, 0, 1)
draw_w = idle_w + (peak_w - idle_w) * load_index
```

`cpuinfo_avg_freq` is preferred over `scaling_cur_freq`, with
`cpuinfo_max_freq`/`scaling_max_freq` as the max. If cpufreq is unavailable,
utilization is used directly and uncertainty increases.

Static detection uses `/proc/cpuinfo` (vendor, model, physical core count) and
DMI chassis type (`/sys/class/dmi/id/chassis_type`) for desktop vs mobile.
Conservative class defaults:

| Class | TDP | Idle | Peak | Uncertainty |
| --- | --- | --- | --- | --- |
| AMD desktop 7–12 cores | 105 W | 22 W | 142 W | 50% |
| Intel desktop 7–12 cores | 125 W | 11 W | 188 W | 50% |
| AMD desktop ≤6 / >12 | 65 / 170 W | 15 / 30 W | 88 / 230 W | 50% |
| Intel desktop ≤6 / >12 | 65 / 150 W | 9 / 15 W | 90 / 220 W | 50% |
| AMD mobile ≤6 / 7–12 / >12 | 35 / 45 / 55 W | 6 / 8 / 10 W | 55 / 65 / 80 W | 50% |
| Intel mobile ≤6 / 7–12 / >12 | 35 / 45 / 55 W | 5 / 7 / 9 W | 60 / 70 / 85 W | 50% |

Small exact-model refinements:

- AMD Ryzen 7 5800X (8c desktop): TDP 105 W, idle 22 W, peak 142 W.
- AMD Ryzen 7 5700X (8c desktop): TDP 65 W, idle 20 W, peak 76 W.

Exact classes use 35% uncertainty; classes without frequency data add 15 points
(capped at 80%). Modeled CPU watts are rounded to one decimal. `max_w` is the
class peak. The assumptions object records class, cores, TDP/idle/peak,
frequency use, and the formula.

VMs (DMI/hypervisor markers) and non-x86 hosts do not get a watt model. If
`/proc/stat` is available they emit a utilization-only component with
`draw_kind: "utilization"`, `draw_w: null`, `max_w: null`, and exact
`utilization_percent`. If `/proc/stat` is unavailable, CPU is omitted rather
than inventing a load.

## RAM model (`model:ram-capacity/1`)

With no measured DRAM watts and estimates enabled:

- Capacity from `/proc/meminfo` `MemTotal`; occupancy is
  `(MemTotal - MemAvailable) / MemTotal` and is exposed exactly as
  `utilization_percent`.
- Coefficients: idle `0.15 W/GiB`, active `0.35 W/GiB`; `max_w` is capacity ×
  active. A 16 GiB host therefore models about 2.4 W idle and 5.6 W max.
- A weak activity index drives watts, never occupancy directly:
  `0.75 * mem_pressure_avg10 + 0.25 * cpu_util` when
  `/proc/pressure/memory` is available, otherwise `0.25 * cpu_util`.
- `draw_w = idle + (max - idle) * activity`, clamped.
- Uncertainty is about 50% (55% when pressure or CPU utilization is missing).
- `MemTotal` is required. Without it, or without `MemAvailable`, RAM is omitted
  rather than fabricated.

For modeled RAM, `activity_percent` is the weak activity index used by the watt
model; `utilization_percent` remains exact memory occupancy.

## Disk estimate (`model:disk-busy/1`)

Exact disk wattage is not available from the kernel. The disk component is
emitted only when at least one real whole block device is identifiable. A
candidate from `/proc/diskstats` is accepted only if `/sys/block/<name>` exists,
has a `device` attribute, and has no `partition` attribute. Virtual devices
(`loop*`, `zram*`, `dm-*`, `md*`, `ram*`, optical `sr*`, `fd*`, `nbd*`) and
partitions are excluded.

Activity is the summed `io_ticks` delta across accepted devices divided by
elapsed milliseconds, clamped to 100%. The documented linear estimate is
unchanged from v1:

```
draw_w = 0.5 + (10.0 - 0.5) * activity_percent / 100
```

so an idle disk reports about 0.5 W and a fully busy disk up to 10 W. This is
always marked `draw_kind: "modeled"`, `draw_source: "model:disk-busy/1"`, with
about 50% uncertainty. It is not a measured rail.

## Scaling fallbacks (`max_w`)

Measured components without a hardware cap use design-document values for
frontend scaling only:

| Component | Fallback max |
| --- | --- |
| CPU | 150 W |
| GPU | 300 W per uncapped GPU |
| RAM | 15 W |
| Disk | 10 W estimate |

These are never used as measured `draw_w`. Modeled components use their model
peak/active maximum instead.

## Tests

```sh
cd backend
gofmt -l .        # should print nothing
go test -count=1 ./...
go vet ./...
```

Tests use temporary fake roots and never require real hardware. They cover the
v2 contract and field ordering, null-only-utilization semantics, measured vs
modeled provenance, measured-wins selection, CPU model fixtures (idle, full
load, frequency, no-frequency, VM/non-x86/missing proc stat), RAM model fixtures
(16 GiB idle/busy, occupancy does not scale watts, missing MemTotal), disk
numbers unchanged and marked modeled, total-kind consistency, `--estimates=off`,
powercap `enabled=0` monitoring, plus the v1 sensor parsing/filtering tests.

## Layout

```
backend/
  cmd/uth-power/          CLI, flags, streaming/one-shot loop
  internal/collector/     sensor orchestration and component assembly
  internal/report/        v2 JSON contract (struct order is output order)
  internal/units/         explicit µJ/µW conversions, rounding, clamping
  internal/fsx/           root-relative /sys and /proc access
  internal/powercap/      name-based powercap energy zones (Intel RAPL/AMD)
  internal/amdenergy/     AMD amd_energy hwmon channels
  internal/hwmon/         GPU hwmon power and Intel energy counters
  internal/nvidia/        nvidia-smi runner and CSV parser
  internal/diskstats/     sysfs-gated /proc/diskstats activity estimate
  internal/cpustat/       /proc/stat CPU utilization
  internal/powersupply/   battery capacity and AC state
  internal/cpumodel/      CPU identity/DMI/cpufreq detection + watt model
  internal/rammodel/      /proc/meminfo/pressure + RAM watt model
```
