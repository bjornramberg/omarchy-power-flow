#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root/backend"

for arch in amd64 arm64; do
  output="$repo_root/bin/uth-power-linux-$arch"
  printf 'Building %s\n' "$output"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
    -trimpath \
    -ldflags='-s -w' \
    -o "$output" \
    ./cmd/uth-power
  chmod 0755 "$output"
done
