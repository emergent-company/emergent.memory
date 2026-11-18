#!/usr/bin/env python3
"""Reconcile `VERSION` on `main` against the release tag, and heal the gap.

Why this exists (#1047)
-----------------------
`auto-tag.yml` creates the release tag and starts the whole release fan-out.
Its only entry point used to be

    on: push: branches: [main], paths: ['VERSION']

and that is not enough: a release PR merged by ``app/github-actions`` — which is
what the reviewed path produces, because ``github-review-gate.yml`` arms
auto-merge and GitHub merges it as the Actions app — raises a push event from
``GITHUB_TOKEN``, and GitHub starts **no workflow runs** for those. The tag is
never created and the fan-out never starts. Observed on v0.104.0: two merged
commits, ``actions/runs?head_sha=...`` returning ``total_count: 0`` for both,
and a hand-pushed annotated tag as the only recovery. Same shape as #116: a
marker path that is correct and then observed by nobody.

Nothing observer-shaped can hang off that push, so the observer has to be the
kind of event the suppression does not cover. A ``schedule`` is one of them
(``workflow_dispatch`` and ``repository_dispatch`` are the other documented
exceptions). This check runs on a schedule and on demand, reads `VERSION` on
`main`, and asks the Actions API whether the tag exists or a run is already in
flight:

  * tag exists                     -> consistent, nothing to do;
  * `[skip release]` bump commit   -> deliberately not tagged, nothing to do;
  * auto-tag run queued/running    -> in flight, wait;
  * auto-tag run already finished  -> drift: the run happened and the tag is
                                      still missing, so this fails loudly
                                      instead of dispatching in a loop;
  * nothing ran                    -> dispatch `auto-tag.yml` on `main`. A
                                      dispatch-triggered run *does* start from
                                      a `GITHUB_TOKEN`, so this is the heal.

`auto-tag.yml` is idempotent (it exits early when the tag exists), so a
dispatch that races a human recovery is harmless.

Exit codes:
  0  consistent, deliberately skipped, in flight, or healed by a dispatch
  1  drift that needs a human (a run finished with the tag still missing), or
     the API/inputs cannot be read

Usage:
  reconcile-release-tag.py [--repo OWNER/NAME] [--branch main] [--dry-run]

Stdlib only, like ``check-release-fanout.py``: nothing but ``gh`` is assumed on
the runner.
"""

import argparse
import datetime
import json
import os
import subprocess
import sys

AUTO_TAG_WORKFLOW = "auto-tag.yml"
AUTO_TAG_PATH = f".github/workflows/{AUTO_TAG_WORKFLOW}"
SKIP_MARKER = "[skip release]"
IN_FLIGHT = {"queued", "in_progress", "waiting", "requested", "pending"}


class ReconcileError(Exception):
    """The release state cannot be determined; fail closed."""


def gh(*args):
    """Run `gh` and return the completed process, text mode."""
    return subprocess.run(
        ["gh", *args],
        capture_output=True,
        text=True,
        env=os.environ.copy(),
    )


def api_json(path):
    result = gh("api", path)
    if result.returncode != 0:
        raise ReconcileError(
            f"gh api {path} failed: {result.stderr.strip() or result.stdout.strip()}"
        )
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise ReconcileError(f"gh api {path} returned invalid JSON: {exc}") from exc


def read_version(repo, branch):
    result = gh(
        "api",
        "-H",
        "Accept: application/vnd.github.raw",
        f"repos/{repo}/contents/VERSION?ref={branch}",
    )
    if result.returncode != 0:
        raise ReconcileError(
            f"cannot read VERSION on {branch}: "
            f"{result.stderr.strip() or result.stdout.strip()}"
        )
    version = result.stdout.strip()
    if not version:
        raise ReconcileError(f"VERSION on {branch} is empty")
    parts = version.split(".")
    if len(parts) != 3 or not all(part.isdigit() for part in parts):
        raise ReconcileError(
            f"VERSION on {branch} is {version!r}, expected MAJOR.MINOR.PATCH"
        )
    return version


def tag_exists(repo, tag):
    result = gh("api", f"repos/{repo}/git/ref/tags/{tag}")
    if result.returncode == 0:
        return True
    # 404 is the only "absent" answer; anything else is an API failure and must
    # not read as "no tag" — that would dispatch a tag creation on a broken API.
    if "HTTP 404" in result.stderr or "Not Found" in result.stderr:
        return False
    raise ReconcileError(
        f"cannot check tag {tag}: {result.stderr.strip() or result.stdout.strip()}"
    )


def bump_commit(repo, branch):
    """The last commit that changed VERSION on `branch`."""
    commits = api_json(f"repos/{repo}/commits?path=VERSION&sha={branch}&per_page=1")
    if not isinstance(commits, list) or not commits:
        raise ReconcileError(f"no commit touching VERSION on {branch}")
    commit = commits[0]
    try:
        sha = commit["sha"]
        message = commit["commit"]["message"]
        created = commit["commit"]["committer"]["date"]
    except (KeyError, TypeError) as exc:
        raise ReconcileError(f"unexpected commit payload: {exc}") from exc
    return sha, message, created


def parse_time(value):
    # GitHub returns ISO-8601 with a trailing `Z`, which
    # datetime.fromisoformat only accepts from Python 3.11 on.
    try:
        return datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ReconcileError(f"unparseable timestamp {value!r}: {exc}") from exc


def auto_tag_runs(repo, branch, since):
    """auto-tag runs on `branch` created at or after `since`."""
    # The per-workflow endpoint, not the repo-wide runs list: the latter is
    # capped at the newest 100 runs on the branch, and a busy `main` could push
    # the auto-tag run out of the window and turn "in flight" into a second
    # dispatch.
    runs = api_json(
        f"repos/{repo}/actions/workflows/{AUTO_TAG_WORKFLOW}/runs"
        f"?branch={branch}&per_page=100"
    )
    if not isinstance(runs, dict):
        raise ReconcileError("unexpected actions/runs payload")
    selected = []
    for run in runs.get("workflow_runs") or []:
        if run.get("path") != AUTO_TAG_PATH:
            continue
        created = run.get("created_at")
        if not created:
            # A run without a timestamp must not be hidden: including it can
            # only make this check wait or report drift, never re-dispatch.
            selected.append(run)
            continue
        try:
            stamp = parse_time(created)
        except ReconcileError:
            # An unparseable timestamp must not hide a run: including it can
            # only make this check wait or report drift, never re-dispatch.
            selected.append(run)
            continue
        if stamp >= since:
            selected.append(run)
    return sorted(selected, key=lambda run: run.get("created_at") or "")


def describe(run):
    status = run.get("status") or "unknown"
    conclusion = run.get("conclusion") or "-"
    url = run.get("html_url") or "(no url)"
    return f"{status}/{conclusion} {url}"


def dispatch_auto_tag(repo, branch):
    result = gh("workflow", "run", "auto-tag.yml", "--repo", repo, "--ref", branch)
    if result.returncode != 0:
        raise ReconcileError(
            "dispatching auto-tag.yml failed: "
            f"{result.stderr.strip() or result.stdout.strip()}"
        )


def reconcile(repo, branch, dry_run=False):
    version = read_version(repo, branch)
    tag = f"v{version}"
    print(f"{branch}: VERSION={version} (tag {tag})")

    if tag_exists(repo, tag):
        print(f"OK: tag {tag} exists; nothing to reconcile")
        return 0

    sha, message, created = bump_commit(repo, branch)
    short = sha[:7]
    if SKIP_MARKER in message:
        print(
            f"OK: the last VERSION commit {short} carries {SKIP_MARKER}; "
            f"tag {tag} is deliberately absent"
        )
        return 0

    runs = auto_tag_runs(repo, branch, parse_time(created))
    print(
        f"tag {tag} is missing; VERSION was last changed by {short} "
        f"({created}), auto-tag runs for that bump: {len(runs)}"
    )
    for run in runs:
        print(f"  - {describe(run)}")

    in_flight = [run for run in runs if run.get("status") in IN_FLIGHT]
    if in_flight:
        print(f"OK: auto-tag is already running for {tag}; not dispatching a duplicate")
        return 0

    succeeded = [run for run in runs if run.get("conclusion") == "success"]
    if succeeded:
        raise ReconcileError(
            f"auto-tag ran successfully for {tag} but the tag does not exist "
            f"({describe(succeeded[-1])}); a tag that exists on origin while "
            "nothing runs is the half-state #116 exists to prevent"
        )

    finished = [run for run in runs if run.get("conclusion")]
    if finished:
        raise ReconcileError(
            f"the auto-tag run for {tag} finished as "
            f"{finished[-1].get('conclusion')} without creating the tag "
            f"({describe(finished[-1])}); re-run it once the cause is fixed "
            "instead of dispatching it again from the schedule"
        )

    if dry_run:
        print(f"would dispatch auto-tag.yml on {branch} for {tag} (dry run)")
        return 0

    dispatch_auto_tag(repo, branch)
    print(
        f"dispatched auto-tag.yml on {branch} for {tag}: the tag push from the "
        "Actions app starts no workflow runs (#1047), so the dispatch is the heal"
    )
    return 0


def build_parser():
    parser = argparse.ArgumentParser(
        description="Reconcile VERSION on the release branch with its tag."
    )
    parser.add_argument(
        "--repo",
        default=os.environ.get("REPO") or os.environ.get("GITHUB_REPOSITORY"),
        help="OWNER/NAME (default $REPO or $GITHUB_REPOSITORY)",
    )
    parser.add_argument("--branch", default="main", help="release branch (default main)")
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="report the decision without dispatching auto-tag.yml",
    )
    return parser


def main(argv):
    parser = build_parser()
    args = parser.parse_args(argv[1:])
    if not args.repo:
        parser.error("--repo (or $REPO / $GITHUB_REPOSITORY) is required")
    try:
        return reconcile(args.repo, args.branch, dry_run=args.dry_run)
    except ReconcileError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        print(
            "The release tag is missing and this check will not fix it by "
            "guessing; see the run above for the evidence.",
            file=sys.stderr,
        )
        return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
