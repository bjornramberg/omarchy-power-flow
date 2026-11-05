# Provenance

How the collector binaries in `bin/` are produced, how to check them
independently, and where the signed build evidence lives.

## The build contract

Every input below is pinned, and changing any of them changes the produced
bytes and therefore the published hashes.

| Input | Value | Where it is pinned |
| --- | --- | --- |
| Go release | `go1.27.3` | `toolchain` directive in `backend/go.mod`, exported as `GOTOOLCHAIN` by the build scripts |
| `GOEXPERIMENT` | default experiment set | upstream release defaults; the v1.1.0 snapshot used `nodwarf5`, see below |
| `CGO_ENABLED` | `0` | build scripts (static binaries) |
| `GOOS` / `GOARCH` | `linux` / `amd64`, `linux` / `arm64` | `scripts/build-binaries.sh` |
| Build flags | `-buildvcs=false -trimpath -ldflags='-s -w'` | `scripts/build-binaries.sh` |
| Dependencies | none, Go standard library only | `backend/go.mod` has no `require` directives |

`GOTOOLCHAIN` is exported explicitly because the `toolchain` directive is only
a minimum for the `go` command: a newer Go on the host would otherwise be used
silently and produce different bytes. The backend has no external
dependencies, so the build needs no network access.

## Current binaries

Plugin version 1.1.1, built from `backend/` on `main`:

| Path | Size | Git blob | SHA-256 |
| --- | --- | --- | --- |
| `bin/omarchy-power-flow-linux-amd64` | 3371168 | `691a98189bdc3e1254212a4ebce478ef1b8d7630` | `9046e8a7adc92155e6d6a43da483f5166e9d28dda7d3320bd63f22e60f8697b7` |
| `bin/omarchy-power-flow-linux-arm64` | 3211424 | `70894219496ca4854e7bcb53a6a01e13044a0acb` | `f3dd2a662deb4d47054bc961a569252f6b353b522510f24201c23037a434fd27` |

Both are static Go executables built from the source in this repository.

## Independent rebuild

```sh
git clone https://github.com/bjornramberg/omarchy-power-flow.git
cd omarchy-power-flow
cd backend
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    GOTOOLCHAIN=go1.27.3 GOWORK=off \
    go build -buildvcs=false -trimpath -ldflags='-s -w' \
    -o "/tmp/omarchy-power-flow-linux-$arch" ./cmd/omarchy-power-flow
done
sha256sum /tmp/omarchy-power-flow-linux-amd64 /tmp/omarchy-power-flow-linux-arm64
```

Expected output:

```
9046e8a7adc92155e6d6a43da483f5166e9d28dda7d3320bd63f22e60f8697b7  /tmp/omarchy-power-flow-linux-amd64
f3dd2a662deb4d47054bc961a569252f6b353b522510f24201c23037a434fd27  /tmp/omarchy-power-flow-linux-arm64
```

The same check is available as a script, which also compares against the
tracked files and `SHA256SUMS`:

```sh
./scripts/verify-binaries.sh
```

## Continuous verification and attestation

`.github/workflows/verify-binaries.yml` runs on every push to `main`, on every
pull request, and on demand. It rebuilds both collectors with the pinned
toolchain, compares them byte-for-byte with the tracked binaries, checks
`SHA256SUMS`, runs the backend test suite, publishes the rebuilt binaries as
workflow artifacts, and attaches a GitHub build provenance attestation to
each one.

The attestation is signed by GitHub and can be checked by anyone with the
repository checked out:

```sh
gh attestation verify bin/omarchy-power-flow-linux-amd64 \
  --repo bjornramberg/omarchy-power-flow
gh attestation verify bin/omarchy-power-flow-linux-arm64 \
  --repo bjornramberg/omarchy-power-flow
```

The attested subject digest is the SHA-256 above, so verifying the tracked
file also verifies that the file in the repository is the one this build
produced.

The workflow can be run against any commit, which is what published snapshots
need:

```sh
gh workflow run verify-binaries.yml --ref main \
  -f source_ref=a215739296226869f63746aac641935031a87960
```

## The v1.1.0 snapshot (commit a215739)

The v1.1.0 snapshot shipped different collector binaries, built with the DWARF
v5 experiment disabled:

| Path | Git blob | SHA-256 |
| --- | --- | --- |
| `bin/omarchy-power-flow-linux-amd64` | `02ac5e03d2455e1a1fd9993a9f1c2c48d88997c2` | `93fae74777366850ccceb269dd18f335056ce52952ce979eee6d1435900547e9` |
| `bin/omarchy-power-flow-linux-arm64` | `641c693faa38cdb18ba6e65888babab090cb642e` | `fd11e8ae88ecb6b4c2dbb851fb008dda5476c513771e66dbb3f70704d96de3e8` |

The recipe published with that snapshot recorded `CGO_ENABLED=0`,
`-buildvcs=false`, `-trimpath` and `-ldflags='-s -w'`, but not the experiment
and not the toolchain pin, so `scripts/verify-binaries.sh` at `a215739` does
not reproduce those two files on a clean machine. The files themselves are
source-equivalent. Rebuilding `backend/` at `a215739` with the upstream
`go1.27.3` release and the experiment disabled reproduces both of them
byte-for-byte:

```sh
git clone https://github.com/bjornramberg/omarchy-power-flow.git
cd omarchy-power-flow
git checkout a215739296226869f63746aac641935031a87960
cd backend
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    GOTOOLCHAIN=go1.27.3 GOEXPERIMENT=nodwarf5 GOWORK=off \
    go build -buildvcs=false -trimpath -ldflags='-s -w' \
    -o "/tmp/omarchy-power-flow-linux-$arch" ./cmd/omarchy-power-flow
done
sha256sum /tmp/omarchy-power-flow-linux-amd64 /tmp/omarchy-power-flow-linux-arm64
```

Expected output:

```
93fae74777366850ccceb269dd18f335056ce52952ce979eee6d1435900547e9  /tmp/omarchy-power-flow-linux-amd64
fd11e8ae88ecb6b4c2dbb851fb008dda5476c513771e66dbb3f70704d96de3e8  /tmp/omarchy-power-flow-linux-arm64
```

The same rebuild runs in CI:

```sh
gh workflow run verify-binaries.yml --ref main \
  -f source_ref=a215739296226869f63746aac641935031a87960 \
  -f goexperiment=nodwarf5
```

`go1.27.3-X:nodwarf5` in the build information of those two binaries is the
upstream release string for a build with that experiment disabled, not a
locally patched toolchain: the same upstream release reproduces both files
byte-for-byte when `GOEXPERIMENT=nodwarf5` is set, which is what the command
above does.

The v1.1.1 binaries in `bin/` were rebuilt with the default experiment set so
that the published recipe, the CI check and the shipped bytes agree without
any additional flag.
