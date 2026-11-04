#!/usr/bin/env python3
"""Reject dynamic ${{ ... }} interpolation inside GitHub Actions shell (run:)
and github-script javascript (script:) payloads.

Why: GitHub expands ``${{ ... }}`` into the *raw text* of a ``run:``/``script:``
body before the shell or JS engine parses it. Values that carry untrusted
ref/commit/input/event text — branch and tag names (``github.ref*``), PR refs
(``github.head_ref``/``github.base_ref``), workflow inputs (``inputs.*``),
job/step outputs (``steps.*.outputs.*`` / ``needs.*.outputs.*``), and event
payload fields (``github.event.*``) — must therefore never be expanded there.
The same value is safe when passed through ``env:`` and read as ``"${VAR}"`` in
shell or ``process.env.VAR`` in JS, because environment-variable expansion is
not re-parsed for command substitution.

Detection model
---------------
The scanner walks the YAML structurally (Python 3 stdlib only, no external YAML
dependency) and locates ``run`` / ``with.script`` scalars in these spellings:

  * block scalars (``run: |``, ``run: >``) and inline scalars (``run: echo …``),
    in mapping form (``run:`` at line start) and list-item form (``- run:``);
  * single-line flow collections: ``- {name: x, run: "…"}`` and
    ``steps: [{run: '…'}]`` (``run``/``script`` keys inside ``{ … }``/``[ … ]``).

For each scalar it scans the raw text for the untrusted-expression families.
Within a ``run:`` body it applies shell-aware masking:

  * a *quoted* heredoc (``<<'EOF'``, ``<<-"EOF"``) — the shell does not expand
    the body, so it is inert data and is skipped;
  * an *unquoted* heredoc (``<<EOF``) — the body is evaluated, so it is scanned;
  * the opener line's redirect target (``cat <<'EOF' > "${{…}}.md"``) is still
    scanned — only the ``<< 'EOF'`` token itself is removed;
  * a full-line shell comment (first non-blank char ``#``) is not scanned.

For flow-style steps, the ``run``/``script`` value is extracted (quoted or
plain, up to the flow delimiter at depth 0) so that interpolations in *other*
keys of the same flow mapping (e.g. a ``name:``) are not mistaken for payloads.
YAML comments (``# …``) and quoted literals are masked before a ``run``/``script``
key is recognised, so a literal ``run:`` inside a string or comment is ignored.

Known, deliberate exclusions (not attacker-controlled text):
  * ``github.event_name`` — a fixed enum (``push``/``pull_request``/…), no dot.
  * ``github.repository`` / ``github.repository_owner`` / ``github.actor`` /
    ``github.sha`` / ``matrix.*`` / ``runner.*`` / ``env.*`` / ``secrets.*`` —
    repo identity, fixed config, or secret-handling concerns.

This is a **tripwire, not a security boundary**: it detects the interpolation
spellings enumerated above so the common forms are kept out of the tree. It can
be bypassed by creative YAML the scanner does not model (plain multi-line
scalars with continuation lines, or other unusual encodings); defence-in-depth
still relies on reviewers not re-introducing the pattern.

Usage:
  check-injection.py [WORKFLOW_DIR_OR_FILE ...]   # scan (default .github/workflows)
  check-injection.py --self-test                  # run built-in fixtures
"""

import os
import re
import sys

# Dynamic/ untrusted expression families. Each must appear as the prefix of the
# expression inside ``${{ ... }}``. Order does not matter (alternation).
FAMILIES = (
    r"github\.ref(?:_name|_type|_protected)?\b",   # branch/tag name
    r"github\.(head_ref|base_ref)\b",              # PR refs
    r"github\.event\.",                            # event payload fields
    r"\binputs\.",                                 # workflow inputs
    r"steps\.[A-Za-z0-9_-]+\.outputs\.",           # step outputs
    r"needs\.[A-Za-z0-9_-]+\.outputs\.",           # job outputs
)
DANGEROUS = re.compile(r"\$\{\{\s*(?:" + "|".join(FAMILIES) + r")")

# A ``run:``/``script:`` mapping key, in either block/list-item spelling:
#   run: …            (mapping key)
#   - run: …          (sequence item with an inline mapping)
# ``m.start('key')`` is the column where ``run``/``script`` begins, which is the
# indentation a following block scalar must exceed.
KEY_RE = re.compile(r"^(?P<indent>[ \t]*)(?:(?P<dash>-)[ \t]+)?(?P<key>run|script):(?P<rest>.*)$")

# Block scalar indicator (``|``, ``>`` with optional chomping/indent modifiers).
BLOCK_RE = re.compile(r"^[|>]")

# A *quoted* heredoc opener, e.g. ``<<'EOF'``, ``<<- 'EOF'``, ``<<"EOF"``.
# ``$GITHUB_OUTPUT`` multi-line markers (``echo "commits<<EOF"``) are inside
# double quotes and have no quote char immediately after ``<<``, so they do not
# match — they are not heredocs. Unquoted heredocs (``<<EOF``) do not match
# either, so their (evaluated) bodies are scanned as code.
HEREDOC_RE = re.compile(r"<<-?\s*(?P<q>'[^']*'|\"[^\"]*\")")

# A ``run``/``script`` flow key: the word ``run``/``script`` (not a substring of
# a longer identifier) followed by ``:``. Used only after quotes/comments are
# masked, and gated on being inside a flow ``{ … }``/``[ … ]`` collection.
FLOW_KEY_RE = re.compile(r"(?<![A-Za-z0-9_-])(run|script):")


def strip_yaml_comment(s):
    """Drop a trailing YAML comment (``#`` outside quotes preceded by blank)
    from a plain inline scalar. ``#`` inside quotes (e.g. ``"${VERSION#v}"``) is
    content, not a comment."""
    quote = None
    i = 0
    prev = " "
    while i < len(s):
        c = s[i]
        if quote is not None:
            if c == quote:
                quote = None
        elif c in "'\"":
            quote = c
        elif c == "#" and prev in " \t":
            return s[:i].rstrip()
        prev = c
        i += 1
    return s.rstrip()


def is_comment_line(line, kind):
    """True when ``line`` is purely a comment in the given payload language."""
    stripped = line.lstrip(" \t")
    if kind == "run":
        return stripped.startswith("#")
    return stripped.startswith("//") or stripped.startswith("/*") or stripped.startswith("*")


def mask_quotes_comments(line):
    """Return a same-length copy of ``line`` with quoted-string contents and
    YAML comments blanked to spaces, leaving only unquoted, non-comment text.
    Used to locate ``run``/``script`` keys without matching a literal ``run:``
    inside a string or a comment."""
    out = list(line)
    quote = None
    i = 0
    n = len(line)
    while i < n:
        c = line[i]
        if quote is not None:
            out[i] = " "
            if c == quote and not (quote == '"' and i > 0 and line[i - 1] == "\\"):
                quote = None
            i += 1
            continue
        if c in "'\"":
            quote = c
            out[i] = " "
            i += 1
            continue
        if c == "#" and i > 0 and line[i - 1] in " \t":
            while i < n:
                out[i] = " "
                i += 1
            break
        i += 1
    return "".join(out)


def flow_value(line, colon_end):
    """Extract the value of a flow key whose ``:`` ends at ``colon_end`` (index
    of the char after ``:``). Handles a quoted scalar (``'…'``/``"…"``) or a
    plain scalar that runs until a flow delimiter (`,``/``}``/``]`) at depth 0."""
    i = colon_end
    n = len(line)
    while i < n and line[i] in " \t":
        i += 1
    if i >= n:
        return ""
    c = line[i]
    if c in "'\"":
        q = c
        j = i + 1
        while j < n:
            if line[j] == q and not (q == '"' and line[j - 1] == "\\"):
                return line[i : j + 1]
            j += 1
        return line[i:]
    j = i
    depth = 0
    while j < n:
        ch = line[j]
        if depth == 0 and ch in ",}]":
            break
        if ch in "{[":
            depth += 1
        elif ch in "}]":
            depth -= 1
        j += 1
    return line[i:j]


def flow_violations(line):
    """Return the dangerous expressions interpolated into flow-style
    ``run``/``script`` values on a single line."""
    masked = mask_quotes_comments(line)
    violations = []
    for m in FLOW_KEY_RE.finditer(masked):
        prefix = masked[: m.start()]
        depth = prefix.count("{") + prefix.count("[") - prefix.count("}") - prefix.count("]")
        if depth <= 0:
            continue  # not inside a flow collection; e.g. a plain scalar 'run:' in a name
        value = flow_value(line, m.end())
        for em in DANGEROUS.finditer(value):
            violations.append(em.group(0))
    return violations


def iter_scalars(text):
    """Yield ``(kind, first_body_line_no, body_lines, flow)`` for every run:/
    script: scalar in ``text``. ``flow`` is True for single-line flow-style
    steps (``{… run: …}`` / ``[… run: …]``)."""
    lines = text.split("\n")
    i = 0
    n = len(lines)
    while i < n:
        line = lines[i]
        m = KEY_RE.match(line)
        if m:
            kind = m.group("key")
            key_col = m.start("key")
            rest = m.group("rest")
            rest = rest.strip() if rest.strip() else rest
            if BLOCK_RE.match(rest.lstrip()) or rest.strip() == "":
                body = []
                j = i + 1
                while j < n:
                    linej = lines[j]
                    if linej.strip() == "":
                        body.append(linej)
                        j += 1
                        continue
                    cur_indent = len(linej) - len(linej.lstrip(" \t"))
                    if cur_indent > key_col:
                        body.append(linej)
                        j += 1
                    else:
                        break
                yield kind, i + 2, body, False
                i = j
            else:
                yield kind, i + 1, [strip_yaml_comment(rest)], False
                i += 1
            continue
        # flow-style probe: run:/script: inside {…} or […] on this line
        if FLOW_KEY_RE.search(mask_quotes_comments(line)):
            yield "run", i + 1, [line], True
        i += 1


def find_violations(kind, first_line_no, body_lines, flow):
    """Return a list of ``(file_line_no, expression)`` for dangerous
    interpolations in a run:/script: body. Quoted-heredoc bodies of a ``run:``
    body are inert and skipped; unquoted-heredoc bodies are scanned."""
    violations = []
    if flow:
        for offset, line in enumerate(body_lines):
            for expr in flow_violations(line):
                violations.append((first_line_no + offset, expr))
        return violations
    in_heredoc = None
    for offset, line in enumerate(body_lines):
        file_line_no = first_line_no + offset
        if kind == "run":
            if in_heredoc is not None:
                if line.strip() == in_heredoc:
                    in_heredoc = None
                continue
            h = HEREDOC_RE.search(line)
            if h:
                line = line[: h.start()] + line[h.end():]
                in_heredoc = h.group("q").strip("'\"")
            if is_comment_line(line, "run"):
                continue
        else:
            if is_comment_line(line, "script"):
                continue
        for em in DANGEROUS.finditer(line):
            violations.append((file_line_no, em.group(0)))
    return violations


def scan_text(text):
    """Return a list of ``(file_line_no, expression)`` violations in ``text``."""
    violations = []
    for kind, first_line_no, body, flow in iter_scalars(text):
        violations.extend(find_violations(kind, first_line_no, body, flow))
    return violations


def scan_path(path):
    violations = []
    if os.path.isdir(path):
        for name in sorted(os.listdir(path)):
            if name.endswith((".yml", ".yaml")):
                violations.extend(scan_path(os.path.join(path, name)))
        return violations
    with open(path, encoding="utf-8") as f:
        text = f.read()
    for line_no, expr in scan_text(text):
        violations.append((path, line_no, expr))
    return violations


# ---------------------------------------------------------------------------
# Self-test fixtures: (label, workflow_text, expect_violation)
# ---------------------------------------------------------------------------

_CLEAN = """\
name: clean
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: version
        id: version
        run: echo "version=${GITHUB_REF_NAME#v}" >> "$GITHUB_OUTPUT"
      - name: build
        env:
          VERSION: ${{ steps.version.outputs.version }}
        run: |
          go build -ldflags="-X main.version=${VERSION}" ./cmd
      - name: changelog
        uses: actions/github-script@v7
        env:
          COMMITS: ${{ steps.commits.outputs.commits }}
        with:
          script: |
            const c = process.env.COMMITS || '';
            console.log(c);
      - name: release notes
        run: |
          cat > notes.md << 'EOF'
          Download: https://example.com/${{ github.repository }}/${{ github.ref_name }}
          EOF
"""

_FIXTURES = [
    ("- run: inline", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.ref_name }}"
""", True),
    ("- run: | block", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: |
          echo "${{ github.ref_name }}"
""", True),
    ("- run: > folded", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: >
          echo "${{ github.ref_name }}"
""", True),
    ("run: | block (mapping form)", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - name: build
        run: |
          echo "${{ github.ref_name }}"
""", True),
    ("github-script script:", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/github-script@v7
        with:
          script: |
            console.log("${{ github.head_ref }}");
""", True),
    ("github.event.head_commit.message", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.event.head_commit.message }}"
""", True),
    ("unquoted heredoc body", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: |
          cat << EOF
          ${{ github.ref_name }}
          EOF
""", True),
    ("heredoc redirect target", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: |
          cat << 'EOF' > "notes-${{ github.ref_name }}.md"
          body
          EOF
""", True),
    ("flow mapping (- {name: x, run: \"…\"})", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - {name: x, run: "echo ${{ github.ref_name }}"}
""", True),
    ("flow sequence (steps: [{run: '…'}])", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps: [{run: 'echo ${{ github.ref_name }}'}]
""", True),
    # --- clean fixtures ---
    ("env value (clean)", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${VERSION}"
        env:
          VERSION: ${{ steps.version.outputs.version }}
""", False),
    ("non-run/script step key (clean)", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - name: Memory ${{ github.ref_name }}
        run: echo "hi"
""", False),
    ("comment line (clean)", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: |
          # ${{ github.ref_name }} is a comment
          echo "done"
""", False),
    ("quoted heredoc body (clean)", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: |
          cat << 'EOF'
          ${{ github.ref_name }}
          EOF
""", False),
    ("flow: literal run: in quoted value (clean)", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - {name: x, run: "echo see run: docs"}
""", False),
    ("flow: run: in name value (clean)", """\
name: x
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - {name: Memory ${{ github.ref_name }}, run: echo hi}
""", False),
]


def self_test():
    failures = []
    if scan_text(_CLEAN):
        failures.append("_CLEAN should have no violations")
    for label, text, expect in _FIXTURES:
        found = bool(scan_text(text))
        verdict = "FLAG" if found else "PASS"
        want = "FLAG" if expect else "PASS"
        if verdict != want:
            failures.append(f"{label}: got {verdict}, want {want}")
    if failures:
        for f in failures:
            print("self-test FAILED:", f, file=sys.stderr)
        return 1
    n = len(_FIXTURES)
    print(f"self-test OK: {n} fixtures + clean workflow all passed (incl. - run: and flow forms)")
    return 0


def main(argv):
    if "--self-test" in argv:
        return self_test()
    targets = [a for a in argv if not a.startswith("-")] or [".github/workflows"]
    all_violations = []
    for target in targets:
        all_violations.extend(scan_path(target))
    if all_violations:
        print(
            "workflow injection guard: dynamic ${{ ... }} interpolation into run:/script: payloads:",
            file=sys.stderr,
        )
        for path, line_no, expr in all_violations:
            print(f"  {path}:{line_no}: {expr}", file=sys.stderr)
        print(
            "  pass the value via env: and reference ${VAR} (shell) or process.env.* (JS).",
            file=sys.stderr,
        )
        return 1
    print("workflow injection guard: no dynamic interpolation into run:/script: payloads")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
