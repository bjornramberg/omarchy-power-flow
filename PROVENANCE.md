# Provenance

This document describes how the binaries in `bin/` are produced, how to check
them independently, and where the signed build evidence lives.

## What is shipped

| Path | Size | Git blob | SHA-256 |
| --- | --- | --- | --- |
| `bin/omarchy-power-flow-linux-amd64` | 3371168 | `02ac5e03d2455e1a1fd9993a9f1c2c48d88997c2` | `93fae74777366850ccceb269dd18f335056ce52952ce979eee6d1435900547e9` |
| `bin/omarchy-power-flow-linux-arm64` | 3211424 | `641c693faa38cdb18ba6e65888babab090cb642e` | `fd11e8ae88ecb6b4c2dbb851fb008dda5476c513771e66dbb3f70704d96de3e8` |
| `bin/omarchy-power-flow` | 503 | `1f8b272e580186d1cc9cbf5bb284d196a42c7da0` | shell launcher, not a binary |

Both collectors are static, dependency-free Go executables built from
`backend/` in this repository.

## Build contract

Every input below is pinned in `scripts/build-env.sh`, which both
`scripts/build-binaries.sh` and `scripts/verify-binaries.sh` source. Changing
any of them changes the produced bytes and therefore the hashes above.

| Input | Value | Source of the value |
| --- | --- | --- |
| Go release | `go1.27.3` | `toolchain` directive in `backend/go.mod` |
| `GOEXPERIMENT` | `nodwarf5` | `scripts/build-env.sh` (DWARF v5 debug info disabled) |
| `CGO_ENABLED` | `0` | `scripts/build-env.sh` (static binaries) |
| `GOOS` / `GOARCH` | `linux` / `amd64`, `linux` / `arm64` | `scripts/build-binaries.sh` |
| Build flags | `-buildvcs=false -trimpath -ldflags='-s -w'` | `scripts/build-binaries.sh` |
| `GOWORK` | `off` | `scripts/build-env.sh` |
| Dependencies | none, Go standard library only | `backend/go.mod` has no `require` directives |

The `toolchain` directive in `backend/go.mod` is only a minimum for the `go`
command, so `GOTOOLCHAIN` is pinned explicitly: a newer local Go would
otherwise be used silently and produce different bytes.

## Independent rebuild

The following reproduces both shipped binaries byte-for-byte from a clean
checkout. It needs no network access, because the backend has no external
dependencies.

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

The same check is available as a script, which also compares against the
tracked files and `SHA256SUMS`:

```sh
./scripts/verify-binaries.sh
```

## Continuous verification

`.github/workflows/verify-binaries.yml` runs on every push to `main`, on every
pull request, and on demand. It rebuilds both collectors with the pinned
environment, compares them byte-for-byte with the tracked binaries, checks
`SHA256SUMS`, runs the backend test suite, and publishes a build provenance
attestation for the rebuilt binaries.

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

The workflow can also be run against an older commit, for example the commit a
marketplace snapshot was taken from. It accepts a `baseline_ref` input that
fails the run unless `backend/` and `bin/` are identical to that commit:

```sh
gh workflow run verify-binaries.yml --ref main \
  -f baseline_ref=a215739296226869f63746aac641935031a87960
```

## History of the build recipe

The recipe published with the v1.1.0 snapshot (commit `a215739`) documented
`CGO_ENABLED=0`, `-buildvcs=false`, `-trimpath` and `-ldflags='-s -w'`, but
omitted the `GOEXPERIMENT=nodwarf5` setting and did not pin the Go release.
The binaries themselves were built with that experiment enabled, so
`scripts/verify-binaries.sh` as published did not reproduce the shipped files
on a clean machine.

The tracked binaries are unchanged and are source-equivalent: rebuilding
`backend/` at `a215739` with the contract above reproduces both blobs
byte-for-byte. `scripts/build-env.sh` closes the gap by recording the
experiment and pinning the toolchain, and the workflow above keeps the claim
honest on every commit.
