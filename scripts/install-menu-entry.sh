#!/usr/bin/env bash
# Install or remove the Omarchy Power Flow row in the user's Omarchy menu
# extension file.
#
# This helper is intentionally conservative:
#   * it only writes ~/.config/omarchy/extensions/omarchy-menu.jsonc;
#   * it never writes below /usr/share/omarchy;
#   * it backs up the user's file before changing it;
#   * it validates the resulting JSONC before replacing the original;
#   * it never rewrites the file as plain JSON, so comments and formatting
#     survive.
#
# The default mode installs the row.  "--remove" deletes only the plugin's own
# root rows ("omarchy-power-flow" and the legacy "uth-power") and leaves every
# unrelated row and comment untouched.  Both modes are idempotent: install
# no-ops when the row is already present, and --remove no-ops cleanly when it
# is already absent.
#
# The embedded Python helper implements a minimal JSONC parser by stripping
# comments and trailing commas (string-aware), then uses the standard-library
# json module for semantic validation.  The actual insertion/replacement/
# removal is brace-aware and text-preserving.
#
# OMARCHY_MENU_TEMPLATE may point at an alternate template for tests; normal
# installs use the system path above and never write below /usr/share.
#
# Usage: install-menu-entry.sh [--remove]
set -euo pipefail

readonly TEMPLATE_DEFAULT="/usr/share/omarchy/config/omarchy/extensions/omarchy-menu.jsonc"

mode=install
for arg in "$@"; do
  case "$arg" in
    --remove)
      mode=remove
      ;;
    -h|--help)
      printf 'Usage: %s [--remove]\n' "${0##*/}"
      printf '  (no flag)  install the Omarchy Power Flow menu row\n'
      printf '  --remove   remove the plugin menu row from the user extension\n'
      exit 0
      ;;
    *)
      printf '%s: unknown argument: %s\n' "${0##*/}" "$arg" >&2
      printf 'Usage: %s [--remove]\n' "${0##*/}" >&2
      exit 2
      ;;
  esac
done

if [[ "$mode" == remove ]]; then
  prog=remove-menu-entry
else
  prog=install-menu-entry
fi

if [[ -z "${HOME:-}" ]]; then
  printf '%s: HOME is not set.\n' "$prog" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  printf '%s: python3 is required but was not found.\n' "$prog" >&2
  exit 1
fi

target="${HOME}/.config/omarchy/extensions/omarchy-menu.jsonc"
template="${OMARCHY_MENU_TEMPLATE:-$TEMPLATE_DEFAULT}"

if [[ "$mode" == remove && ! -e "$target" ]]; then
  printf '%s: no user menu extension at %s; nothing to remove.\n' "$prog" "$target"
  exit 0
fi

if [[ "$mode" == install && ! -e "$target" ]]; then
  if [[ ! -f "$template" || ! -r "$template" ]]; then
    printf '%s: target does not exist and template is unavailable: %s\n' \
      "$prog" "$template" >&2
    exit 1
  fi
  mkdir -p -- "$(dirname -- "$target")"
  cp -- "$template" "$target"
fi

set +e
python3 - "$target" "$mode" "$prog" <<'PY'
import json
import os
import re
import shutil
import sys
import tempfile
import time

TARGET = sys.argv[1]
MODE = sys.argv[2]
PROG = sys.argv[3]
NEW_KEY = "omarchy-power-flow"
OLD_KEY = "uth-power"
ICON = "\uf0e7"
LABEL = "Omarchy Power Flow"
DESCRIPTION = "Live component power-flow panel"
ACTION = "omarchy-shell shell toggle io.github.bjornramberg.omarchy-power-flow"
WHEN = (
    'test -f "$HOME/.config/omarchy/plugins/'
    'io.github.bjornramberg.omarchy-power-flow/manifest.json"'
)
ENTRY = {
    "icon": ICON,
    "label": LABEL,
    "description": DESCRIPTION,
    "action": ACTION,
    "when": WHEN,
}


def fail(message):
    print(f"{PROG}: {message}", file=sys.stderr)
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


def skip_ws_comments(text, i):
    """Advance past whitespace and comments, returning the next index."""
    while i < len(text):
        ch = text[i]
        if ch in " \t\r\n":
            i += 1
            continue
        if ch == "/" and i + 1 < len(text):
            nxt = text[i + 1]
            if nxt == "/":
                newline = text.find("\n", i)
                if newline < 0:
                    return len(text)
                i = newline + 1
                continue
            if nxt == "*":
                end = text.find("*/", i + 2)
                if end < 0:
                    raise ValueError("unterminated block comment")
                i = end + 2
                continue
        break
    return i


def scan_value_end(text, i):
    """Return the exclusive end index of the JSON value starting at i."""
    if i >= len(text):
        raise ValueError("missing value")
    ch = text[i]
    if ch == '"':
        _, end = read_string(text, i)
        return end
    if ch in "{[":
        depth = 0
        while i < len(text):
            ch = text[i]
            if ch == '"':
                _, i = read_string(text, i)
                continue
            if ch == "/" and i + 1 < len(text) and text[i + 1] == "/":
                newline = text.find("\n", i)
                i = len(text) if newline < 0 else newline + 1
                continue
            if ch == "/" and i + 1 < len(text) and text[i + 1] == "*":
                end = text.find("*/", i + 2)
                if end < 0:
                    raise ValueError("unterminated block comment")
                i = end + 2
                continue
            if ch in "{[":
                depth += 1
            elif ch in "}]":
                depth -= 1
                if depth == 0:
                    return i + 1
            i += 1
        raise ValueError("unterminated value")
    while i < len(text) and text[i] not in ",}] \t\r\n":
        i += 1
    return i


def root_members(text):
    """Return top-level object member spans as dictionaries."""
    i = skip_ws_comments(text, 0)
    if i >= len(text) or text[i] != "{":
        raise ValueError("root is not a JSON object")
    i += 1
    members = []
    while True:
        i = skip_ws_comments(text, i)
        if i >= len(text):
            raise ValueError("missing closing brace")
        if text[i] == "}":
            break
        if text[i] != '"':
            raise ValueError("expected a top-level key")
        key_start = i
        token, i = read_string(text, i)
        key = json.loads(token)
        i = skip_ws_comments(text, i)
        if i >= len(text) or text[i] != ":":
            raise ValueError(f"expected ':' after {key}")
        i = skip_ws_comments(text, i + 1)
        value_start = i
        value_end = scan_value_end(text, i)
        line_start = text.rfind("\n", 0, key_start) + 1
        prefix = text[line_start:key_start]
        indent = prefix if prefix.strip() == "" else detect_indent(text, key_start)
        members.append({
            "key": key,
            "key_start": key_start,
            "value_start": value_start,
            "value_end": value_end,
            "indent": indent,
        })
        i = skip_ws_comments(text, value_end)
        if i < len(text) and text[i] == ",":
            i += 1
            continue
        if i < len(text) and text[i] == "}":
            continue
        raise ValueError(f"expected ',' or '}}' after {key}")
    return members


def detect_indent(text, pos=None):
    limit = len(text) if pos is None else pos
    match = re.search(r'(?m)^([ \t]+)"', text[:limit])
    return match.group(1) if match else "  "


def closing_indent(text, close):
    line_start = text.rfind("\n", 0, close) + 1
    candidate = text[line_start:close]
    return candidate if candidate.strip() == "" else ""


def render_entry(indent, key=NEW_KEY):
    quote = lambda value: json.dumps(value, ensure_ascii=False)
    lines = [
        f'{indent}"{key}": {{',
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


def replace_member(text, member):
    """Replace one top-level member, keeping its position and surrounding text."""
    start = member["key_start"]
    line_start = text.rfind("\n", 0, start) + 1
    if text[line_start:start].strip() == "":
        start = line_start
    return text[:start] + render_entry(member["indent"]) + text[member["value_end"] :]


def remove_member(text, member, members):
    """Remove one top-level member and its separating comma."""
    start = member["key_start"]
    end = member["value_end"]

    after = skip_ws_comments(text, end)
    if after < len(text) and text[after] == ",":
        end = after + 1
        # Consume the removed row's line break so no blank line is left behind.
        if text.startswith("\r\n", end):
            end += 2
        elif end < len(text) and text[end] == "\n":
            end += 1
    else:
        before = start - 1
        while before >= 0 and text[before] in " \t\r\n":
            before -= 1
        if before >= 0 and text[before] == ",":
            start = before
        else:
            # This was the only member: consume its line break so the closing
            # brace does not gain a blank line.
            if text.startswith("\r\n", end):
                end += 2
            elif end < len(text) and text[end] == "\n":
                end += 1

    # Trim the now-empty line left behind by a removed trailing member.
    line_start = text.rfind("\n", 0, start) + 1
    if text[line_start:start].strip() == "" and start > line_start:
        start = line_start
    return text[:start] + text[end:]


def remove_plugin_rows(text, document):
    """Remove the plugin's own top-level rows, preserving everything else."""
    removed = []
    for key in (NEW_KEY, OLD_KEY):
        if key not in document:
            continue
        members = root_members(text)
        member = next((m for m in members if m["key"] == key), None)
        if member is None:
            raise ValueError(f"cannot locate top-level {key} row")
        text = remove_member(text, member, members)
        removed.append(key)
    return text, removed


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

    has_new = NEW_KEY in document
    has_old = OLD_KEY in document

    if MODE == "remove":
        if not has_new and not has_old:
            return 12
        try:
            candidate, removed = remove_plugin_rows(original, document)
            candidate_document = parse_jsonc(candidate)
        except Exception as exc:
            fail(f"cannot update {TARGET}: {exc}; target left untouched")

        expected = dict(document)
        for key in removed:
            expected.pop(key, None)
        if candidate_document != expected:
            fail(f"internal validation failed for {TARGET}; target left untouched")
        if any(key in candidate_document for key in (NEW_KEY, OLD_KEY)):
            fail(f"internal removal validation failed for {TARGET}; target left untouched")

        backup = unique_backup(TARGET)
        try:
            shutil.copy2(TARGET, backup)
        except Exception as exc:
            fail(f"cannot back up {TARGET}: {exc}; target left untouched")

        try:
            write_atomic(TARGET, candidate)
        except Exception as exc:
            fail(f"cannot replace {TARGET}: {exc}; backup is {backup}")

        print(f"{PROG}: removed {', '.join(removed)}", file=sys.stderr)
        print(f"{PROG}: backup: {backup}", file=sys.stderr)
        return 0

    try:
        members = root_members(original)
        new_member = next((m for m in members if m["key"] == NEW_KEY), None)
        old_member = next((m for m in members if m["key"] == OLD_KEY), None)
    except Exception as exc:
        fail(f"cannot inspect {TARGET}: {exc}; target left untouched")

    if has_new and not has_old:
        return 10

    try:
        if has_old and not has_new:
            candidate = replace_member(original, old_member)
            action = "migrated"
        elif has_new and has_old:
            candidate = remove_member(original, old_member, members)
            action = "removed legacy row from"
        else:
            close, last_sig = find_object_span(original)
            candidate = insert_entry(original, close, last_sig)
            action = "added"
        candidate_document = parse_jsonc(candidate)
    except Exception as exc:
        fail(f"cannot update {TARGET}: {exc}; target left untouched")

    expected = dict(document)
    expected.pop(OLD_KEY, None)
    if not has_new:
        expected[NEW_KEY] = ENTRY
    if candidate_document != expected:
        fail(f"internal validation failed for {TARGET}; target left untouched")
    if not has_new and candidate_document.get(NEW_KEY) != ENTRY:
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

    print(f"{PROG}: {action} \"{NEW_KEY}\"", file=sys.stderr)
    print(f"{PROG}: backup: {backup}", file=sys.stderr)
    return 0


raise SystemExit(main())
PY
status=$?
set -e

if [[ "$mode" == remove ]]; then
  changed_word="removed"
else
  changed_word="installed"
fi

case "$status" in
  0)
    if ! command -v omarchy >/dev/null 2>&1; then
      printf '%s: row %s, but omarchy was not found; run "omarchy menu refresh" manually.\n' \
        "$prog" "$changed_word" >&2
      exit 1
    fi
    if ! omarchy menu refresh; then
      printf '%s: row %s, but "omarchy menu refresh" failed.\n' "$prog" "$changed_word" >&2
      exit 1
    fi
    printf '%s: %s "omarchy-power-flow" in %s\n' "$prog" "$changed_word" "$target"
    ;;
  10)
    printf '%s: "omarchy-power-flow" already present; no changes.\n' "$prog"
    ;;
  12)
    printf '%s: "omarchy-power-flow" already absent; no changes.\n' "$prog"
    ;;
  *)
    printf '%s: failed; target left unchanged.\n' "$prog" >&2
    exit 1
    ;;
esac
