#!/usr/bin/env bash
# The generated CLI skill reference must stay pinned to the CLI help text.
#
# apps/cli/internal/skillsfs/skills/memory-cli-reference/SKILL.md is produced by
# `go generate ./cmd/...` (see apps/cli/cmd/main.go) and committed, but nothing
# re-ran the generator. Help-text edits therefore silently desynced the skill
# that agents read (issue #690, item 5). This gate regenerates and fails when
# the committed file drifts from the source of truth.
#
# Run by scripts/preflight/all.sh, which the `ci` GitHub workflow invokes; also
# wired into lefthook's pre-commit for CLI-scoped changes. Run from anywhere;
# resolves the repo root from the script's own location — not from
# `git rev-parse --show-toplevel`, which answers the hook's cwd when a git hook
# exports GIT_DIR. lefthook runs this job with cwd `apps/cli`, so the old form
# diffed `apps/cli/apps/cli/internal/skillsfs`, matched nothing and reported
# every generated file as deleted. ui-guard.sh documents the same trap.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

skill_dir="apps/cli/internal/skillsfs"

if ! command -v go >/dev/null 2>&1; then
  echo "cli-skill-pin: go not found on PATH (required to run the CLI generator)" >&2
  exit 1
fi

drift() {
  echo "cli-skill-pin: generated CLI skill reference is out of date" >&2
  echo "  Regenerate with: (cd apps/cli && go generate ./cmd/...) and commit the diff under $skill_dir/" >&2
  git diff --stat -- "$skill_dir" >&2 || true
}

# Check before regenerating: an unstaged hand-edit would otherwise be silently
# overwritten by the generator and look clean.
if ! git diff --exit-code -- "$skill_dir" >/dev/null 2>&1; then
  drift
  exit 1
fi

(cd apps/cli && go generate ./cmd/...)

# Check against the index (not --cached): a stale file already staged shows up
# as a worktree-vs-index diff only after regeneration.
if ! git diff --exit-code -- "$skill_dir" >/dev/null 2>&1; then
  drift
  exit 1
fi

echo "cli-skill-pin: OK (generated CLI skill is in sync)"
