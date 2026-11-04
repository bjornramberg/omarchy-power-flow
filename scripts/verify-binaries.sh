#!/usr/bin/env bash
# Verify that the tracked linux binaries match a clean build with the same
# flags and toolchain used by scripts/build-binaries.sh.
#
# Both architectures are rebuilt into a temporary directory (the tracked files
# in bin/ are only ever read), then compared byte-for-byte.  The temporary
# directory is removed on exit, including on failure.
#
# Usage: scripts/verify-binaries.sh [--output-dir DIR]
#
#   --output-dir DIR  keep the rebuilt binaries in DIR instead of discarding
#                     them.  Continuous integration uses this to publish the
#                     rebuilt files as artifacts and to attest their build
#                     provenance.
#
# The toolchain is pinned to the version recorded in backend/go.mod, so the
# check does not depend on which Go happens to be installed on the host.
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
backend_dir="$repo_root/backend"
bin_dir="$repo_root/bin"

output_dir=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      if [[ $# -lt 2 ]]; then
        printf 'verify-binaries: --output-dir requires a directory\n' >&2
        exit 1
      fi
      output_dir="$2"
      shift 2
      ;;
    --output-dir=*)
      output_dir="${1#*=}"
      shift
      ;;
    -h | --help)
      sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      printf 'verify-binaries: unknown argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

for tool in go cmp sha256sum; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'verify-binaries: required tool not found: %s\n' "$tool" >&2
    exit 1
  fi
done

toolchain="$(awk '$1 == "toolchain" { print $2 }' "$backend_dir/go.mod")"
if [[ -z "$toolchain" ]]; then
  printf 'verify-binaries: backend/go.mod does not pin a toolchain\n' >&2
  exit 1
fi
export GOTOOLCHAIN="$toolchain"

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

  rebuilt_sha="$(sha256sum -- "$rebuilt" | cut -d' ' -f1)"
  tracked_sha="$(sha256sum -- "$tracked" | cut -d' ' -f1)"

  if cmp -s -- "$tracked" "$rebuilt"; then
    printf 'verify-binaries: OK %s matches a clean build\n' "${tracked#"$repo_root"/}"
    printf '  sha256: %s\n' "$rebuilt_sha"
    if [[ -n "$output_dir" ]]; then
      mkdir -p -- "$output_dir"
      cp -- "$rebuilt" "$output_dir/$(basename -- "$tracked")"
      chmod 0755 "$output_dir/$(basename -- "$tracked")"
      printf '  copied to %s\n' "$output_dir/$(basename -- "$tracked")"
    fi
  else
    printf 'verify-binaries: MISMATCH %s does not match a clean build\n' "${tracked#"$repo_root"/}" >&2
    printf '  tracked sha256: %s\n' "$tracked_sha" >&2
    printf '  rebuilt sha256: %s\n' "$rebuilt_sha" >&2
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
