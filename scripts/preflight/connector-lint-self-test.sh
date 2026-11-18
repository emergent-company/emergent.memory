#!/usr/bin/env bash
# Hermetic self-test for the Mac connector lint ratchet.
#
# apps/connector.mac/Scripts/lint-swift.sh must not report a clean run when
# swiftformat/swiftlint are absent (#925). That fail-open is how the lefthook
# `connector-mac-swift` hook went green without linting anything, letting a
# count-sensitive .swiftlint.baseline regression reach dev and first fail on the
# dev -> main promotion. The test drives the script under a stripped PATH with
# stub linters, so it needs no Swift toolchain and belongs in the required `ci`
# job — connector.yml is path-filtered and has no `pull_request` trigger.
#
# Wired through scripts/preflight/all.sh, which the required `ci` job runs.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

exec bash apps/connector.mac/Scripts/lint-swift.test.sh
