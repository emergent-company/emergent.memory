#!/usr/bin/env python3
"""Reject workflow jobs that read a signing / App credential without declaring
the environment that owns it.

Why: repository secrets are handed to *every* workflow run from a
same-repository branch. GitHub withholds them only from fork PRs, so a
credential that can push a workflow file can add ``on: pull_request`` with
``run: … ${{ secrets.X }}``, open a PR, and read every repository secret with
no approval — branch protection gates the merge, not that run (#1232). Signing
and App material therefore belongs in environments whose deployment branch
policy refuses agent-pushed refs, read only by a job that declares the
environment.

This guard keeps the tree half of that invariant: a job that references a
mapped secret must declare the environment that holds it. A job that breaks the
rule fails closed once the repo-level copies are deleted — it sees an empty
value — and the failure would surface on the next tag release or dev dispatch,
which is expensive. Here it surfaces in the required ``ci`` job, on the PR that
introduces it.

The platform half cannot be checked from the tree: the environments must exist,
their deployment branch policy must name only the refs that deploy, their
secrets must hold the values, and no repo-level copy may remain.
``check-secret-environments.baseline`` carries the runbook.

The baseline is a SHRINKING list of jobs that still read the secret from repo
level. Each entry is ``<path><TAB><job><TAB><expected environment>``; it
accepts every finding in that job for that environment (and nothing else). An
entry that matches no finding is stale and fails, so the list cannot rot: the
PR that adds the ``environment:`` line deletes the entry. New jobs need the
line from the start.

Detection model: a small structural scan for the ``jobs:`` mapping (2-space
indentation, as every workflow here uses), each job's 4-space
``environment:`` property (plain or ``environment:`` + nested ``name:``), and
``secrets.<NAME>`` references in that job's body. YAML comments are masked, so
a comment that names a secret does not trip the guard. This is a tripwire, not
a security boundary: defence in depth still relies on the environment's
deployment branch policy and on reviewers.

Usage:
  check-secret-environments.py [--root DIR] [--baseline PATH]
  check-secret-environments.py --self-test

Defaults: the repository root from ``git rev-parse --show-toplevel`` and the
baseline at ``.github/scripts/check-secret-environments.baseline``.
"""

import os
import re
import subprocess
import sys

# Secret name -> the environment that owns it. Keep this in step with the
# environments: a value moved into an environment and left out of this map is
# silently readable from any job again.
ENVIRONMENT_SECRETS = {
    "signing": (
        "APPLE_DIST_CERT_BASE64",
        "APPLE_DIST_CERT_PASSWORD",
        "APP_STORE_CONNECT_API_KEY",
        "APP_STORE_CONNECT_ISSUER_ID",
        "APP_STORE_CONNECT_KEY_ID",
        "CERTIFICATE_BASE64",
        "CERTIFICATE_PASSWORD",
        "DEVELOPMENT_TEAM",
        "SPARKLE_PRIVATE_KEY",
    ),
    "dev-dispatch": (
        "GH_APP_ID",
        "GH_APP_PRIVATE_KEY",
    ),
}
SECRET_TO_ENVIRONMENT = {
    name: environment
    for environment, names in ENVIRONMENT_SECRETS.items()
    for name in names
}

WORKFLOW_DIR = ".github/workflows"
DEFAULT_BASELINE = ".github/scripts/check-secret-environments.baseline"

SECRET_RE = re.compile(r"\bsecrets\.([A-Za-z0-9_]+)\b")
# A job key under a top-level ``jobs:`` block (2-space indent).
JOB_HEADER_RE = re.compile(r"^  (?P<name>[A-Za-z0-9_][A-Za-z0-9_.-]*):[ \t]*(?:#.*)?$")
# A job-level ``environment:`` property (4-space indent under the job key).
JOB_ENV_RE = re.compile(r"^    environment:[ \t]*(?P<rest>.*)$")
# The ``name:`` of a nested ``environment:`` mapping.
NESTED_NAME_RE = re.compile(r"^[ \t]+name:[ \t]*(?P<rest>.*)$")


def strip_yaml_comment(line):
    """Drop a YAML comment (``#`` outside quotes, preceded by blank or at the
    start of the line) from a scalar, so a secret named in a comment is not
    mistaken for a reference."""
    quote = None
    prev = " "
    for i, c in enumerate(line):
        if quote is not None:
            if c == quote:
                quote = None
        elif c in "'\"":
            quote = c
        elif c == "#" and prev in " \t":
            return line[:i].rstrip()
        prev = c
    return line.rstrip()


def secret_names(line):
    """Return the secret names referenced by ``line``, ignoring comments."""
    code = strip_yaml_comment(line)
    if not code.strip():
        return []
    return SECRET_RE.findall(code)


def iter_jobs(lines):
    """Yield ``{"name": str, "lines": [(line_no, line), ...]}`` for every job
    under the top-level ``jobs:`` mapping, its header line excluded."""
    current = None
    in_jobs = False
    for line_no, raw in enumerate(lines, 1):
        line = raw.rstrip("\r")
        if not in_jobs:
            if line.rstrip() == "jobs:":
                in_jobs = True
            continue
        if not line.strip() or line.lstrip().startswith("#"):
            if current is not None:
                current["lines"].append((line_no, line))
            continue
        indent = len(line) - len(line.lstrip(" "))
        if indent == 0:
            # A new top-level key ends the jobs block.
            if current is not None:
                yield current
                current = None
            in_jobs = False
            continue
        header = JOB_HEADER_RE.match(line)
        if indent == 2 and header:
            if current is not None:
                yield current
            current = {"name": header.group("name"), "lines": []}
            continue
        if current is not None:
            current["lines"].append((line_no, line))
    if current is not None:
        yield current


def job_environment(body):
    """Return ``(environment_name_or_None, line_no)`` for a job's
    ``environment:`` property. ``None`` means the job declares no environment;
    an empty string means one the scan could not resolve (a mismatch)."""
    for idx, (line_no, line) in enumerate(body):
        m = JOB_ENV_RE.match(line)
        if not m:
            continue
        rest = strip_yaml_comment(m.group("rest")).strip()
        if rest:
            return rest.strip("'\""), line_no
        # ``environment:`` + nested ``name:`` mapping.
        for _line_no2, line2 in body[idx + 1 :]:
            if not line2.strip():
                continue
            indent = len(line2) - len(line2.lstrip(" "))
            if indent <= 4:
                break
            nested = NESTED_NAME_RE.match(line2)
            if nested:
                value = strip_yaml_comment(nested.group("rest")).strip().strip("'\"")
                return value, line_no
            break
        return "", line_no
    return None, None


def scan_text(text):
    """Return ``(line_no, secret, job_or_None, expected_environment)`` findings
    in ``text``: every reference to a mapped secret that is not inside a job
    declaring the environment that owns it."""
    lines = text.split("\n")
    findings = []
    accepted = set()
    for job in iter_jobs(lines):
        environment, _env_line = job_environment(job["lines"])
        for line_no, line in job["lines"]:
            for name in secret_names(line):
                expected = SECRET_TO_ENVIRONMENT.get(name)
                if expected is None:
                    continue
                if environment == expected:
                    accepted.add((line_no, name))
                else:
                    findings.append((line_no, name, job["name"], expected))
    # Catch a reference outside any job (e.g. a workflow-level env:) and any
    # job line the scan could not attribute to an accepted pair.
    for line_no, line in enumerate(lines, 1):
        for name in secret_names(line):
            expected = SECRET_TO_ENVIRONMENT.get(name)
            if expected is None or (line_no, name) in accepted:
                continue
            if any(f[0] == line_no and f[1] == name for f in findings):
                continue
            findings.append((line_no, name, None, expected))
    findings.sort(key=lambda f: (f[0], f[1]))
    return findings


def describe(finding):
    line_no, name, job, expected = finding
    where = "outside any job" if job is None else f"in job `{job}`"
    return (
        f"secrets.{name} referenced {where} without the environment that owns "
        f"it; expected environment `{expected}`"
    )


def read_baseline(path):
    """Return a sorted list of ``(path, job, environment)`` baseline entries."""
    entries = []
    if not os.path.exists(path):
        return entries
    with open(path, encoding="utf-8") as f:
        for raw in f:
            line = raw.rstrip("\n")
            if not line.strip() or line.lstrip().startswith("#"):
                continue
            parts = line.split("\t")
            if len(parts) != 3:
                raise SystemExit(
                    f"{path}: malformed baseline entry (want path<TAB>job<TAB>"
                    f"environment): {line!r}"
                )
            entries.append(tuple(part.strip() for part in parts))
    return sorted(entries)


def repo_root():
    try:
        return subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        raise SystemExit("check-secret-environments: not inside a git repository")


def scan_tree(root, workflow_dir=WORKFLOW_DIR):
    """Return every finding in the workflow directory as
    ``(path, line_no, secret, job, expected_environment)``, path relative to
    ``root``."""
    base = os.path.join(root, workflow_dir)
    findings = []
    names = sorted(os.listdir(base)) if os.path.isdir(base) else []
    for name in names:
        if not name.endswith((".yml", ".yaml")):
            continue
        path = os.path.join(base, name)
        with open(path, encoding="utf-8") as f:
            text = f.read()
        rel = os.path.join(workflow_dir, name)
        for line_no, secret, job, expected in scan_text(text):
            findings.append((rel, line_no, secret, job, expected))
    return findings


def check(root, baseline_path):
    findings = scan_tree(root)
    baseline = read_baseline(baseline_path)
    accepted = {(path, job, environment) for path, job, environment in baseline}
    finding_keys = {(path, job, expected) for path, _no, _s, job, expected in findings}

    new = [f for f in findings if (f[0], f[3], f[4]) not in accepted]
    stale = sorted(accepted - finding_keys)

    if new:
        print(
            "secret-environment guard: signing/App secrets read without the "
            "environment that owns them:",
            file=sys.stderr,
        )
        for path, line_no, secret, job, expected in new:
            print(f"  {path}:{line_no}: {describe((line_no, secret, job, expected))}", file=sys.stderr)
        print(
            "  add `environment: <name>` to that job and delete its entry from "
            f"{os.path.relpath(baseline_path, root)}; never read the value from "
            "repo level.",
            file=sys.stderr,
        )
    if stale:
        print(
            "secret-environment guard: stale baseline entr(ies) -- no such "
            f"finding remains, so delete them from {os.path.relpath(baseline_path, root)}:",
            file=sys.stderr,
        )
        for path, job, environment in stale:
            print(f"  {path}\t{job}\t{environment}", file=sys.stderr)
    if new or stale:
        return 1
    print(
        f"secret-environment guard: OK ({len(baseline)} baselined job(s) awaiting "
        "their environment line, no new findings)"
    )
    return 0


# ---------------------------------------------------------------------------
# Self-test fixtures for the scanner: (label, workflow_text, expect_finding)
# ---------------------------------------------------------------------------

_CLEAN = """\
name: clean
on:
  push:
    tags: ['v*']
permissions:
  contents: read
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
  build:
    runs-on: [self-hosted, macOS, ARM64, mac]
    needs: check
    environment: signing
    env:
      DEVELOPMENT_TEAM: ${{ secrets.DEVELOPMENT_TEAM }}
    steps:
      - name: key
        env:
          SPARKLE_PRIVATE_KEY: ${{ secrets.SPARKLE_PRIVATE_KEY }}
          APP_STORE_CONNECT_KEY_ID: ${{ secrets.APP_STORE_CONNECT_KEY_ID }}
        run: echo signing
  dispatch:
    runs-on: ubuntu-latest
    environment:
      name: dev-dispatch
    steps:
      - env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: echo "${{ secrets.GH_APP_ID }}"
"""

_FIXTURES = [
    ("clean: signing + nested dev-dispatch, GITHUB_TOKEN unmapped", _CLEAN, False),
    (
        "missing environment",
        """\
name: x
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - env:
          SPARKLE_PRIVATE_KEY: ${{ secrets.SPARKLE_PRIVATE_KEY }}
        run: echo signing
""",
        True,
    ),
    (
        "wrong environment",
        """\
name: x
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    environment: dev-dispatch
    steps:
      - env:
          CERTIFICATE_BASE64: ${{ secrets.CERTIFICATE_BASE64 }}
        run: echo signing
""",
        True,
    ),
    (
        "workflow-level env: reference",
        """\
name: x
on: [push]
env:
  SPARKLE_PRIVATE_KEY: ${{ secrets.SPARKLE_PRIVATE_KEY }}
jobs:
  build:
    runs-on: ubuntu-latest
    environment: signing
    steps:
      - run: echo hi
""",
        True,
    ),
    (
        "one good job, one job without the environment",
        """\
name: x
on: [push]
jobs:
  good:
    runs-on: ubuntu-latest
    environment: signing
    steps:
      - run: echo "${{ secrets.CERTIFICATE_PASSWORD }}"
  bad:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ secrets.CERTIFICATE_PASSWORD }}"
""",
        True,
    ),
    (
        "comment naming a secret is inert",
        """\
name: x
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    # was: secrets.SPARKLE_PRIVATE_KEY here before #1232
    steps:
      - run: echo ok
""",
        False,
    ),
    (
        "unmapped secret needs no environment",
        """\
name: x
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ secrets.SOME_OTHER_TOKEN }}"
""",
        False,
    ),
]


def self_test():
    failures = []
    if scan_text(_CLEAN):
        failures.append("_CLEAN should have no findings")
    for label, text, expect in _FIXTURES:
        found = scan_text(text)
        verdict = "FLAG" if found else "PASS"
        want = "FLAG" if expect else "PASS"
        if verdict != want:
            failures.append(f"{label}: got {verdict}, want {want}")
    # The reported line number must be the referencing line, not the job header.
    missing = scan_text(_FIXTURES[1][1])
    if len(missing) != 1 or missing[0][0] != 8:
        failures.append(f"line attribution: got {missing}, want one finding on line 8")
    if failures:
        for failure in failures:
            print("self-test FAILED:", failure, file=sys.stderr)
        return 1
    print(
        f"self-test OK: {len(_FIXTURES)} fixtures + clean workflow passed "
        "(environment presence, mapping form, wrong environment, comments)"
    )
    return 0


def main(argv):
    if "--self-test" in argv:
        return self_test()
    root = ""
    baseline_path = ""
    args = list(argv)
    while args:
        arg = args.pop(0)
        if arg in ("--root", "--baseline"):
            if not args:
                print(f"usage: check-secret-environments.py {arg} PATH", file=sys.stderr)
                return 2
            value = args.pop(0)
            if arg == "--root":
                root = value
            else:
                baseline_path = value
        elif arg in ("-h", "--help"):
            print(
                "usage: check-secret-environments.py [--root DIR] "
                "[--baseline PATH] | --self-test"
            )
            return 0
        else:
            print(f"check-secret-environments: unknown argument: {arg}", file=sys.stderr)
            return 2
    if not root:
        root = repo_root()
    if not baseline_path:
        baseline_path = os.path.join(root, DEFAULT_BASELINE)
    return check(root, baseline_path)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
