#!/usr/bin/env bash
# Shared build environment for the collector binaries.
#
# Every value pinned here is part of the reproducible build contract.  Changing
# any of them changes the produced bytes and therefore SHA256SUMS.  The tracked
# binaries in bin/ are byte-identical to a build of backend/ with exactly these
# values; scripts/verify-binaries.sh proves it on every run.
#
# Source this file, do not execute it.

if [[ -n "${OMARCHY_POWER_FLOW_BUILD_ENV_LOADED:-}" ]]; then
  return 0 2>/dev/null || exit 0
fi
OMARCHY_POWER_FLOW_BUILD_ENV_LOADED=1

build_env_repo_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)

# Exact Go release recorded in backend/go.mod.  The `toolchain` directive is
# only a minimum for the `go` command, so a newer local Go would otherwise be
# used silently and produce different bytes.
build_env_toolchain=$(awk '$1 == "toolchain" { print $2; exit }' "$build_env_repo_root/backend/go.mod")
if [[ -z "$build_env_toolchain" ]]; then
  printf 'build-env: backend/go.mod has no toolchain directive\n' >&2
  return 1 2>/dev/null || exit 1
fi

# The tracked binaries were built with DWARF v5 debug information disabled.
# Leaving the experiment at its default produces different bytes.
export GOTOOLCHAIN="$build_env_toolchain"
export GOEXPERIMENT=nodwarf5
export CGO_ENABLED=0
export GOFLAGS=
export GOWORK=off

unset build_env_repo_root build_env_toolchain
