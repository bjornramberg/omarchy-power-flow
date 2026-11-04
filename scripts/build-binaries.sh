#!/usr/bin/env bash
# Build the static linux collector binaries shipped in bin/.
#
# The build is reproducible: the Go toolchain is pinned to the version
# recorded in backend/go.mod, CGO is disabled, the module and VCS paths are
# trimmed, and the symbol table and DWARF data are stripped.  A rebuild with
# the same pinned toolchain produces byte-identical files, which
# scripts/verify-binaries.sh checks and .github/workflows/verify-binaries.yml
# re-checks on every push.
#
# Usage: scripts/build-binaries.sh
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root/backend"

toolchain="$(awk '$1 == "toolchain" { print $2 }' go.mod)"
if [[ -z "$toolchain" ]]; then
  printf 'build-binaries: backend/go.mod does not pin a toolchain\n' >&2
  exit 1
fi

# GOTOOLCHAIN names the exact upstream toolchain.  A machine with a different
# or locally patched Go downloads and uses this one instead, so the produced
# binaries do not depend on the host toolchain build.
export GOTOOLCHAIN="$toolchain"

printf 'build-binaries: using %s\n' "$(go version)"

for arch in amd64 arm64; do
  output="$repo_root/bin/omarchy-power-flow-linux-$arch"
  printf 'Building %s\n' "$output"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
    -buildvcs=false \
    -trimpath \
    -ldflags='-s -w' \
    -o "$output" \
    ./cmd/omarchy-power-flow
  chmod 0755 "$output"
done

printf 'build-binaries: updating SHA256SUMS\n'
(
  cd "$repo_root"
  sha256sum \
    bin/omarchy-power-flow-linux-amd64 \
    bin/omarchy-power-flow-linux-arm64 \
    > SHA256SUMS
)
cat "$repo_root/SHA256SUMS"
