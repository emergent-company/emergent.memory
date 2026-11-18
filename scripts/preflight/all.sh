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
for script in base-check.sh base-check.test.sh version-drift.test.sh migration-number.sh go-decl-collision.sh gofmt-tracked.sh cli-skill-pin.sh docs-stale-paths.sh docs-port.sh skip-census.sh mcp-authz.sh modelref-split.sh runrole-guard.sh schemadrift-guard.sh gitleaks-waivers.sh gitleaks-env-files.sh connector-lint-self-test.sh ui-guard.sh brand.sh gh-pages-budget.sh; do
  echo "==> $script"
  if bash "$dir/$script"; then
    echo "==> $script PASSED"
  else
    echo "==> $script FAILED"
    status=1
  fi
done

# The release tag reconcile/dispatch decision tables (#1047): a release PR
# merged by `app/github-actions` raises its push from GITHUB_TOKEN, for which
# GitHub starts no workflow runs, so auto-tag's push trigger never fires and the
# tag (and the whole fan-out) never happens. Both suites drive the scripts the
# review gate and the scheduled watchdog call, with a fake `gh` on PATH: offline,
# no credentials. They run here so the required `ci` job exercises them without
# a workflow-file change.
for suite in reconcile-release-tag.test.sh dispatch-release-tag.test.sh; do
  echo "==> .github/scripts/$suite"
  if bash "$root/.github/scripts/$suite"; then
    echo "==> .github/scripts/$suite PASSED"
  else
    echo "==> .github/scripts/$suite FAILED"
    status=1
  fi
done

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

echo "preflight: OK (base, base-check tests, version-drift tests, migration numbering, cross-branch Go declaration collisions, gofmt, CLI skill pin, stale docs, Astro docs port, skip census, mcp authz, modelref split, runrole guard, schemadrift, gitleaks waivers, gitleaks env files, connector lint self-test, ui guardrails, brand tokens, gh-pages budget self-tests, connector install channel, release tag reconcile/dispatch)"
