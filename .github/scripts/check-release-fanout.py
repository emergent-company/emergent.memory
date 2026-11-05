#!/usr/bin/env python3
"""Fail the release when auto-tag's dispatch manifest has drifted.

``auto-tag.yml`` creates the release tag and, when ``RELEASE_TOKEN`` is absent,
starts the release fan-out itself with ``gh workflow run``: a ``GITHUB_TOKEN``
tag push starts no workflow runs (#116). That fallback is only complete if its
workflow list equals the set of workflows declaring a ``push`` trigger on the
tag being released. A renamed or newly added workflow would otherwise be
skipped silently -- the tag would exist and one release workflow would never
run.

This check runs *before* the tag is created and compares:

  * every workflow whose ``on.push.tags`` matches the new tag, with
  * every entry in ``.github/release-workflows.txt``,

and additionally requires every one of those workflows to declare
``workflow_dispatch`` (without it the fallback cannot start it at all).

Stdlib only, like ``check-injection.py``: no PyYAML is assumed on the runner.
The parser understands the block and flow spellings of ``on.push.tags`` used
in this repository. It fails closed when it cannot find an ``on:`` block
rather than guessing.

Usage:
  check-release-fanout.py <workflow-dir> <manifest> <tag>
"""

import fnmatch
import os
import re
import sys

KEY_RE = re.compile(r"^( *)([A-Za-z_][A-Za-z0-9_-]*)\s*:(.*)$")


def indent_of(line):
    return len(line) - len(line.lstrip(" "))


def strip_inline_comment(value):
    quote = None
    for i, ch in enumerate(value):
        if quote:
            if ch == quote:
                quote = None
        elif ch in ("'", '"'):
            quote = ch
        elif ch == "#" and (i == 0 or value[i - 1] in " \t"):
            return value[:i].rstrip()
    return value.rstrip()


def unquote(value):
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
        return value[1:-1]
    return value


def key_of(line):
    """Return (indent, key, inline-value) or None for a non-key line."""
    match = KEY_RE.match(line)
    if not match:
        return None
    return match.group(1), match.group(2), strip_inline_comment(match.group(3)).strip()


def block(lines, start, parent_indent):
    """Return the child range of the key on line ``start``."""
    end = len(lines)
    for i in range(start + 1, len(lines)):
        if indent_of(lines[i]) <= parent_indent:
            end = i
            break
    return start + 1, end


def parse_on(path):
    """Return (event names, tag patterns) from a workflow's top-level ``on:``.

    Returns None when no top-level ``on:`` block can be found, so the caller
    can fail closed instead of treating the workflow as untriggered.
    """
    with open(path, encoding="utf-8") as handle:
        lines = [
            line.rstrip()
            for line in handle.read().splitlines()
            if line.strip() and not line.lstrip().startswith("#")
        ]

    on_index = None
    for i, line in enumerate(lines):
        key = key_of(line)
        if key and indent_of(line) == 0 and key[1] == "on":
            on_index = i
            break
    if on_index is None:
        return None

    rest = key_of(lines[on_index])[2]
    if rest:
        # Flow form such as `on: [push, pull_request]`: event names only, no
        # per-event tag block, so nothing can match a tag here.
        events = {unquote(item) for item in rest.strip("[]").split(",") if item.strip()}
        return events, []

    start, end = block(lines, on_index, 0)
    if start == end:
        return set(), []

    event_indent = min(indent_of(lines[i]) for i in range(start, end))
    events = set()
    tags = []

    for i in range(start, end):
        if indent_of(lines[i]) != event_indent:
            continue
        key = key_of(lines[i])
        if not key:
            continue
        events.add(key[1])
        if key[1] != "push":
            continue

        push_rest = key[2]
        if push_rest:
            # `push: {tags: ['v*'], ...}`
            match = re.search(r"\btags\s*:\s*\[([^\]]*)\]", push_rest)
            if match:
                tags.extend(
                    unquote(item.strip())
                    for item in match.group(1).split(",")
                    if item.strip()
                )
            continue

        push_start, push_end = block(lines, i, event_indent)
        for j in range(push_start, push_end):
            tags_key = key_of(lines[j])
            if not tags_key or tags_key[1] != "tags":
                continue
            tags_rest = tags_key[2]
            if tags_rest:
                if tags_rest.startswith("["):
                    inner = tags_rest[1:tags_rest.rfind("]")]
                    tags.extend(
                        unquote(item.strip())
                        for item in inner.split(",")
                        if item.strip()
                    )
                else:
                    tags.append(unquote(tags_rest))
            else:
                tags_indent = indent_of(lines[j])
                for k in range(j + 1, push_end):
                    if indent_of(lines[k]) <= tags_indent:
                        break
                    item = lines[k].lstrip()
                    if item.startswith("-"):
                        tags.append(unquote(strip_inline_comment(item[1:]).strip()))
            break

    return events, tags


def tag_matches(pattern, tag):
    pattern = unquote(pattern)
    if pattern.startswith("!"):
        # Negated patterns are rare in this repo; ignore them when deciding
        # membership. A workflow that is excluded by a negation would show up
        # as a false "missing manifest entry", which fails safe (no tag).
        return False
    return fnmatch.fnmatchcase(tag, pattern)


def discover(workflow_dir, tag):
    found = {}
    for name in sorted(os.listdir(workflow_dir)):
        if not name.endswith((".yml", ".yaml")):
            continue
        path = os.path.join(workflow_dir, name)
        parsed = parse_on(path)
        if parsed is None:
            raise SystemExit(
                f"ERROR: {path}: no top-level `on:` block; refusing to guess "
                "whether it is tag-triggered"
            )
        events, tags = parsed
        if any(tag_matches(pattern, tag) for pattern in tags):
            found[name] = events
    return found


def read_manifest(path):
    entries = []
    with open(path, encoding="utf-8") as handle:
        for lineno, raw in enumerate(handle, 1):
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            name = line.split("|", 1)[0].strip()
            if not name.endswith((".yml", ".yaml")):
                raise SystemExit(
                    f"ERROR: {path}:{lineno}: expected a workflow file, got {line!r}"
                )
            entries.append(name)
    return entries


def main(argv):
    if len(argv) != 4:
        sys.stderr.write(__doc__)
        return 2

    workflow_dir, manifest, tag = argv[1:4]
    found = discover(workflow_dir, tag)
    entries = read_manifest(manifest)
    errors = []

    duplicates = sorted({name for name in entries if entries.count(name) > 1})
    for name in duplicates:
        errors.append(f"{manifest} lists {name} more than once")

    for name in sorted(found):
        if name not in entries:
            errors.append(
                f"{name} triggers on tag {tag} but is missing from {manifest}; "
                "the dispatch fallback would never start it"
            )

    for name in entries:
        if name not in found:
            errors.append(
                f"{manifest} lists {name}, but it does not trigger on tag {tag}"
            )
        elif "workflow_dispatch" not in found[name]:
            errors.append(
                f"{name} triggers on tag {tag} but has no workflow_dispatch:, so "
                "the dispatch fallback cannot start it"
            )

    print(f"release tag: {tag}")
    print(f"tag-triggered workflows ({len(found)}): {', '.join(sorted(found)) or '(none)'}")
    print(f"dispatch manifest ({len(entries)}): {', '.join(entries)}")

    if errors:
        print(f"ERROR: release fan-out drift for {tag}:", file=sys.stderr)
        for error in errors:
            print(f"  - {error}", file=sys.stderr)
        print(
            "Update .github/release-workflows.txt (and declare workflow_dispatch: "
            "on any new workflow) before creating the tag.",
            file=sys.stderr,
        )
        return 1

    print("MATCH: every tag-triggered workflow is dispatchable and in the manifest.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
