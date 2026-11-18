#!/usr/bin/env bash
# Hermetic test for version-drift.sh's release-attribution rule (#1026).
#
# WHY. A VERSION bump is only allowed when the change is attributable to a
# release: the PR carries the `release` label, a commit subject contains
# `[skip release]`, or a commit subject matches `chore: release vX.Y.Z`. The
# rule lives in the CI-only half of the guard (it needs GITHUB_BASE_REF), so a
# subject the rule does not recognise used to be discovered only after the push
# -- and `task release` emitted exactly such a subject (`chore: bump version to
# X.Y.Z`, issue #1026). This suite exercises the rule offline against a
# throwaway git repository, and pins the subject the release task emits to one
# the guard accepts, so the two cannot drift apart again.
#
# Each case: a temporary repo whose `main` holds the current version and whose
# `refs/remotes/origin/main` names that base (CI has the base commit; the guard
# three-dot diffs against it), then a `feature` branch with a bump and a
# subject under test. No network, no remote, no toolchain beyond git and awk.
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)
script="$here/version-drift.sh"
all_sh="$here/all.sh"
taskfile="$root/Taskfile.yml"

for cmd in git awk grep sed; do
  command -v "$cmd" >/dev/null 2>&1 || {
    printf 'FAIL [version-drift]: %s is required\n' "$cmd" >&2
    exit 2
  }
done

failures=0
checks=0
ok() {
  checks=$((checks + 1))
  printf 'ok   [version-drift]: %s\n' "$1"
}
no() {
  checks=$((checks + 1))
  failures=$((failures + 1))
  printf 'FAIL [version-drift]: %s\n' "$1" >&2
  if [ -n "${out:-}" ]; then
    printf '%s\n' "$out" | sed 's/^/    /' >&2
  fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
work="$tmp/repo"

version_old=0.102.3
version_new=0.102.4

git_() { git -C "$work" "$@"; }

# fresh: an empty repository whose `main` has the version carriers aligned at
# version_old, with origin/main naming that base commit.
fresh() {
  rm -rf "$work"
  mkdir -p "$work/apps/server/cmd/server" "$work/apps/ios/VoiceAgent.xcodeproj"
  git_ init -q -b main
  git_ config user.email test@example.com
  git_ config user.name test
  git_ config commit.gpgsign false
  put_version "$version_old"
  git_ add -A
  git_ commit -qm "chore: seed version"
  git_ update-ref refs/remotes/origin/main HEAD
  git_ checkout -q -b feature
}

put_version() {
  printf '%s\n' "$1" >"$work/VERSION"
  printf '// @version %s\n' "$1" >"$work/apps/server/cmd/server/main.go"
  printf '\t\t\t\tMARKETING_VERSION = %s;\n' "$1" \
    >"$work/apps/ios/VoiceAgent.xcodeproj/project.pbxproj"
}

commit() {
  git_ add -A
  git_ commit -qm "$1"
}

out=""
status=0
run_guard() {
  status=0
  # PR_LABELS is set even when empty to shadow any value in the caller's
  # environment; GITHUB_EVENT_BEFORE is scrubbed so the base comes from
  # GITHUB_BASE_REF alone, exactly as it does for a pull_request run.
  out=$(cd "$work" && env -u GITHUB_EVENT_BEFORE -u PREFLIGHT_BASE \
    GITHUB_BASE_REF=main PR_LABELS="${1:-}" bash "$script" 2>&1) || status=$?
}

expect_status() {
  if ((status == $1)); then
    ok "$2"
  else
    no "$2 (exit ${status}, expected $1)"
  fi
}

expect_contains() {
  if grep -Fq -- "$2" <<<"$out"; then
    ok "$1"
  else
    no "$1 (output does not contain: $2)"
  fi
}

expect_not_contains() {
  if grep -Fq -- "$2" <<<"$out"; then
    no "$1 (output unexpectedly contains: $2)"
  else
    ok "$1"
  fi
}

# ---------------------------------------------------------------------------
# 1. The subject the release task emits is accepted.
# ---------------------------------------------------------------------------
fresh
put_version "$version_new"
commit "chore: release v$version_new"
run_guard
expect_status 0 "a 'chore: release vX.Y.Z' subject is attributed to a release"
expect_contains "the accepted subject is named" "release commit subject"
expect_contains "the aligned carriers still pass" "version aligned"

# ---------------------------------------------------------------------------
# 2. The subject this issue is about -- what `task release` used to emit -- is
#    still rejected. If it ever becomes acceptable, this case says so loudly
#    rather than silently losing the regression.
# ---------------------------------------------------------------------------
fresh
put_version "$version_new"
commit "chore: bump version to $version_new"
run_guard
expect_status 1 "a 'chore: bump version to X.Y.Z' subject is not a release"
expect_contains "the rejection explains the rule" "version attribution"
expect_contains "the rejection names the expected subject" "chore: release vX.Y.Z"
expect_not_contains "the rejected bump does not report success" "version aligned"

# ---------------------------------------------------------------------------
# 3. `[skip release]` attributes the bump (and suppresses auto-tagging).
# ---------------------------------------------------------------------------
fresh
put_version "$version_new"
commit "chore: bump version to $version_new [skip release]"
run_guard
expect_status 0 "'[skip release]' attributes a bump"
expect_contains "the escape is named" "'[skip release]' marker"

# ---------------------------------------------------------------------------
# 4. The PR's `release` label attributes the bump -- even a rewritten subject.
#    An unrelated label must not.
# ---------------------------------------------------------------------------
fresh
put_version "$version_new"
commit "chore: bump version to $version_new"
run_guard "release"
expect_status 0 "the PR 'release' label attributes a bump"
expect_contains "the label escape is named" "PR 'release' label"

fresh
put_version "$version_new"
commit "chore: bump version to $version_new"
run_guard "bug,release-notes"
expect_status 1 "an unrelated label is not the release label"

# ---------------------------------------------------------------------------
# 5. No VERSION change: attribution never runs, whatever the subject says.
# ---------------------------------------------------------------------------
fresh
printf 'notes\n' >"$work/README.md"
commit "docs: add notes"
run_guard
expect_status 0 "a branch that leaves VERSION alone is fine"
expect_not_contains "attribution does not run without a VERSION change" "version attribution"

# ---------------------------------------------------------------------------
# 6. The release task in Taskfile.yml emits a subject case 1 accepts. This is
#    the actual regression (#1026): the two halves must agree on one subject.
# ---------------------------------------------------------------------------
release_block=$(awk '
  /^  release:[[:space:]]*$/ { in_release = 1; next }
  in_release && /^  [^[:space:]]+:/ { exit }
  in_release
' "$taskfile")

commit_subject=$(printf '%s\n' "$release_block" |
  sed -nE 's/^[[:space:]]*git commit -m "(.*)"[[:space:]]*$/\1/p' | head -1)

if [ -n "$commit_subject" ]; then
  ok "the release task has a commit subject"
else
  no "the release task has a commit subject (none found in Taskfile.yml)"
fi

if [[ $commit_subject == *'{{.VERSION}}'* ]]; then
  ok "the release subject interpolates {{.VERSION}}"
else
  no "the release subject interpolates {{.VERSION}} (got: $commit_subject)"
fi

release_subject=${commit_subject//'{{.VERSION}}'/$version_new}
if [ -n "$release_subject" ] && [ "$release_subject" != "$commit_subject" ]; then
  fresh
  put_version "$version_new"
  commit "$release_subject"
  run_guard
  expect_status 0 "the release task's subject ('$release_subject') is accepted"
  expect_contains "the release task's subject is attributed" "release commit subject"
else
  no "the release task's subject resolves to a concrete version (got: $release_subject)"
fi

# ---------------------------------------------------------------------------
# 7. The release commit stages the version carriers and nothing else: the CLI
#    reference SKILL.md has no version (frontmatter `version: "1.0"`), so
#    staging it folds an unrelated generated diff into the release commit.
# ---------------------------------------------------------------------------
add_line=$(printf '%s\n' "$release_block" | grep -E '^[[:space:]]*git add ' | head -1)
if [ -n "$add_line" ]; then
  ok "the release task stages files explicitly"
else
  no "the release task stages files explicitly (no 'git add' line found)"
fi

for carrier in VERSION apps/server/cmd/server/main.go apps/ios/VoiceAgent.xcodeproj/project.pbxproj; do
  if [[ $add_line == *"$carrier"* ]]; then
    ok "the release commit stages $carrier"
  else
    no "the release commit stages $carrier"
  fi
done

if [[ $add_line == *memory-cli-reference* ]] || [[ $add_line == *SKILL.md* ]]; then
  no "the release commit does not stage the CLI reference SKILL.md"
else
  ok "the release commit does not stage the CLI reference SKILL.md"
fi

# ---------------------------------------------------------------------------
# 8. Wiring: the suite must run where the required `ci` job can see it, and
#    both scripts must parse.
# ---------------------------------------------------------------------------
if grep -Fq "version-drift.test.sh" "$all_sh"; then
  ok "all.sh runs this suite"
else
  no "all.sh runs this suite"
fi
if bash -n "$script"; then
  ok "version-drift.sh parses"
else
  no "version-drift.sh parses"
fi
if bash -n "${BASH_SOURCE[0]}"; then
  ok "version-drift.test.sh parses"
else
  no "version-drift.test.sh parses"
fi

if ((checks > 0)); then
  printf '\n'
fi
if ((failures > 0)); then
  printf 'FAIL [version-drift]: %d of %d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'ok [version-drift]: %d checks passed\n' "$checks"
