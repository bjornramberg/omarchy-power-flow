# Omarchy Power Flow

![Omarchy Power Flow](preview.png)

An Omarchy shell plugin that turns live component power readings into an
animated power-flow panel. A source node feeds CPU, GPU, RAM, and disk nodes
through Bezier cords; faster flow and wider strokes represent a larger power
draw.

## Requirements

- **Omarchy with the current Quickshell shell** — the only runtime platform
  dependency.
- **Linux hardware telemetry** — support is auto-detected for each component.
  Measured sensors are used when readable; anything else is clearly marked as
  an estimate or shown as load/usage only.
- **`nvidia-smi` (optional)** — used only to read NVIDIA GPU power when no DRM
  power rail is available. It is not needed on AMD, Intel, or other systems.
- **Unprivileged by design** — installation, removal, and normal operation all
  run as your user. No elevated access is requested.
- **No runtime language dependency** — the collector ships as prebuilt static
  amd64/arm64 binaries in `bin/`; Go, Python, and other language runtimes do
  not need to be installed to run the plugin.

## Installation

Install from the official public repository:

```sh
omarchy plugin add https://github.com/bjornramberg/omarchy-power-flow.git --enable
```

The plugin is enabled as a bar widget in the right section. Activate the bolt
icon to open the power-flow panel; dismiss it with Escape, an outside click, or
the same bar location.

## Removal

Remove the plugin by its permanent ID:

```sh
omarchy plugin remove io.github.bjornramberg.omarchy-power-flow
```

## Architecture

```text
manifest.json              Omarchy bar-widget plugin manifest
qml/PowerFlow.qml          Bar icon, lifecycle, process bridge, anchored popup
qml/PowerFlowWidget.qml    Dashboard, source hub, consumer tiles, cord layer
qml/AnimatedCord.qml       Continuous watt-share gradient and energy packets
backend/                   Go sensor collector and tests
bin/                       Static amd64/arm64 collector binaries and launcher
scripts/                   Build and binary-verification helpers
SECURITY.md                Security model, build provenance, and reporting
DEVELOPMENT.md             Local development, build, sensor, and JSON details
SHA256SUMS                 Published hashes for bundled collector binaries
```

## License

Omarchy Power Flow is released under the [MIT License](LICENSE).

Copyright (c) 2026 Björn Ramberg.
