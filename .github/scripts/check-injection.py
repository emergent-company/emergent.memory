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

Known, deliberate exclusions (not attacker-controlled text, and not part of the
class this guard protects against):
  * ``github.event_name`` — a fixed enum (``push``/``pull_request``/…), no dot.
  * ``github.repository`` / ``github.repository_owner`` / ``github.actor`` /
    ``github.sha`` / ``matrix.*`` / ``runner.*`` / ``env.*`` / ``secrets.*`` —
    repo identity, fixed config, or secret-handling concerns.
  * Interpolations inside a *single-quoted* heredoc (``<<'EOF'``) of a ``run:``
    body: the shell does not interpret quoted-heredoc content, so the expanded
    text is inert data, never code.

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

# A ``run:``/``script:`` mapping key followed by a block scalar (``|``, ``>``,
# with optional chomping/indentation indicators) or an inline scalar.
KEY_RE = re.compile(r"^(\s*)(run|script):\s*(.*)$")
BLOCK_RE = re.compile(r"^[|>]")

# A single-quoted heredoc opener, e.g. ``<<'EOF'``, ``<<- 'EOF'``, ``<<"EOF"``.
# ``$GITHUB_OUTPUT`` multi-line markers (``echo "commits<<EOF"``) are inside
# double quotes and have no quote char immediately after ``<<``, so they do not
# match — they are not heredocs.
HEREDOC_RE = re.compile(r"<<-?\s*(?P<q>'[^']*'|\"[^\"]*\")")


def iter_scalars(text):
    """Yield ``(kind, first_body_line_no, body_lines)`` for every run:/script:
    scalar value in ``text`` (kind is ``'run'`` or ``'script'``)."""
    lines = text.split("\n")
    i = 0
    n = len(lines)
    while i < n:
        m = KEY_RE.match(lines[i])
        if not m:
            i += 1
            continue
        indent = len(m.group(1))
        kind = m.group(2)
        rest = m.group(3).rstrip()
        if BLOCK_RE.match(rest):
            body = []
            j = i + 1
            while j < n:
                line = lines[j]
                if line.strip() == "":
                    body.append(line)
                    j += 1
                    continue
                cur_indent = len(line) - len(line.lstrip(" "))
                if cur_indent > indent:
                    body.append(line)
                    j += 1
                else:
                    break
            yield kind, i + 2, body  # first body line is the line after the key
            i = j
        else:
            yield kind, i + 1, [rest]  # inline scalar lives on the key line
            i += 1


def find_violations(kind, first_line_no, body_lines):
    """Return a list of ``(file_line_no, expression)`` for dangerous
    interpolations in a run:/script: body. Single-quoted heredoc regions of a
    ``run:`` body are treated as inert data and skipped."""
    violations = []
    in_heredoc = None
    for offset, line in enumerate(body_lines):
        file_line_no = first_line_no + offset
        if kind == "run" and in_heredoc is not None:
            if line.strip() == in_heredoc:
                in_heredoc = None
            continue
        if kind == "run":
            h = HEREDOC_RE.search(line)
            if h:
                # Scan the code before ``<<``; the heredoc body is inert.
                prefix = line[: h.start()]
                for em in DANGEROUS.finditer(prefix):
                    violations.append((file_line_no, em.group(0)))
                in_heredoc = h.group("q").strip("'\"")
                continue
        for em in DANGEROUS.finditer(line):
            violations.append((file_line_no, em.group(0)))
    return violations


def scan_text(text):
    """Return a list of ``(file_line_no, expression)`` violations in ``text``."""
    violations = []
    for kind, first_line_no, body in iter_scalars(text):
        violations.extend(find_violations(kind, first_line_no, body))
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


CLEAN_WORKFLOW = """\
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

VIOLATING_WORKFLOW = """\
name: violating
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: bad
        run: TAG="${{ github.ref_name }}"
"""


def self_test():
    failures = []
    if scan_text(CLEAN_WORKFLOW):
        failures.append("CLEAN_WORKFLOW should have no violations")
    if not scan_text(VIOLATING_WORKFLOW):
        failures.append("VIOLATING_WORKFLOW should have a violation")
    if failures:
        for f in failures:
            print("self-test FAILED:", f, file=sys.stderr)
        return 1
    print("self-test OK: guard flags injected violations and accepts env/heredoc patterns")
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
