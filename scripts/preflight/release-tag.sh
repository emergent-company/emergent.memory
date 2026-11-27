#!/usr/bin/env bash
# Guard: a release branch's VERSION must have its release tag (#1047).
#
# auto-tag.yml creates the release tag from a `push` to main touching VERSION,
# and that tag is what starts the whole release fan-out. But the normal path is
# a release PR merged by `app/github-actions` (github-review-gate.yml arms
# auto-merge, and GitHub merges it as the Actions app). The push event that
# merge raises comes from GITHUB_TOKEN, and GitHub starts **no workflow runs**
# for those -- so auto-tag never ran, v0.104.0's tag was never created, and the
# nine-workflow fan-out never started. The tag had to be pushed by hand.
#
# Nothing observed that state: auto-tag has no `workflow_dispatch` trigger (its
# only entry point is the push), check-release-fanout.py checks the manifest
# against the workflows rather than that a merged bump produced a tag, and the
# tag-triggered workflows cannot miss a tag they never hear about.
#
# This is the observer that needs no workflow file. It runs in
# scripts/preflight/all.sh, i.e. in the branch-protection-required `ci` job, on
# every PR: it reads VERSION from the release branch and fails when the matching
# tag is absent. Drift is then a red required check on the next PR -- minutes,
# not "whoever notices that a release never shipped".
#
# It talks to the remote through git only (`fetch` + `ls-remote`), so it needs
# no `gh`, no token beyond the read access a checkout already has, and no API
# quota. It is deliberately not a second copy of the reconcile decision table
# (.github/scripts/reconcile-release-tag.py, which owns the heal): this guard
# only answers "does the tag exist", and reports.
#
# Three states are not drift and pass:
#   * the bump carries `[skip release]` -- deliberately untagged (snapshot push,
#     re-landed bump after a failed release), the same marker auto-tag honours;
#   * the bump is younger than RELEASE_TAG_GRACE_MINUTES (default 15) -- a
#     release is most likely in flight, so do not red every lane's PR for it;
#   * the remote cannot be read -- a guard that fails closed on a network blip
#     is a guard people turn off; it warns instead.
#
# Env: RELEASE_BRANCH (default main), RELEASE_REMOTE (default origin),
#      RELEASE_TAG_GRACE_MINUTES (default 15).
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

branch="${RELEASE_BRANCH:-main}"
remote="${RELEASE_REMOTE:-origin}"
grace_minutes="${RELEASE_TAG_GRACE_MINUTES:-15}"
marker='[skip release]'
tracking="refs/remotes/${remote}/${branch}"

# Fetch with an explicit refspec: a bare-name `git fetch origin main` also
# resolves a TAG named `main`, and would leave ${tracking} stale (#956).
fetch_args=(--quiet --no-tags)
if [ "$(git rev-parse --is-shallow-repository 2>/dev/null || echo false)" = "true" ]; then
  # A shallow checkout of a PR has only the branch tip; deepen so the VERSION
  # commit and its message are reachable, not just the last commit.
  fetch_args+=(--deepen=50)
fi
if ! git fetch "${fetch_args[@]}" "$remote" "+refs/heads/${branch}:${tracking}" 2>/dev/null; then
  echo "release tag guard: WARNING could not fetch ${remote}/${branch}; cannot judge the release tag, skipping." >&2
  exit 0
fi

if ! version="$(git show "${tracking}:VERSION" 2>/dev/null | tr -d '[:space:]')" || [ -z "$version" ]; then
  echo "release tag guard: cannot read VERSION at ${remote}/${branch}" >&2
  exit 2
fi

tag="v${version}"

# `--refs` drops the peeled `^{}` line of an annotated tag; the tag itself is
# what matters. A network failure here is a warning, not drift.
if ! listed="$(git ls-remote --refs --tags "$remote" "refs/tags/${tag}" 2>&1)"; then
  echo "release tag guard: WARNING could not list tags on ${remote} (${listed}); skipping." >&2
  exit 0
fi

if [ -n "$listed" ]; then
  echo "release tag aligned: ${remote}/${branch} is ${version} and ${tag} exists"
  exit 0
fi

tip_message="$(git log -1 --format=%B "$tracking" 2>/dev/null || true)"
bump_message="$(git log -1 --format=%B "$tracking" -- VERSION 2>/dev/null || true)"
if printf '%s\n%s\n' "$tip_message" "$bump_message" | grep -Fq "$marker"; then
  echo "release tag: ${remote}/${branch} is ${version} and ${tag} is missing, but the bump carries '${marker}' -- deliberately untagged"
  exit 0
fi

bump_time="$(git log -1 --format=%ct "$tracking" -- VERSION 2>/dev/null || true)"
now="$(date -u +%s)"
if [ -n "$bump_time" ] && [ "$grace_minutes" -gt 0 ] 2>/dev/null; then
  age=$((now - bump_time))
  if [ "$age" -ge 0 ] && [ "$age" -lt $((grace_minutes * 60)) ]; then
    echo "release tag: WARNING ${remote}/${branch} is ${version} and ${tag} is missing, but the bump is ${age}s old (grace ${grace_minutes}m) -- a release may be in flight" >&2
    exit 0
  fi
fi

tip_sha="$(git rev-parse "$tracking")"
echo "release tag: ${remote}/${branch} is ${version}, but the release tag ${tag} does not exist." >&2
echo "  auto-tag.yml creates it from a push to ${branch} that touches VERSION. When the" >&2
echo "  release PR is merged by app/github-actions (auto-merge armed by" >&2
echo "  github-review-gate.yml), that push is raised by GITHUB_TOKEN, GitHub starts no" >&2
echo "  workflow run for it, and the tag -- with the whole release fan-out -- never" >&2
echo "  happens (#1047; v0.104.0's tag had to be pushed by hand)." >&2
echo "  Start the tag path explicitly:" >&2
echo "    gh workflow run auto-tag.yml --repo <owner>/<repo> --ref ${branch}" >&2
echo "  or, where that trigger does not exist yet, push the annotated tag yourself:" >&2
echo "    git tag -a ${tag} ${tip_sha} -m '${tag}' && git push ${remote} refs/tags/${tag}" >&2
echo "  A bump that must not be tagged carries '${marker}' in its commit message." >&2
exit 1
