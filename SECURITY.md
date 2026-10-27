# Security

## Trust model

Omarchy plugins run unsandboxed with the permissions of the user who installs
them. Omarchy Power Flow is designed to remain within that user account:

- the QML plugin reads local hardware telemetry and launches only the bundled
  collector;
- the Go collector uses the Go standard library and performs read-only access
  to `/sys`, `/proc`, and `/proc/pressure`;
- it has no network client, credential reader, service manager, or elevated
  execution path;
- NVIDIA telemetry is optional. Automatic discovery is limited to trusted
  absolute system paths, subprocess output is bounded, and execution has a
  two-second timeout.

CPU and RAM power values may be estimates because Linux commonly restricts
hardware energy counters. Those values are explicitly marked and must not be
treated as measurements of a protected rail.

## Bundled binaries

The repository ships static amd64 and arm64 Go collector binaries. They are
built from the `backend/` source in this repository with the standard library
only, `CGO_ENABLED=0`, `-trimpath`, and `-buildvcs=false`.

Verify the tracked binaries against clean builds with the pinned Go toolchain:

```sh
./scripts/verify-binaries.sh
```

Published hashes are recorded in `SHA256SUMS`. A mismatch means the tracked
binary must be reviewed and rebuilt rather than installed blindly.

## Optional configuration helpers

The menu installer, menu remover, and legacy-ID migration script are never run
automatically by `omarchy plugin add`. When explicitly invoked, they:

- operate only inside the user's Omarchy configuration directory;
- create backups before changing files;
- validate JSON/JSONC before replacing a file;
- preserve unrelated configuration;
- serialize menu writes and abort if the target changed concurrently;
- do not request elevated access.

The migration script archives a duplicate legacy plugin directory rather than
discarding it.

## Reporting a vulnerability

Please report suspected vulnerabilities privately through GitHub Security
Advisories:

https://github.com/bjornramberg/omarchy-power-flow/security/advisories/new

Include the affected version or commit, reproduction steps, and the expected
impact. Do not include credentials or other sensitive data in a public issue.

## Scope

This document describes the plugin's intended security properties. It is not a
formal audit or a guarantee that an unsandboxed community plugin is free from
vulnerabilities. Review the source, bundled binaries, permissions, and external
dependencies before installing.
