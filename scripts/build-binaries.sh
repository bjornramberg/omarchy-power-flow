#!/usr/bin/env bash
# Build the static linux collector binaries.
#
# The build is pinned by scripts/build-env.sh: an exact Go release, an exact
# experiment set and no cgo.  With that environment the output is byte-identical
# to the tracked binaries in bin/ and to the hashes in SHA256SUMS.
#
# Usage: scripts/build-binaries.sh [--out-dir DIR]
#
# Without --out-dir the binaries are written to bin/, replacing the tracked
# files.  Pass --out-dir to build elsewhere, for example to compare a fresh
# build against the tracked files without touching them.
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/build-env.sh
source "$repo_root/scripts/build-env.sh"

out_dir="$repo_root/bin"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-dir)
      if [[ $# -lt 2 ]]; then
        printf 'build-binaries: --out-dir needs a directory argument\n' >&2
        exit 1
      fi
      out_dir=$2
      shift 2
      ;;
    *)
      printf 'build-binaries: unknown argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

mkdir -p -- "$out_dir"
cd "$repo_root/backend"

for arch in amd64 arm64; do
  output="$out_dir/omarchy-power-flow-linux-$arch"
  printf 'Building %s\n' "$output"
  GOOS=linux GOARCH="$arch" go build \
    -buildvcs=false \
    -trimpath \
    -ldflags='-s -w' \
    -o "$output" \
    ./cmd/omarchy-power-flow
  chmod 0755 "$output"
done
