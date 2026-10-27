#!/usr/bin/env bash
# Remove the optional Omarchy Power Flow row from the user's Omarchy menu
# extension.
#
# This is a thin, explicit wrapper around:
#   scripts/install-menu-entry.sh --remove
#
# It only edits ~/.config/omarchy/extensions/omarchy-menu.jsonc, backs the file
# up first, validates the resulting JSONC, preserves unrelated rows/comments,
# refreshes the menu, and exits cleanly when the row is already absent. It
# never uses sudo and never writes below /usr/share.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec -- "$script_dir/install-menu-entry.sh" --remove "$@"
