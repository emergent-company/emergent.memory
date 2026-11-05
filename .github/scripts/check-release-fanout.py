#!/usr/bin/env python3
"""Fail the release when auto-tag's dispatch manifest has drifted.

``auto-tag.yml`` creates the release tag and, when ``RELEASE_TOKEN`` is absent,
starts the release fan-out itself with ``gh workflow run``: a ``GITHUB_TOKEN``
tag push starts no workflow runs (#116). That fallback is only complete if its
workflow list equals the set of workflows declaring a ``push`` trigger that
fires for the tag being released. A renamed or newly added workflow would
otherwise be skipped silently -- the tag would exist and one release workflow
would never run.

This check runs *before* the tag is created and compares:

  * every workflow whose ``on.push`` fires for the new tag, with
  * every entry in ``.github/release-workflows.txt``,

and additionally requires every one of those workflows to declare
``workflow_dispatch`` (without it the fallback cannot start it at all).

GitHub's push-filter semantics matter here, so the parser implements them
rather than looking for a literal ``tags:`` key:

  * ``tags`` present  -> only matching tags (negations supported);
  * ``tags-ignore``   -> all tags except the ignored patterns;
  * neither tag key, but ``branches``/``branches-ignore`` present -> branch
    pushes only, no tag runs;
  * neither tag nor branch key (bare ``push:``, or ``paths`` only) -> every
    tag, because ``paths`` is not evaluated for tag pushes.

Stdlib only, like ``check-injection.py``: no PyYAML is assumed on the runner.
Anything the parser cannot classify fails closed instead of guessing.

Usage:
  check-release-fanout.py <workflow-dir> <manifest> <tag>
  check-release-fanout.py --self-test
"""

import contextlib
import fnmatch
import io
import os
import re
import sys
import tempfile

KEY_RE = re.compile(r"^( *)['\"]?([A-Za-z_][A-Za-z0-9_-]*)['\"]?\s*:(.*)$")

PUSH_FILTERS = (
    "tags",
    "tags-ignore",
    "branches",
    "branches-ignore",
    "paths",
    "paths-ignore",
)


class FanoutError(Exception):
    """The workflow tree cannot be classified confidently."""


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


def split_top_level(value, separator):
    """Split on `separator`, ignoring separators inside quotes/brackets."""
    parts = []
    current = []
    depth = 0
    quote = None
    for ch in value:
        if quote:
            current.append(ch)
            if ch == quote:
                quote = None
            continue
        if ch in ("'", '"'):
            quote = ch
            current.append(ch)
        elif ch in "[{(":
            depth += 1
            current.append(ch)
        elif ch in "]})":
            depth -= 1
            current.append(ch)
        elif ch == separator and depth == 0:
            parts.append("".join(current))
            current = []
        else:
            current.append(ch)
    parts.append("".join(current))
    return parts


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


def flow_balance(text):
    depth = 0
    quote = None
    for ch in text:
        if quote:
            if ch == quote:
                quote = None
        elif ch in ("'", '"'):
            quote = ch
        elif ch in "[{":
            depth += 1
        elif ch in "]}":
            depth -= 1
    return depth


def join_flow_value(text, lines, index, end):
    """Join a flow value that continues onto deeper lines until it balances."""
    if not text or text[0] not in "[{":
        return text
    depth = flow_balance(text)
    j = index + 1
    while depth > 0 and j < end:
        text = text + " " + lines[j].strip()
        depth = flow_balance(text)
        j += 1
    if depth != 0:
        raise FanoutError("unbalanced flow list in a push filter")
    return text


def parse_list_value(text):
    """Parse a scalar or flow list, or return None when a block list follows."""
    text = strip_inline_comment(text).strip()
    if not text:
        return None
    if text.startswith("["):
        inner = text[1 : text.rfind("]")] if "]" in text else text[1:]
        return [unquote(item.strip()) for item in split_top_level(inner, ",") if item.strip()]
    return [unquote(text)]


def block_list(lines, key_index, end, key_indent):
    items = []
    for i in range(key_index + 1, end):
        if indent_of(lines[i]) <= key_indent:
            break
        item = lines[i].lstrip()
        if item.startswith("-"):
            items.append(unquote(strip_inline_comment(item[1:]).strip()))
        elif item:
            raise FanoutError(f"cannot parse list item {lines[i]!r}")
    return items


def parse_block_push(lines, start, end):
    """Classify a block-style ``push:`` body into filter -> pattern list."""
    child_indent = None
    for i in range(start, end):
        indent = indent_of(lines[i])
        if indent > 0 and (child_indent is None or indent < child_indent):
            child_indent = indent
    if child_indent is None:
        return {}

    spec = {}
    for i in range(start, end):
        if indent_of(lines[i]) != child_indent:
            continue
        key = key_of(lines[i])
        if not key:
            raise FanoutError(f"cannot parse push filter {lines[i]!r}")
        name = key[1]
        if name not in PUSH_FILTERS:
            continue
        raw = join_flow_value(key[2], lines, i, end)
        values = parse_list_value(raw)
        if values is None:
            values = block_list(lines, i, end, child_indent)
        if name in ("tags", "tags-ignore", "branches", "branches-ignore"):
            spec[name] = values
    return spec


def parse_flow_push(text, lines, index, end):
    """Classify a flow-style ``push: { ... }`` body, including multi-line."""
    parts = [text]
    depth = text.count("{") - text.count("}")
    while depth > 0 and index + 1 < end:
        index += 1
        parts.append(lines[index])
        depth += lines[index].count("{") - lines[index].count("}")
    flow = " ".join(parts)
    if "{" not in flow or "}" not in flow:
        raise FanoutError("unbalanced flow-style push map")
    body = flow[flow.index("{") + 1 : flow.rindex("}")]
    spec = {}
    for entry in split_top_level(body, ","):
        if not entry.strip():
            continue
        if ":" not in entry:
            raise FanoutError(f"cannot parse flow-style push filter {entry!r}")
        name, value = entry.split(":", 1)
        name = unquote(name.strip())
        if name in ("tags", "tags-ignore", "branches", "branches-ignore"):
            spec[name] = parse_list_value(value) or []
    return spec


def parse_on(path):
    """Return (event names, push spec or None) from a workflow's ``on:`` block."""
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
        if rest.startswith("{"):
            raise FanoutError(
                f"{path}: flow-style `on:` map is not supported; use the block form"
            )
        events = {unquote(item) for item in rest.strip("[]").split(",") if item.strip()}
        return events, ({} if "push" in events else None)

    start, end = block(lines, on_index, 0)
    if start == end:
        return set(), None

    event_indent = min(indent_of(lines[i]) for i in range(start, end))
    events = set()
    push_spec = None

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
        if not push_rest:
            push_start, push_end = block(lines, i, event_indent)
            push_spec = parse_block_push(lines, push_start, push_end)
        elif push_rest.startswith("{"):
            push_spec = parse_flow_push(push_rest, lines, i, end)
        else:
            raise FanoutError(f"{path}: cannot classify `push: {push_rest}`")

    return events, push_spec


def tag_triggers(tag, spec):
    """Apply GitHub's push-filter rules for a tag ref."""
    if "tags" in spec:
        # GitHub evaluates tag patterns in order: a matching `!` pattern
        # excludes, and a later positive match includes again.
        matched = False
        for pattern in spec["tags"]:
            if pattern.startswith("!"):
                if fnmatch.fnmatchcase(tag, pattern[1:]):
                    matched = False
            elif fnmatch.fnmatchcase(tag, pattern):
                matched = True
        return matched

    if "tags-ignore" in spec:
        return not any(fnmatch.fnmatchcase(tag, p) for p in spec["tags-ignore"])

    # No tag filter at all: a push restricted to branches does not run on tags,
    # while a bare push (or one filtered only by paths) does, because `paths`
    # is not evaluated for tag pushes.
    if "branches" in spec or "branches-ignore" in spec:
        return False
    return True


def discover(workflow_dir, tag):
    found = {}
    for name in sorted(os.listdir(workflow_dir)):
        if not name.endswith((".yml", ".yaml")):
            continue
        path = os.path.join(workflow_dir, name)
        parsed = parse_on(path)
        if parsed is None:
            raise FanoutError(
                f"{path}: no top-level `on:` block; refusing to guess whether "
                "it is tag-triggered"
            )
        events, push_spec = parsed
        if push_spec is not None and tag_triggers(tag, push_spec):
            found[name] = events
    return found


def read_manifest(path):
    entries = []
    with open(path, encoding="utf-8") as handle:
        for lineno, raw in enumerate(handle, 1):
            line = strip_inline_comment(raw).strip()
            if not line or line.startswith("#"):
                continue
            name = line.split("|", 1)[0].strip()
            if not name.endswith((".yml", ".yaml")):
                raise FanoutError(
                    f"{path}:{lineno}: expected a workflow file, got {line!r}"
                )
            entries.append(name)
    return entries


def verify(workflow_dir, manifest, tag):
    """Return (errors, discovered, entries); raise FanoutError when unsure."""
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
            errors.append(f"{manifest} lists {name}, but it does not trigger on tag {tag}")
        elif "workflow_dispatch" not in found[name]:
            errors.append(
                f"{name} triggers on tag {tag} but has no workflow_dispatch:, so "
                "the dispatch fallback cannot start it"
            )

    return errors, found, entries


def report(workflow_dir, manifest, tag):
    try:
        errors, found, entries = verify(workflow_dir, manifest, tag)
    except FanoutError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        print(
            "Refusing to create a release tag: classify the workflow explicitly.",
            file=sys.stderr,
        )
        return 1

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


def run_self_test():
    cases = [
        # (workflow yaml, manifest lines, tag, expected rc, description)
        (
            "name: A\non:\n  push:\n    tags:\n      - 'v*'\n  workflow_dispatch:\n",
            ["a.yml"],
            "v1.2.3",
            0,
            "block tags list is discovered and matched",
        ),
        (
            "name: A\non:\n  push:\n    tags: ['v*']\n  workflow_dispatch:\n",
            ["a.yml"],
            "v1.2.3",
            0,
            "flow tags list is discovered and matched",
        ),
        (
            "name: A\non:\n  'push': { tags: ['v*'] }\n  workflow_dispatch:\n",
            ["a.yml"],
            "v1.2.3",
            0,
            "quoted push key and single-line flow map",
        ),
        (
            "name: A\non:\n  push: {\n    tags: ['v*']\n  }\n  workflow_dispatch:\n",
            ["a.yml"],
            "v1.2.3",
            0,
            "multi-line flow map",
        ),
        (
            "name: A\non:\n  push:\n  workflow_dispatch:\n",
            ["a.yml"],
            "v1.2.3",
            0,
            "bare push runs on tags",
        ),
        (
            "name: A\non:\n  push:\n    paths: ['docs/**']\n  workflow_dispatch:\n",
            ["a.yml"],
            "v1.2.3",
            0,
            "paths-only push runs on tags (paths is not evaluated for tags)",
        ),
        (
            "name: A\non:\n  push:\n    tags-ignore: ['python-sdk/**']\n  workflow_dispatch:\n",
            ["a.yml"],
            "v1.2.3",
            0,
            "tags-ignore-only push runs on non-ignored tags",
        ),
        (
            "name: A\non:\n  push:\n    branches: [main]\n  workflow_dispatch:\n",
            [],
            "v1.2.3",
            0,
            "branch-only push does not run on tags",
        ),
        (
            "name: A\non:\n  push:\n    tags: ['v*']\n  workflow_dispatch:\n",
            [],
            "v1.2.3",
            1,
            "tag-triggered workflow missing from the manifest fails",
        ),
        (
            "name: A\non:\n  push:\n    tags: ['v*']\n",
            ["a.yml"],
            "v1.2.3",
            1,
            "tag-triggered workflow without workflow_dispatch fails",
        ),
        (
            "name: A\non:\n  push:\n    tags: ['python-sdk/v*']\n  workflow_dispatch:\n",
            [],
            "v1.2.3",
            0,
            "different tag series is not treated as a release tag",
        ),
        (
            "name: A\non:\n  push:\n    tags: ['v*', '!v0.*']\n  workflow_dispatch:\n",
            [],
            "v0.1.2",
            0,
            "negated tag pattern excludes the tag",
        ),
        (
            "name: A\non:\n  push:\n    tags:\n      - v*\n      - '!v0.91.*'\n  workflow_dispatch:\n",
            [],
            "v0.91.0",
            0,
            "a matching negative after a positive excludes the tag",
        ),
        (
            "name: A\non:\n  push:\n    tags:\n      - '!v0.*'\n      - v0.91.*\n  workflow_dispatch:\n",
            [],
            "v0.91.0",
            1,
            "a positive after a negative re-includes the tag",
        ),
        (
            "name: A\non:\n  push:\n    tags: ['v2.*',\n      'v0.91.*']\n  workflow_dispatch:\n",
            [],
            "v0.91.0",
            1,
            "multi-line flow list matches on a continuation line",
        ),
    ]

    with tempfile.TemporaryDirectory() as workdir:
        for index, (yaml_text, manifest, tag, expected, description) in enumerate(cases):
            case_dir = os.path.join(workdir, f"case-{index}")
            workflow_dir = os.path.join(case_dir, "workflows")
            os.makedirs(workflow_dir)
            with open(os.path.join(workflow_dir, "a.yml"), "w", encoding="utf-8") as handle:
                handle.write(yaml_text)
            manifest_path = os.path.join(case_dir, "release-workflows.txt")
            with open(manifest_path, "w", encoding="utf-8") as handle:
                handle.write("\n".join(manifest) + ("\n" if manifest else ""))

            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(
                io.StringIO()
            ):
                actual = report(workflow_dir, manifest_path, tag)
            if actual != expected:
                print(
                    f"FAIL: {description}: expected rc={expected}, got rc={actual}",
                    file=sys.stderr,
                )
                return 1

        # Unclassifiable push body must fail closed, not be treated as inert.
        case_dir = os.path.join(workdir, "case-unclassifiable")
        os.makedirs(case_dir)
        workflow_dir = os.path.join(case_dir, "workflows")
        os.makedirs(workflow_dir)
        with open(os.path.join(workflow_dir, "a.yml"), "w", encoding="utf-8") as handle:
            handle.write("name: A\non:\n  push: sometimes\n  workflow_dispatch:\n")
        manifest_path = os.path.join(case_dir, "release-workflows.txt")
        open(manifest_path, "w", encoding="utf-8").close()
        try:
            verify(workflow_dir, manifest_path, "v1.2.3")
        except FanoutError:
            pass
        else:
            print("FAIL: unclassifiable push body did not fail closed", file=sys.stderr)
            return 1

    print(f"self-test: {len(cases) + 1} cases passed")
    return 0


def main(argv):
    if len(argv) == 2 and argv[1] == "--self-test":
        return run_self_test()
    if len(argv) != 4:
        sys.stderr.write(__doc__)
        return 2
    return report(argv[1], argv[2], argv[3])


if __name__ == "__main__":
    sys.exit(main(sys.argv))
