#!/usr/bin/env bash
# Install the UTH Power row into the user's Omarchy menu extension.
#
# This helper is intentionally conservative:
#   * it only writes ~/.config/omarchy/extensions/omarchy-menu.jsonc;
#   * it never writes below /usr/share/omarchy;
#   * it backs up the user's file before changing it;
#   * it validates the resulting JSONC before replacing the original;
#   * it never rewrites the file as plain JSON, so comments and formatting
#     survive.
#
# The embedded Python helper implements a minimal JSONC parser by stripping
# comments and trailing commas (string-aware), then uses the standard-library
# json module for semantic validation.  The actual insertion is brace-aware and
# text-preserving.
#
# OMARCHY_MENU_TEMPLATE may point at an alternate template for tests; normal
# installs use the system path above and never write below /usr/share.
set -euo pipefail

readonly TEMPLATE_DEFAULT="/usr/share/omarchy/config/omarchy/extensions/omarchy-menu.jsonc"

if [[ -z "${HOME:-}" ]]; then
  printf 'install-menu-entry: HOME is not set.\n' >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  printf 'install-menu-entry: python3 is required but was not found.\n' >&2
  exit 1
fi

target="${HOME}/.config/omarchy/extensions/omarchy-menu.jsonc"
template="${OMARCHY_MENU_TEMPLATE:-$TEMPLATE_DEFAULT}"

if [[ ! -e "$target" ]]; then
  if [[ ! -f "$template" || ! -r "$template" ]]; then
    printf 'install-menu-entry: target does not exist and template is unavailable: %s\n' \
      "$template" >&2
    exit 1
  fi
  mkdir -p -- "$(dirname -- "$target")"
  cp -- "$template" "$target"
fi

set +e
python3 - "$target" <<'PY'
import json
import os
import re
import shutil
import sys
import tempfile
import time

TARGET = sys.argv[1]
ICON = "\uf0e7"
LABEL = "UTH Power"
DESCRIPTION = "Live component power-flow panel"
ACTION = "omarchy-shell shell toggle uth.power"
WHEN = 'test -f "$HOME/.config/omarchy/plugins/uth.power/manifest.json"'
ENTRY = {
    "icon": ICON,
    "label": LABEL,
    "description": DESCRIPTION,
    "action": ACTION,
    "when": WHEN,
}


def fail(message):
    print(f"install-menu-entry: {message}", file=sys.stderr)
    raise SystemExit(1)


def read_string(text, start):
    """Return the complete string token starting at start and the next index."""
    out = [text[start]]
    i = start + 1
    while i < len(text):
        ch = text[i]
        out.append(ch)
        if ch == "\\":
            if i + 1 >= len(text):
                raise ValueError("unterminated string escape")
            out.append(text[i + 1])
            i += 2
            continue
        if ch == '"':
            return "".join(out), i + 1
        i += 1
    raise ValueError("unterminated string")


def strip_comments(text):
    """Remove // and /* */ comments while preserving strings and line count."""
    out = []
    i = 0
    while i < len(text):
        ch = text[i]
        if ch == '"':
            token, i = read_string(text, i)
            out.append(token)
            continue
        if ch == "/" and i + 1 < len(text):
            nxt = text[i + 1]
            if nxt == "/":
                newline = text.find("\n", i)
                if newline < 0:
                    break
                out.append("\n")
                i = newline + 1
                continue
            if nxt == "*":
                end = text.find("*/", i + 2)
                if end < 0:
                    raise ValueError("unterminated block comment")
                out.append(" " + "\n" * text.count("\n", i, end + 2))
                i = end + 2
                continue
        out.append(ch)
        i += 1
    return "".join(out)


def strip_trailing_commas(text):
    """Remove commas directly before } or ] outside strings."""
    out = []
    i = 0
    while i < len(text):
        ch = text[i]
        if ch == '"':
            token, i = read_string(text, i)
            out.append(token)
            continue
        if ch == ",":
            j = i + 1
            while j < len(text) and text[j] in " \t\r\n":
                j += 1
            if j < len(text) and text[j] in "}]":
                i += 1
                continue
        out.append(ch)
        i += 1
    return "".join(out)


def parse_jsonc(text):
    try:
        return json.loads(strip_trailing_commas(strip_comments(text)))
    except (ValueError, json.JSONDecodeError) as exc:
        raise ValueError(str(exc)) from exc


def find_object_span(text):
    """Find the root object's closing brace and its last significant content."""
    i = 0
    depth = 0
    started = False
    close = None
    last_sig = None
    while i < len(text):
        ch = text[i]
        if ch == '"':
            _, j = read_string(text, i)
            last_sig = j - 1
            i = j
            continue
        if ch == "/" and i + 1 < len(text):
            nxt = text[i + 1]
            if nxt == "/":
                newline = text.find("\n", i)
                if newline < 0:
                    break
                i = newline + 1
                continue
            if nxt == "*":
                end = text.find("*/", i + 2)
                if end < 0:
                    raise ValueError("unterminated block comment")
                i = end + 2
                continue
        if ch in " \t\r\n":
            i += 1
            continue
        if not started:
            if ch != "{":
                raise ValueError("root is not a JSON object")
            started = True
            depth = 1
            last_sig = i
            i += 1
            continue
        if ch in "{[":
            depth += 1
            last_sig = i
            i += 1
            continue
        if ch in "}]":
            depth -= 1
            if depth < 0:
                raise ValueError("unexpected closing bracket")
            if depth == 0:
                if ch != "}":
                    raise ValueError("root object closed with ]")
                close = i
                break
            last_sig = i
            i += 1
            continue
        last_sig = i
        i += 1
    if close is None:
        raise ValueError("missing closing brace")
    return close, last_sig


def detect_indent(text, close):
    match = re.search(r'(?m)^([ \t]+)"', text[:close])
    return match.group(1) if match else "  "


def closing_indent(text, close):
    line_start = text.rfind("\n", 0, close) + 1
    candidate = text[line_start:close]
    return candidate if candidate.strip() == "" else ""


def render_entry(indent):
    quote = lambda value: json.dumps(value, ensure_ascii=False)
    lines = [
        f'{indent}"uth-power": {{',
        f'{indent}  "icon": {quote(ICON)},',
        f'{indent}  "label": {quote(LABEL)},',
        f'{indent}  "description": {quote(DESCRIPTION)},',
        f'{indent}  "action": {quote(ACTION)},',
        f'{indent}  "when": {quote(WHEN)}',
        f'{indent}}}',
    ]
    return "\n".join(lines)


def insert_entry(text, close, last_sig):
    prefix = text[:close]
    if last_sig is not None and text[last_sig] not in "{,":
        # Add the missing separator immediately after the last value, before
        # any trailing comment.  This also handles a normal final property.
        prefix = prefix[: last_sig + 1] + "," + prefix[last_sig + 1 :]

    indent = detect_indent(text, close)
    close_indent = closing_indent(text, close)
    new_prefix = (
        prefix.rstrip(" \t\r\n")
        + "\n"
        + render_entry(indent)
        + "\n"
        + close_indent
    )
    return new_prefix + text[close:]


def unique_backup(path):
    stamp = time.strftime("%Y%m%d-%H%M%S")
    candidate = f"{path}.bak.{stamp}"
    suffix = 1
    while os.path.exists(candidate):
        candidate = f"{path}.bak.{stamp}.{suffix}"
        suffix += 1
    return candidate


def write_atomic(path, content):
    directory = os.path.dirname(path) or "."
    fd, temp_path = tempfile.mkstemp(prefix=".omarchy-menu.", suffix=".tmp", dir=directory)
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        shutil.copymode(path, temp_path)
        os.replace(temp_path, path)
    except Exception:
        try:
            os.unlink(temp_path)
        except FileNotFoundError:
            pass
        raise


def main():
    try:
        with open(TARGET, "r", encoding="utf-8") as handle:
            original = handle.read()
    except OSError as exc:
        fail(f"cannot read {TARGET}: {exc}")

    try:
        document = parse_jsonc(original)
    except ValueError as exc:
        fail(f"malformed JSONC in {TARGET}: {exc}; target left untouched")

    if not isinstance(document, dict):
        fail(f"malformed target {TARGET}: root is not a JSON object; target left untouched")
    if "uth-power" in document:
        return 10

    try:
        close, last_sig = find_object_span(original)
        candidate = insert_entry(original, close, last_sig)
        candidate_document = parse_jsonc(candidate)
    except Exception as exc:
        fail(f"cannot update {TARGET}: {exc}; target left untouched")

    expected = dict(document)
    expected["uth-power"] = ENTRY
    if candidate_document != expected:
        fail(f"internal validation failed for {TARGET}; target left untouched")
    if candidate_document.get("uth-power") != ENTRY:
        fail(f"internal entry validation failed for {TARGET}; target left untouched")

    backup = unique_backup(TARGET)
    try:
        shutil.copy2(TARGET, backup)
    except Exception as exc:
        fail(f"cannot back up {TARGET}: {exc}; target left untouched")

    try:
        write_atomic(TARGET, candidate)
    except Exception as exc:
        fail(f"cannot replace {TARGET}: {exc}; backup is {backup}")

    print(f"install-menu-entry: backup: {backup}", file=sys.stderr)
    return 0


raise SystemExit(main())
PY
status=$?
set -e

case "$status" in
  0)
    if ! command -v omarchy >/dev/null 2>&1; then
      printf 'install-menu-entry: row installed, but omarchy was not found; run "omarchy menu refresh" manually.\n' >&2
      exit 1
    fi
    if ! omarchy menu refresh; then
      printf 'install-menu-entry: row installed, but "omarchy menu refresh" failed.\n' >&2
      exit 1
    fi
    printf 'install-menu-entry: added "uth-power" to %s\n' "$target"
    ;;
  10)
    printf 'install-menu-entry: "uth-power" already present; no changes.\n'
    ;;
  *)
    printf 'install-menu-entry: failed; target left unchanged.\n' >&2
    exit 1
    ;;
esac
