#!/usr/bin/env bash
# Hermetic test for release-tag.sh (#1047).
#
# The guard exists because the release tag can go missing with nothing
# observing it: a release PR merged by `app/github-actions` raises its push from
# GITHUB_TOKEN, GitHub starts no workflow runs for it, and auto-tag's push
# trigger never fires. v0.104.0's tag had to be pushed by hand.
#
# The states that must NOT fail are as important as the drift state itself: a
# guard that reds every lane during a release (or offline) is a guard people
# turn off, which is the "observed by nobody" shape this issue is about.
#
# Fixtures: a bare `origin`, a work clone that pushes `main`, and a subject
# clone that runs the guard. Commit dates are controlled so the in-flight grace
# window is exercised without sleeping. Hermetic: file-path remote, no network.
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/release-tag.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0
ok() { printf 'ok   %s\n' "$1"; }
no() {
  printf 'FAIL %s\n' "$1"
  failures=$((failures + 1))
}

git_quiet() { git "$@" >/dev/null 2>&1; }

origin="$tmp/origin.git"
work="$tmp/work"
clone="$tmp/clone"
git init -q --bare "$origin"
# Point the bare HEAD at main so the subject clone below checks it out.
git -C "$origin" symbolic-ref HEAD refs/heads/main
git init -q -b main "$work"
git -C "$work" config user.email test@example.com
git -C "$work" config user.name test
git_quiet -C "$work" remote add origin "$origin"

old_date="2020-01-01T00:00:00Z"
now_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# commit_version VERSION SUBJECT DATE -- land a bump on main and push it.
commit_version() {
  printf '%s\n' "$1" >"$work/VERSION"
  printf '%s\n' "$1" >"$work/notes"
  git_quiet -C "$work" add VERSION notes
  GIT_AUTHOR_DATE="$3" GIT_COMMITTER_DATE="$3" \
    git -C "$work" commit -q -m "$2"
  git_quiet -C "$work" push origin refs/heads/main:refs/heads/main
}

commit_version "1.0.0" "chore: release v1.0.0" "$old_date"
# A lane checkout: its own VERSION is irrelevant, main's is what is judged.
git clone -q "$origin" "$clone"

run() {
  out=$(cd "$clone" && env -u CI -u GITHUB_ACTIONS \
    RELEASE_BRANCH=main RELEASE_REMOTE="${remote:-origin}" \
    RELEASE_TAG_GRACE_MINUTES="${grace:-15}" \
    bash "$script" 2>&1)
  status=$?
}

# 1. The bug: main carries a version whose tag was never created. The bump is
#    old, so this is not a release in flight.
run
if ((status == 1)) && [[ $out == *"release tag v1.0.0 does not exist"* ]]; then
  ok "missing tag on an old bump fails the guard"
else
  no "missing tag on an old bump fails the guard (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi
if [[ $out == *"gh workflow run auto-tag.yml"* ]] && [[ $out == *"refs/tags/v1.0.0"* ]]; then
  ok "drift names both recoveries (dispatch, hand tag)"
else
  no "drift names both recoveries (dispatch, hand tag)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi
if [[ $out != *"deliberately untagged"* ]]; then
  ok "drift is not excused as deliberate"
else
  no "drift is not excused as deliberate"
fi

# 2. Once the annotated tag exists the guard is green -- and the peeled `^{}`
#    line `ls-remote` prints for an annotated tag must not be mistaken for a
#    malformed ref.
git_quiet -C "$work" tag -a v1.0.0 -m v1.0.0
git_quiet -C "$work" push origin refs/tags/v1.0.0
run
if ((status == 0)) && [[ $out == *"release tag aligned"* ]]; then
  ok "annotated tag for main's VERSION passes"
else
  no "annotated tag for main's VERSION passes (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi

# 3. A tag for an older version does not cover a newer bump -- the case a
#    "some tag exists" check would miss.
commit_version "2.0.0" "chore: release v2.0.0" "$old_date"
run
if ((status == 1)) && [[ $out == *"release tag v2.0.0 does not exist"* ]]; then
  ok "a tag for an older version does not cover a newer bump"
else
  no "a tag for an older version does not cover a newer bump (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi

# 4. The marker is the documented way to land a bump without tagging.
commit_version "3.0.0" "chore: release v3.0.0 [skip release]" "$old_date"
run
if ((status == 0)) && [[ $out == *"deliberately untagged"* ]]; then
  ok "[skip release] on the bump passes as deliberate"
else
  no "[skip release] on the bump passes as deliberate (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi

# 5. A bump made moments ago may be mid-release: a lane's PR must not go red in
#    that window. Once the window is over -- pinned here by the env override
#    instead of sleeping -- the same state is drift.
commit_version "4.0.0" "chore: release v4.0.0" "$now_date"
run
if ((status == 0)) && [[ $out == *"a release may be in flight"* ]]; then
  ok "a fresh bump is inside the grace window"
else
  no "a fresh bump is inside the grace window (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi
grace=0
run
if ((status == 1)) && [[ $out == *"release tag v4.0.0 does not exist"* ]]; then
  ok "the grace window is bounded (grace=0 fails the same state)"
else
  no "the grace window is bounded (grace=0 fails the same state) (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi
unset grace

# 6. Unreadable remote: warn and skip. Failing closed here would red every lane
#    on a network blip -- the guard would be turned off, which is worse.
remote=nope
run
if ((status == 0)) && [[ $out == *"WARNING could not fetch"* ]]; then
  ok "an unreadable remote warns and skips"
else
  no "an unreadable remote warns and skips (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi
unset remote

# 7. A release branch that does not exist is not drift either.
out=$(cd "$clone" && env -u CI RELEASE_BRANCH=release RELEASE_REMOTE=origin \
  bash "$script" 2>&1)
status=$?
if ((status == 0)) && [[ $out == *"WARNING could not fetch"* ]]; then
  ok "a missing release branch warns and skips"
else
  no "a missing release branch warns and skips (exit $status)"
  printf '%s\n' "$out" | sed 's/^/    /'
fi

if ((failures)); then
  printf 'release-tag tests: %d failure(s)\n' "$failures"
  exit 1
fi
printf 'release-tag tests: OK\n'
