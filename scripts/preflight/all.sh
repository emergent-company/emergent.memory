#!/usr/bin/env bash
# Run every preflight check; exit non-zero if any fails.
set -euo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(git rev-parse --show-toplevel)"
cd "$root"

status=0
# base-check.test.sh is the guard's own regression test (#956): a tag named
# like the branch makes a bare-name `git fetch origin <branch>` resolve the tag
# and exit 0, so base-check used to report a stale base as fresh. It is hermetic
# (a temporary bare origin, no network) and runs here so the required `ci` job
# exercises it without a workflow-file change.
# version-drift.test.sh covers version-drift.sh's release-attribution rule
# (#1026): the rule only runs in CI, so without this suite a subject the guard
# rejects -- what `task release` used to emit -- is discovered after the push.
# It also pins the release task's subject to one the guard accepts.
# design-system.test.sh covers design-system.sh (#1071): its fail-closed
# branch (tokens present, generator missing -- the state the brand.sh ->
# design-system.sh rename produced during #1069), its "nothing to check" pass,
# genuine drift, the clearSpace rule the token file publishes, and a page that
# points at an asset which moved. It needs no Go toolchain: every case exits
# before the wrapper's `go vet`.
# fetch-hygiene.sh is the static half of #1158 (follow-up to #956): it rejects
# `git fetch <remote> <bare-ref>`, where git resolves the name against
# refs/tags/<name> before refs/heads/<name> and this repo publishes
# refs/tags/dev, so the fetch reads the tag and leaves origin/dev stale. Its
# baseline lists the four workflow sites of #1158 that still need
# `workflows: write` to change; when one is fixed the gate fails and names the
# stale entry, so the list can only shrink. It lands here, and not in a
# workflow step, because the required `ci` job runs this file -- the same reason
# base-check.test.sh and the release-tag suites below do.
# release-tag.sh is the observer for #1047: a release PR merged by
# `app/github-actions` raises its push from GITHUB_TOKEN, GitHub starts no
# workflow runs for it, and auto-tag's push trigger never fires -- so the tag,
# and the whole release fan-out, never happen (v0.104.0 had to be tagged by
# hand). It reads VERSION from the release branch and fails when the matching
# tag is absent, which turns that silent miss into a red required check on the
# next PR. It needs no workflow file; release-tag.test.sh is its hermetic suite
# (a temporary bare origin, no network).
for script in base-check.sh base-check.test.sh fetch-hygiene.sh fetch-hygiene.test.sh version-drift.test.sh release-tag.sh release-tag.test.sh migration-number.sh go-decl-collision.sh gofmt-tracked.sh cli-skill-pin.sh docs-stale-paths.sh skip-census.sh mcp-authz.sh modelref-split.sh runrole-guard.sh schemadrift-guard.sh gitleaks-waivers.sh gitleaks-env-files.sh connector-lint-self-test.sh ui-guard.sh design-system.test.sh design-system.sh gh-pages-budget.sh; do
  echo "==> $script"
  if bash "$dir/$script"; then
    echo "==> $script PASSED"
  else
    echo "==> $script FAILED"
    status=1
  fi
done

# The server architectural-debt ratchet (#1120). server.yml owns the script,
# but that workflow is path-filtered and does not run on pushes to `dev`, so the
# baselines drifted there (cross-domain setters 3 -> 7, apperror Style A
# 1225 -> 1241) unseen until the next promotion -- while lefthook's pre-commit
# run of the same script failed every server-side commit in the meantime. It is
# a grep/awk pass with no build, no Postgres and no node, so it runs here with
# the other cheap guards and lands in the required `ci` job without a
# workflow-file change (the same route base-check.test.sh and the release-tag
# suites below take). `--self-test` proves the counters still inspect the tree
# before the gate trusts their zeros; the golangci-lint `--lint-counts` half
# stays in server.yml, which has that toolchain installed.
echo "==> apps/server/scripts/lint-ratchet.sh"
if bash "$root/apps/server/scripts/lint-ratchet.sh" --self-test \
   && bash "$root/apps/server/scripts/lint-ratchet.sh"; then
  echo "==> apps/server/scripts/lint-ratchet.sh PASSED"
else
  echo "==> apps/server/scripts/lint-ratchet.sh FAILED"
  status=1
fi

# The release tag reconcile/dispatch decision tables (#1047): a release PR
# merged by `app/github-actions` raises its push from GITHUB_TOKEN, for which
# GitHub starts no workflow runs, so auto-tag's push trigger never fires and the
# tag (and the whole fan-out) never happens. Both suites drive the scripts the
# review gate and the scheduled watchdog call, with a fake `gh` on PATH: offline,
# no credentials. They run here so the required `ci` job exercises them without
# a workflow-file change.
#
# `review-gate.test.sh` is the third: it decides which comment arms, disarms or
# is ignored by `github-review-gate.yml` (#1385). That logic used to live in a
# `run:` block no test could reach, and the author filter in it was inverted for
# rejections -- product #1036 merged 2m12s after a trusted `CHANGES_REQUESTED`.
# `check-secret-environments.test.sh` is the last suite: the guard and its
# shrinking baseline land here, not in a workflow step, because the required
# `ci` job runs this file and the lane token cannot push .github/workflows/**
# (no `workflows` permission). It exercises the scanner, the baseline's stale
# and wrong-environment rules, and the guard's wiring below (#1232).
for suite in reconcile-release-tag.test.sh dispatch-release-tag.test.sh review-gate.test.sh check-secret-environments.test.sh; do
  echo "==> .github/scripts/$suite"
  if bash "$root/.github/scripts/$suite"; then
    echo "==> .github/scripts/$suite PASSED"
  else
    echo "==> .github/scripts/$suite FAILED"
    status=1
  fi
done

# Signing and App credentials live in the `signing` / `dev-dispatch`
# environments instead of repo-level secrets (#1232): a repository secret is
# readable by any workflow run a pushed branch can add (`on: pull_request` +
# `run: … ${{ secrets.X }}`) with no approval, while an environment secret
# requires the job to declare the environment and the environment's deployment
# branch policy refuses agent-pushed refs. This guard keeps the tree half -- a
# job that reads one of those secrets must declare the environment that owns it
# -- and its baseline (check-secret-environments.baseline) is the shrinking list
# of the four jobs whose `environment:` line still needs `workflows: write` to
# land. It runs here because this file is what the required `ci` job executes,
# so the guard needs no workflow-file change (the route fetch-hygiene.sh takes
# for the same permission gap). A finding fails; a stale baseline entry fails
# too, so the list can only shrink.
echo "==> .github/scripts/check-secret-environments.py"
if python3 "$root/.github/scripts/check-secret-environments.py" --self-test \
   && python3 "$root/.github/scripts/check-secret-environments.py"; then
  echo "==> .github/scripts/check-secret-environments.py PASSED"
else
  echo "==> .github/scripts/check-secret-environments.py FAILED"
  status=1
fi

# The connector dev channel must not resolve a tag named like a branch (#956).
# install.test.sh stubs curl/tar, so it runs offline.
echo "==> apps/connector.linux/install.test.sh"
if bash "$root/apps/connector.linux/install.test.sh"; then
  echo "==> apps/connector.linux/install.test.sh PASSED"
else
  echo "==> apps/connector.linux/install.test.sh FAILED"
  status=1
fi

if [ "$status" -ne 0 ]; then
  echo "preflight: FAILED"
  exit 1
fi

echo "preflight: OK (base, base-check tests, version-drift tests, migration numbering, cross-branch Go declaration collisions, gofmt, CLI skill pin, stale docs, Astro docs port, skip census, mcp authz, modelref split, runrole guard, schemadrift, gitleaks waivers, gitleaks env files, connector lint self-test, ui guardrails, brand gate tests, brand tokens, gh-pages budget self-tests, server architectural-debt ratchet, connector install channel, release tag reconcile/dispatch, release tag drift, fetch hygiene, secret environments)"
