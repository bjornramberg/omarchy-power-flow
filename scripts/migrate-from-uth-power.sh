#!/usr/bin/env bash
# One-time migration from the legacy "UTH Power" identity to "Omarchy Power Flow".
#
# What it changes (only when needed):
#   * ~/.config/omarchy/plugins/uth.power
#       -> ~/.config/omarchy/plugins/io.github.bjornramberg.omarchy-power-flow
#   * the "uth.power" plugin id in ~/.config/omarchy/shell.json
#   * the legacy "uth-power" row in ~/.config/omarchy/extensions/omarchy-menu.jsonc
#
# It never uses sudo, backs up every file it rewrites, refuses to replace an
# existing new plugin directory unless that directory's manifest declares the
# new plugin id, and can be rerun safely.
#
# Run this from an up-to-date checkout of the new release. If the installed
# plugin directory still contains the old code/manifest, update that checkout
# first (for example, `git pull` there) so the new ID is present after the move.
set -euo pipefail

readonly SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
readonly OLD_ID="uth.power"
readonly NEW_ID="io.github.bjornramberg.omarchy-power-flow"
readonly OLD_MENU_KEY="uth-power"
readonly NEW_MENU_KEY="omarchy-power-flow"

readonly OLD_DIR="${HOME:+$HOME/.config/omarchy/plugins/uth.power}"
readonly NEW_DIR="${HOME:+$HOME/.config/omarchy/plugins/io.github.bjornramberg.omarchy-power-flow}"
readonly SHELL_JSON="${HOME:+$HOME/.config/omarchy/shell.json}"
readonly MENU_EXT="${HOME:+$HOME/.config/omarchy/extensions/omarchy-menu.jsonc}"

actions=()
warnings=()
failures=0

say() {
  printf 'migrate-from-uth-power: %s\n' "$*"
}

warn() {
  printf 'migrate-from-uth-power: warning: %s\n' "$*" >&2
  warnings+=("$*")
}

fail() {
  printf 'migrate-from-uth-power: error: %s\n' "$*" >&2
  exit 1
}

if [[ -z "${HOME:-}" ]]; then
  fail "HOME is not set."
fi

if ! command -v python3 >/dev/null 2>&1; then
  fail "python3 is required but was not found."
fi

# Validate shell.json before changing anything so a malformed file cannot leave
# the plugin directory moved but the configuration half-migrated.
if [[ -f "$SHELL_JSON" ]]; then
  python3 - "$SHELL_JSON" <<'PY' ||
import json
import sys

try:
    with open(sys.argv[1], "r", encoding="utf-8") as handle:
        json.load(handle)
except Exception as exc:
    print(f"migrate-from-uth-power: error: malformed {sys.argv[1]}: {exc}", file=sys.stderr)
    raise SystemExit(1)
PY
    exit 1
fi

backup_path() {
  local path=$1
  local stamp candidate
  local suffix=1
  stamp=$(date +%Y%m%d-%H%M%S)
  candidate="${path}.bak.${stamp}"
  while [[ -e "$candidate" ]]; do
    candidate="${path}.bak.${stamp}.${suffix}"
    suffix=$((suffix + 1))
  done
  printf '%s' "$candidate"
}

manifest_id() {
  python3 - "$1" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], "r", encoding="utf-8") as handle:
        document = json.load(handle)
except Exception as exc:
    print(f"cannot read manifest: {exc}", file=sys.stderr)
    raise SystemExit(1)

if not isinstance(document, dict):
    print("manifest root is not an object", file=sys.stderr)
    raise SystemExit(1)
print(document.get("id", ""))
PY
}

# ---------- 1. Plugin directory ----------
if [[ -e "$OLD_DIR" ]]; then
  if [[ -e "$NEW_DIR" ]]; then
    if [[ ! -f "$NEW_DIR/manifest.json" ]]; then
      fail "refusing to replace $NEW_DIR: it exists but has no manifest.json"
    fi
    existing_id=$(manifest_id "$NEW_DIR/manifest.json") ||
      fail "refusing to replace $NEW_DIR: its manifest is unreadable"
    if [[ "$existing_id" != "$NEW_ID" ]]; then
      fail "refusing to replace $NEW_DIR: manifest id is '$existing_id', expected '$NEW_ID'"
    fi

    rm -rf -- "$OLD_DIR"
    actions+=("removed stale legacy plugin directory $OLD_DIR (new directory already valid)")
  else
    mv -T -- "$OLD_DIR" "$NEW_DIR"
    actions+=("moved $OLD_DIR -> $NEW_DIR")

    moved_id=$(manifest_id "$NEW_DIR/manifest.json" 2>/dev/null || true)
    if [[ "$moved_id" != "$NEW_ID" ]]; then
      warn "$NEW_DIR/manifest.json reports id '${moved_id:-unknown}'. Update the installed checkout to the new release if the plugin does not load."
    fi
  fi
else
  actions+=("legacy plugin directory not found; directory move skipped")
fi

# ---------- 2. shell.json plugin id ----------
if [[ -f "$SHELL_JSON" ]]; then
  tmp=$(mktemp -- "${SHELL_JSON}.tmp.XXXXXX") ||
    fail "cannot create a temporary file next to $SHELL_JSON"
  cp -p -- "$SHELL_JSON" "$tmp" || {
    rm -f -- "$tmp"
    fail "cannot stage $SHELL_JSON"
  }

  if ! count=$(python3 - "$SHELL_JSON" "$tmp" <<'PY'
import json
import sys

OLD_ID = "uth.power"
NEW_ID = "io.github.bjornramberg.omarchy-power-flow"

source, destination = sys.argv[1], sys.argv[2]

with open(source, "r", encoding="utf-8") as handle:
    text = handle.read()

# Validate before rewriting so a malformed file is never modified.
json.loads(text)

out = []
count = 0
i = 0
while i < len(text):
    ch = text[i]
    if ch != '"':
        out.append(ch)
        i += 1
        continue

    j = i + 1
    while j < len(text):
        if text[j] == "\\":
            j += 2
            continue
        if text[j] == '"':
            break
        j += 1
    token = text[i : j + 1]
    try:
        value = json.loads(token)
    except json.JSONDecodeError as exc:
        raise SystemExit(f"invalid string token in {source}: {exc}")

    if value == OLD_ID:
        token = json.dumps(NEW_ID)
        count += 1
    out.append(token)
    i = j + 1

candidate = "".join(out)
json.loads(candidate)

with open(destination, "w", encoding="utf-8", newline="") as handle:
    handle.write(candidate)

print(count)
PY
  ); then
    rm -f -- "$tmp"
    fail "cannot migrate $SHELL_JSON; file left untouched"
  fi

  if [[ "$count" -gt 0 ]]; then
    backup=$(backup_path "$SHELL_JSON")
    cp -p -- "$SHELL_JSON" "$backup" ||
      { rm -f -- "$tmp"; fail "cannot back up $SHELL_JSON"; }
    mv -f -- "$tmp" "$SHELL_JSON"
    actions+=("updated $count '$OLD_ID' id reference(s) in $SHELL_JSON (backup: $backup)")
  else
    rm -f -- "$tmp"
    actions+=("$SHELL_JSON has no '$OLD_ID' ids; unchanged")
  fi
else
  actions+=("$SHELL_JSON not found; shell entry skipped")
fi

# ---------- 3. Menu extension ----------
if [[ -f "$MENU_EXT" ]]; then
  if grep -Eq '"'"$OLD_MENU_KEY"'"[[:space:]]*:' -- "$MENU_EXT"; then
    if bash "$SCRIPT_DIR/install-menu-entry.sh"; then
      actions+=("migrated the '$OLD_MENU_KEY' menu row to '$NEW_MENU_KEY' (extension backed up by the installer)")
    elif ! grep -Eq '"'"$OLD_MENU_KEY"'"[[:space:]]*:' -- "$MENU_EXT" &&
      grep -Eq '"'"$NEW_MENU_KEY"'"[[:space:]]*:' -- "$MENU_EXT"; then
      warn "menu row was migrated, but the menu installer reported a refresh problem; refresh the menu manually"
      actions+=("migrated the '$OLD_MENU_KEY' menu row to '$NEW_MENU_KEY' (extension backed up by the installer)")
    else
      warn "could not migrate the '$OLD_MENU_KEY' menu row; check the extension"
      failures=$((failures + 1))
    fi
  else
    actions+=("$MENU_EXT has no '$OLD_MENU_KEY' row; unchanged")
  fi
else
  actions+=("$MENU_EXT not found; menu row skipped")
fi

# ---------- 4. Refresh / rescan ----------
if command -v omarchy-shell >/dev/null 2>&1; then
  if omarchy-shell shell rescanPlugins; then
    actions+=("requested a shell plugin rescan")
  else
    warn "omarchy-shell shell rescanPlugins failed; rescan manually"
  fi
else
  warn "omarchy-shell not found; rescan the shell manually"
fi

if command -v omarchy >/dev/null 2>&1; then
  if omarchy menu refresh; then
    actions+=("requested a menu refresh")
  else
    warn "omarchy menu refresh failed; refresh the menu manually"
  fi
else
  warn "omarchy not found; refresh the menu manually"
fi

# ---------- 5. Report ----------
say "summary:"
for action in "${actions[@]}"; do
  say "  - $action"
done

if [[ ${#warnings[@]} -gt 0 ]]; then
  say "${#warnings[@]} warning(s) above."
fi

if [[ "$failures" -gt 0 ]]; then
  exit 1
fi

say "migration complete."
