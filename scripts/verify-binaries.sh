#!/usr/bin/env bash
# Verify that the tracked linux binaries match a clean build with the same
# flags and toolchain used by scripts/build-binaries.sh.
#
# Both architectures are rebuilt into a temporary directory (the tracked files
# in bin/ are only ever read), then compared byte-for-byte.  The temporary
# directory is removed on exit, including on failure.
#
# Usage: scripts/verify-binaries.sh
#
# Requires the same Go toolchain that produced the tracked binaries.  Run this
# after `scripts/build-binaries.sh` and commit the refreshed binaries.
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
backend_dir="$repo_root/backend"
bin_dir="$repo_root/bin"

for tool in go cmp sha256sum; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'verify-binaries: required tool not found: %s\n' "$tool" >&2
    exit 1
  fi
done

tmp_parent="${TMPDIR:-/tmp/opencode}"
mkdir -p -- "$tmp_parent"
tmpdir=$(mktemp -d "$tmp_parent/omarchy-power-flow-verify.XXXXXX") ||
  {
    printf 'verify-binaries: cannot create a temporary directory under %s\n' "$tmp_parent" >&2
    exit 1
  }

cleanup() {
  rm -rf -- "$tmpdir"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

printf 'verify-binaries: using %s\n' "$(go version)"

status=0
for arch in amd64 arm64; do
  tracked="$bin_dir/omarchy-power-flow-linux-$arch"
  rebuilt="$tmpdir/omarchy-power-flow-linux-$arch"

  if [[ ! -f "$tracked" ]]; then
    printf 'verify-binaries: tracked binary is missing: %s\n' "$tracked" >&2
    status=1
    continue
  fi

  printf 'verify-binaries: rebuilding linux/%s\n' "$arch"
  if ! (
    cd "$backend_dir"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
      -buildvcs=false \
      -trimpath \
      -ldflags='-s -w' \
      -o "$rebuilt" \
      ./cmd/omarchy-power-flow
  ); then
    printf 'verify-binaries: build failed for linux/%s\n' "$arch" >&2
    status=1
    continue
  fi

  if cmp -s -- "$tracked" "$rebuilt"; then
    printf 'verify-binaries: OK %s matches a clean build\n' "${tracked#"$repo_root"/}"
  else
    printf 'verify-binaries: MISMATCH %s does not match a clean build\n' "${tracked#"$repo_root"/}" >&2
    printf '  tracked sha256: %s\n' "$(sha256sum -- "$tracked" | cut -d' ' -f1)" >&2
    printf '  rebuilt sha256: %s\n' "$(sha256sum -- "$rebuilt" | cut -d' ' -f1)" >&2
    status=1
  fi
done

if [[ "$status" -ne 0 ]]; then
  printf '%s\n' \
    'verify-binaries: tracked binaries are not reproducible with this toolchain.' \
    'Run scripts/build-binaries.sh using the same Go version, then commit the result.' >&2
  exit 1
fi

printf 'verify-binaries: all tracked binaries match clean builds\n'
