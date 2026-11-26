#!/usr/bin/env python3
"""Reconcile ``VERSION`` on the release branch with the release tag (#1047).

``auto-tag.yml`` creates the release tag and starts the whole release fan-out.
Its normal trigger is ``push`` to ``main`` touching ``VERSION`` -- but a release
PR merged by ``app/github-actions`` (the reviewed path: ``github-review-gate.yml``
arms auto-merge and GitHub merges it as the Actions app) raises a push event
from ``GITHUB_TOKEN``, and GitHub starts **no workflow runs** for those. On
v0.104.0 the tag was simply missing afterwards and had to be pushed by hand.

``workflow_dispatch`` and ``schedule`` are documented exceptions to that
suppression, so the observer has to be one of them. This script is the decision
table behind both:

  * tag ``vX`` exists                      -> consistent, nothing to do;
  * the commit that last touched ``VERSION`` carries ``[skip release]``
    (or the branch tip does)               -> deliberately untagged, nothing to do;
  * an ``auto-tag`` run for that bump is queued/running
                                           -> wait for it, do not stack a second;
  * an ``auto-tag`` run for that bump already finished and the tag is still
    missing                                -> FAIL LOUDLY: re-dispatching every
                                              tick would hide a real failure;
  * no run has been made for that bump     -> dispatch ``auto-tag.yml`` on the
                                              release branch (it is idempotent).

The last two are the point of the script: a watchdog that only knows how to
retry turns one broken release into an endless dispatch loop and a green
schedule, which is the same "observed by nobody" shape this issue is about.

Everything is read through ``gh``, so the caller only needs a token with
``contents: read`` (tag ref, commits, ``VERSION``) plus ``actions: write`` for
the dispatch. Stdlib only, like ``check-release-fanout.py``.

Usage:
  reconcile-release-tag.py [--repo OWNER/NAME] [--branch main]
                           [--workflow auto-tag.yml] [--dry-run]
"""

import argparse
import base64
import json
import os
import subprocess
import sys

SKIP_MARKER = "[skip release]"
ACTIVE_STATES = ("queued", "in_progress", "waiting", "pending", "requested")

EXIT_OK = 0        # consistent, deliberately skipped, waited, or dispatched
EXIT_FAILED = 1    # the tag should exist and does not: a human/rerun is needed
EXIT_USAGE = 2     # the API could not be read; refuse to guess


class ApiError(Exception):
    """``gh`` failed in a way the decision table cannot classify."""


def gh(args):
    """Run ``gh`` and return (returncode, stdout, stderr)."""
    proc = subprocess.run(
        ["gh", *args],
        capture_output=True,
        text=True,
        env=os.environ,
    )
    if proc.returncode != 0:
        raise ApiError(
            "gh %s failed (exit %d): %s"
            % (" ".join(args), proc.returncode, (proc.stderr or proc.stdout).strip())
        )
    return proc.returncode, proc.stdout, proc.stderr


def gh_json(args):
    _, out, _ = gh(args)
    try:
        return json.loads(out)
    except ValueError as exc:
        raise ApiError("gh %s returned non-JSON: %s" % (" ".join(args), exc))


def is_not_found(stderr, stdout):
    text = (stderr or "") + (stdout or "")
    return "HTTP 404" in text or "Not Found" in text or "not found" in text


def read_version(repo, branch):
    """Return the trimmed ``VERSION`` at ``branch``."""
    data = gh_json(["api", "repos/%s/contents/VERSION?ref=%s" % (repo, branch)])
    raw = (data.get("content") or "").replace("\n", "")
    try:
        text = base64.b64decode(raw).decode("utf-8")
    except (ValueError, UnicodeDecodeError) as exc:
        raise ApiError("VERSION at %s is not decodable base64: %s" % (branch, exc))
    version = text.strip()
    if not version:
        raise ApiError("%s:VERSION is empty" % branch)
    return version


def tag_exists(repo, tag):
    """True/False, or raise when the answer cannot be trusted.

    A 404 is the only answer that means "no tag": a permission error or an
    outage must fail the run, never be read as "missing" and trigger a dispatch
    against a tag that may well exist.
    """
    args = ["api", "repos/%s/git/ref/tags/%s" % (repo, tag)]
    proc = subprocess.run(["gh", *args], capture_output=True, text=True, env=os.environ)
    if proc.returncode == 0:
        return True
    if is_not_found(proc.stderr, proc.stdout):
        return False
    raise ApiError("could not read tag %s: %s"
                   % (tag, (proc.stderr or proc.stdout).strip()))


def commit(repo, ref, path=None):
    """Return ``(sha, message)`` for ``ref`` (a branch name or a commit sha)."""
    if path:
        rows = gh_json(["api", "repos/%s/commits?path=%s&sha=%s&per_page=1"
                        % (repo, path, ref)])
        if not isinstance(rows, list) or not rows:
            raise ApiError("no commit touched %s on %s" % (path, ref))
        row = rows[0]
    else:
        row = gh_json(["api", "repos/%s/commits/%s" % (repo, ref)])
    return row["sha"], (row.get("commit") or {}).get("message") or ""


def auto_tag_runs(repo, workflow, branch):
    """Runs of ``workflow`` for ``branch``, newest first."""
    data = gh_json(["api", "repos/%s/actions/workflows/%s/runs?branch=%s&per_page=100"
                    % (repo, workflow, branch)])
    return data.get("workflow_runs") or []


def dispatch(repo, workflow, branch):
    gh(["workflow", "run", workflow, "--repo", repo, "--ref", branch])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--repo",
                        default=os.environ.get("REPO") or os.environ.get("GITHUB_REPOSITORY"),
                        help="OWNER/NAME (default: $REPO or $GITHUB_REPOSITORY)")
    parser.add_argument("--branch", default="main",
                        help="release branch (default: main)")
    parser.add_argument("--workflow", default="auto-tag.yml",
                        help="tagging workflow (default: auto-tag.yml)")
    parser.add_argument("--dry-run", action="store_true",
                        help="report the decision without dispatching")
    opts = parser.parse_args(argv)

    if not opts.repo or "/" not in opts.repo:
        print("ERROR: --repo OWNER/NAME (or $REPO) is required", file=sys.stderr)
        return EXIT_USAGE

    try:
        version = read_version(opts.repo, opts.branch)
        tag = "v" + version

        if tag_exists(opts.repo, tag):
            print("OK: %s is %s on %s and tag %s exists -- nothing to do."
                  % (opts.branch, version, opts.repo, tag))
            return EXIT_OK

        tip_sha, tip_message = commit(opts.repo, opts.branch)
        bump_sha, bump_message = commit(opts.repo, opts.branch, path="VERSION")

        # The marker is what makes a bump untagged on purpose (snapshot push,
        # re-landed bump after a failed release). Both commits matter: the tip
        # is what the tag would point at, the VERSION commit is the bump.
        if SKIP_MARKER in bump_message or SKIP_MARKER in tip_message:
            print("OK: %s is %s and tag %s is missing, but %s carries %r -- "
                  "deliberately untagged."
                  % (opts.branch, version, tag, bump_sha[:9], SKIP_MARKER))
            return EXIT_OK

        # Only runs for this bump say anything about this bump: an older run
        # (previous version) finishing without a tag is not evidence either way.
        runs = [r for r in auto_tag_runs(opts.repo, opts.workflow, opts.branch)
                if r.get("head_sha") in (bump_sha, tip_sha)]
        active = [r for r in runs if r.get("status") in ACTIVE_STATES]
        if active:
            run = active[0]
            print("OK: %s is %s and tag %s is missing, but %s run %s for %s is %s "
                  "-- waiting for it."
                  % (opts.branch, version, tag, opts.workflow, run.get("id"),
                     bump_sha[:9], run.get("status")))
            return EXIT_OK

        finished = [r for r in runs if r.get("status") == "completed"]
        if finished:
            run = finished[0]
            print("ERROR: %s is %s and tag %s is missing, but %s run %s for %s "
                  "already finished (%s)."
                  % (opts.branch, version, tag, opts.workflow, run.get("id"),
                     bump_sha[:9], run.get("conclusion")), file=sys.stderr)
            print("Re-dispatching would loop on the same failure. Re-run that run, "
                  "or start it again once the cause is fixed:\n"
                  "  gh run rerun %s --repo %s\n"
                  "  gh workflow run %s --repo %s --ref %s"
                  % (run.get("id"), opts.repo, opts.workflow, opts.repo, opts.branch),
                  file=sys.stderr)
            print("If the tag is genuinely unwanted, land a bump whose commit "
                  "message carries %r." % SKIP_MARKER, file=sys.stderr)
            return EXIT_FAILED

        if opts.dry_run:
            print("WOULD DISPATCH: %s is %s and tag %s is missing with no run for "
                  "%s." % (opts.branch, version, tag, bump_sha[:9]))
            return EXIT_OK

        print("DISPATCH: %s is %s and tag %s is missing with no %s run for %s."
              % (opts.branch, version, tag, opts.workflow, bump_sha[:9]))
        try:
            dispatch(opts.repo, opts.workflow, opts.branch)
        except ApiError as exc:
            # 422 here means the workflow declares no `workflow_dispatch`
            # trigger -- the #1047 bug itself, before the workflow half of the
            # fix is present. Name the recovery instead of just failing.
            print("ERROR: could not dispatch %s: %s" % (opts.workflow, exc),
                  file=sys.stderr)
            print("If the workflow has no 'workflow_dispatch' trigger the tag "
                  "cannot be created through the API (#1047); create it and push "
                  "the tag by hand so the fan-out starts:\n"
                  "  git tag -a %s %s && git push origin refs/tags/%s"
                  % (tag, bump_sha, tag), file=sys.stderr)
            return EXIT_FAILED
        print("Dispatched %s on %s for %s." % (opts.workflow, opts.branch, tag))
        return EXIT_OK
    except ApiError as exc:
        print("ERROR: %s" % exc, file=sys.stderr)
        print("Refusing to dispatch: the state of %s could not be read." % opts.repo,
              file=sys.stderr)
        return EXIT_USAGE


if __name__ == "__main__":
    sys.exit(main())
