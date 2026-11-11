#!/usr/bin/env bash
# Guard against version drift between the canonical root VERSION file and its
# checked-in references:
#   * the OpenAPI `// @version` annotation in apps/server/cmd/server/main.go
#   * every MARKETING_VERSION in apps/ios/VoiceAgent.xcodeproj/project.pbxproj
#     (VoiceAgent app, Debug + Release configs)
#
# The generated swagger spec used to be a third reference. It is no longer
# committed (#270), and it is produced *from* the `@version` annotation by
# `task server:swagger`, so comparing the two would be a tautology that only
# ever fired against a stale artifact — or against no artifact at all on a
# fresh checkout, which is what broke this guard.
#
# Root VERSION is the single canonical source of the release version; a release
# must bump every reference together (see the `release` task in Taskfile.yml).
# Fails with a clear message when any of them diverge.
#
# ── Release-attribution guard (control-repo issue #1074) ─────────────────────
# A VERSION bump must be its own reviewable PR, so the release skill mandates a
# `chore: release vX.Y.Z` commit. When a bump rides inside an unrelated fix PR
# it silently collides with a lane's dedicated release PR (which becomes an
# empty diff) and the tag lands on a commit whose subject describes something
# else. This guard fails a PR/push that changes root VERSION unless the change
# is attributable to a release commit, in three documented ways:
#   1. the PR carries the `release` label (CI: PR_LABELS env, comma-separated);
#   2. a commit subject in the range contains `[skip release]` — the same
#      marker auto-tag.yml honours, so a bump that won't tag may ride along;
#   3. a commit subject in the range matches `chore: release vX.Y.Z`.
#
# Only runs in CI, where a base ref (and PR_LABELS for PRs) is present. The
# local pre-commit hook runs before the commit subject exists and has no base
# ref, so it skips attribution; CI is the authoritative gate. The drift checks
# above run everywhere. VERSION-change detection uses a three-dot compare
# (`git diff <base>...HEAD`, i.e. against the merge-base) so a stale branch
# that is merely behind main's own release bump does not false-fail.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

pbx="apps/ios/VoiceAgent.xcodeproj/project.pbxproj"

root_version="$(tr -d '[:space:]' < VERSION)"

api_line="$(grep -m1 '@version' apps/server/cmd/server/main.go || true)"
api_version="$(printf '%s' "$api_line" | sed -E 's#.*@version[[:space:]]*##' | tr -d '[:space:]')"

if [ -z "$root_version" ] || [ -z "$api_version" ]; then
  echo "version drift guard: could not read VERSION and/or the @version annotation" >&2
  exit 1
fi

status=0
attribution_failed=0

if [ "$root_version" != "$api_version" ]; then
  echo "version drift: root VERSION=$root_version != apps/server/cmd/server/main.go @version=$api_version" >&2
  status=1
fi

ios_versions="$(sed -nE 's/^[[:space:]]*MARKETING_VERSION = ([^;]+);.*/\1/p' "$pbx")"
if [ -z "$ios_versions" ]; then
  echo "version drift guard: no MARKETING_VERSION found in $pbx" >&2
  status=1
else
  for mv in $ios_versions; do
    if [ "$mv" != "$root_version" ]; then
      echo "version drift: $pbx MARKETING_VERSION=$mv != root VERSION=$root_version" >&2
      status=1
    fi
  done
fi

# --- Release attribution (control-repo issue #1074) --------------------------
# Only meaningful in CI, where a base ref and (for PRs) PR_LABELS are present.
# Detected by the base-ref env vars GitHub Actions sets for PRs and pushes; the
# local pre-commit hook has neither, so it skips this half.
if [ -n "${GITHUB_BASE_REF:-}" ] || [ -n "${GITHUB_EVENT_BEFORE:-}" ]; then
  if [ -n "${GITHUB_BASE_REF:-}" ]; then
    base="origin/${GITHUB_BASE_REF}"
  elif [ "${GITHUB_EVENT_BEFORE:-}" = "0000000000000000000000000000000000000000" ]; then
    base="origin/main"
  else
    base="${GITHUB_EVENT_BEFORE}"
  fi

  if git rev-parse --verify --quiet "${base}^{commit}" >/dev/null 2>&1; then
    # Three-dot compare: only changes this branch introduces relative to the
    # merge-base, so a stale branch behind main's own bump does not false-fail.
    if ! git diff --quiet "${base}...HEAD" -- VERSION; then
      allowed=0
      allow_reason=""

      # (1) PR label. CI exports PR_LABELS as a comma-separated list.
      if [ -n "${PR_LABELS:-}" ]; then
        labels=",$(printf '%s' "$PR_LABELS" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]'),"
        case "$labels" in
          *",release,"*) allowed=1; allow_reason="PR 'release' label" ;;
        esac
      fi

      # (2)/(3) commit subjects in the range.
      if [ "$allowed" -eq 0 ]; then
        subjects="$(git log --no-merges --format='%s' "${base}..HEAD" || true)"
        if printf '%s\n' "$subjects" | grep -Fq '[skip release]'; then
          allowed=1; allow_reason="'[skip release]' marker"
        elif printf '%s\n' "$subjects" | grep -Eq '^chore: release v[0-9]+\.[0-9]+\.[0-9]+([[:space:]]|$)'; then
          allowed=1; allow_reason="release commit subject"
        fi
      fi

      if [ "$allowed" -eq 0 ]; then
        echo "version attribution: VERSION changed relative to '${base}', but this is not a release PR." >&2
        echo "  A VERSION bump must be its own PR with a commit subject:" >&2
        echo "    chore: release vX.Y.Z" >&2
        echo "  Escapes: the PR 'release' label, a '[skip release]' marker in a commit" >&2
        echo "  subject (also suppresses auto-tagging), or the release subject above." >&2
        echo "  (control-repo issue #1074)" >&2
        attribution_failed=1
      else
        echo "version attribution: VERSION bump allowed by ${allow_reason}"
      fi
    fi
  else
    echo "version attribution: WARNING base '${base}' unavailable; skipping attribution check" >&2
  fi
fi

if [ "$attribution_failed" -ne 0 ]; then
  exit 1
fi

if [ "$status" -ne 0 ]; then
  echo "Bump VERSION, @version and all iOS MARKETING_VERSION values together — see the 'release' task in Taskfile.yml." >&2
  exit 1
fi

echo "version aligned: $root_version"
