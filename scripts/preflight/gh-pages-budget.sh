#!/usr/bin/env bash
# Self-tests for the gh-pages size-budget scripts.
#
# gh-pages carries two publishers (the docs site and the Mac Sparkle appcast)
# against one aggregate guard. check-pages-tree-size.sh refuses to publish a
# tree over that guard, and publish-appcast.sh runs it during a release -- so a
# silent break in the retention or the guard turns into a red `Mac Release` on
# every tag, with the fix hidden in a shell script nobody exercises (#976,
# recurrence of #7). All three scripts are hermetic bash (temp dirs, no network,
# no git remote), so their self-tests run here, in the required `ci` context.
#
# Why not a step of their own in ci.yml next to the other shell self-tests: this
# file is the one place the required job already runs a list of checks, and the
# gh-pages scripts are a single concern. Move them into ci.yml if they grow.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

bash "$root/.github/scripts/prune-doc-versions.test.sh"
bash "$root/.github/scripts/prune-appcast-orphans.test.sh"
bash "$root/.github/scripts/check-pages-tree-size.test.sh"
