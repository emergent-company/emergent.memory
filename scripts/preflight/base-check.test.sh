#!/usr/bin/env bash
# Hermetic test for base-check.sh's base-ref fetch (#956).
#
# A tag and a branch with the same name shadow each other for a bare-name fetch:
# `git fetch origin dev` resolves the TAG, prints `* tag dev -> FETCH_HEAD` and
# exits 0 *even with --no-tags*, leaving origin/dev untouched. base-check.sh
# exists to catch a stale base, and a stale origin/dev is exactly the state that
# makes it report OK -- so the fetch must use an explicit
# `refs/heads/<branch>:refs/remotes/origin/<branch>` refspec.
#
# Fixtures: a bare "origin" plus two clones, one of them a single-branch clone
# (what actions/checkout and a lane worktree resemble). The clone's origin/dev is
# rewound by hand to simulate staleness, a tag `dev` points at an older commit,
# and origin/dev advances -- so only a fetch that actually resolves the branch
# can see the new tip.
#
# Hermetic: no network, no self-hosted runner, runs on the Linux CI runner.
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/base-check.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0
ok() { printf 'ok   %s\n' "$1"; }
no() {
  printf 'FAIL %s\n' "$1"
  failures=$((failures + 1))
}

git_quiet() { git "$@" >/dev/null 2>&1; }
ident() {
  git -C "$1" config user.email "test@example.com"
  git -C "$1" config user.name "test"
}

origin="$tmp/origin.git"
work="$tmp/work"
clone="$tmp/clone"
git init -q --bare "$origin"
git init -q -b dev "$work"
ident "$work"
git_quiet -C "$work" remote add origin "$origin"
printf 'one\n' >"$work/file"
git_quiet -C "$work" add file
git_quiet -C "$work" commit -m one
base_old=$(git -C "$work" rev-parse HEAD)
printf 'two\n' >"$work/file"
git_quiet -C "$work" commit -am two
# The shadowing tag: `dev` also names the branch, pointing at the older commit.
git_quiet -C "$work" tag dev "$base_old"
# Full refspecs: once both refs/heads/dev and refs/tags/dev exist, the bare
# name `git push origin dev` fails with "src refspec dev matches more than one".
git_quiet -C "$work" push origin refs/heads/dev:refs/heads/dev
git_quiet -C "$work" push origin refs/tags/dev

# Single-branch clone, like a lane checkout: only refs/heads/dev is fetched.
git clone -q --single-branch --branch dev "$origin" "$clone"
ident "$clone"

# Rewind the tracking ref so HEAD looks level with a stale origin/dev. This is
# the state a bare-name fetch leaves in place: it resolves the tag, exits 0, and
# origin/dev never moves.
head_sha=$(git -C "$clone" rev-parse HEAD)
git_quiet -C "$clone" update-ref refs/remotes/origin/dev "$base_old"

# Advance the branch on origin.
printf 'three\n' >"$work/file"
git_quiet -C "$work" commit -am three
new_tip=$(git -C "$work" rev-parse HEAD)
git_quiet -C "$work" push origin refs/heads/dev:refs/heads/dev

run() {
  out=$(cd "$1" && env -u CI -u GITHUB_ACTIONS PREFLIGHT_BASE=dev bash "$script" 2>&1)
  status=$?
}

# 1. With the tag present, the fetch must still move origin/dev to the new tip
#    and report the stale base. A bare-name fetch would resolve the tag, leave
#    origin/dev at $base_old, print "OK", and exit 0 -- the bug.
run "$clone"
if ((status == 1)) && [[ $out == *"behind"* ]]; then
  ok "shadowing tag does not hide a stale base"
else
  no "shadowing tag does not hide a stale base (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi
if [ "$(git -C "$clone" rev-parse refs/remotes/origin/dev)" = "$new_tip" ]; then
  ok "origin/dev advanced to the branch tip"
else
  no "origin/dev did not advance to the branch tip"
fi
if [ "$head_sha" != "$new_tip" ]; then
  ok "fixture is genuinely behind (head=$head_sha tip=$new_tip)"
else
  no "fixture is not behind; the stale case proves nothing"
fi

# 2. Once the branch is fast-forwarded, the guard is green -- the explicit
#    refspec must not turn every run red.
git_quiet -C "$clone" merge --ff-only origin/dev
run "$clone"
if ((status == 0)) && [[ $out == *"OK"* ]]; then
  ok "fresh base passes"
else
  no "fresh base passes (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi

# 3. A force-pushed base must still refresh. Without the leading `+` on the
#    refspec, `git fetch` refuses the non-fast-forward update, base-check falls
#    back to the cached tip and reports the rewritten branch as fresh.
git_quiet -C "$work" checkout -q -B dev "$base_old"
printf 'four\n' >"$work/file"
git_quiet -C "$work" commit -am four
divergent=$(git -C "$work" rev-parse HEAD)
git_quiet -C "$work" push --force origin refs/heads/dev:refs/heads/dev
run "$clone"
if ((status == 1)) && [[ $out == *"behind"* ]] \
  && [ "$(git -C "$clone" rev-parse refs/remotes/origin/dev)" = "$divergent" ]; then
  ok "force-pushed base refreshes instead of using the cached tip"
else
  no "force-pushed base refreshes instead of using the cached tip (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi

if ((failures)); then
  printf 'base-check tests: %d failure(s)\n' "$failures"
  exit 1
fi
printf 'base-check tests: OK\n'
