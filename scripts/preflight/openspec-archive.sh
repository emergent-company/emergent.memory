#!/usr/bin/env bash
# Detect merged-but-un-archived OpenSpec changes (archive drift).
#
# WHY: non-trivial work ships an OpenSpec change under `openspec/changes/<name>/`
# in the same PR, and `openspec archive` is supposed to run AFTER merge to sync
# the change's delta specs into `openspec/specs/`. Nothing enforced that
# follow-up, so completed change dirs accumulated on `main` and the specs
# silently stopped describing shipped behaviour (#898: ~34 stale changes; the
# same class recurred in #842/#857/#859/#882/#906). This is the deterministic
# check that closes the loop (#1063).
#
# WHAT "STALE" MEANS (the exact line drawn):
#   A change is stale when BOTH hold:
#     1. all of its tasks are complete (`totalTasks > 0 && completed == total`),
#        i.e. `openspec list` reports `status: complete`; AND
#     2. its directory is already on the base ref (`--base`, default
#        `origin/main`) — proof the implementation PR was merged, since an
#        in-flight change lives only on a feature branch, not on main.
#
#   Condition 2 is what keeps the guard from false-positiving on an in-flight
#   change whose tasks are all ticked but whose PR is still open: on a feature
#   branch that change does NOT exist on `origin/main`, so it is not flagged.
#   A just-merged complete change IS flagged — deliberately: that is the
#   post-merge follow-up window this guard exists to surface. Run it from the
#   scheduled/post-merge workflow, not as a pre-merge gate (a pre-merge gate
#   would fire on every legitimately-deferred archive).
#
#   Changes with no tasks (`totalTasks == 0`) and changes with incomplete tasks
#   are never flagged: their archive state is ambiguous, and a guard that cries
#   wolf is worse than no guard.
#
# Usage:
#   scripts/preflight/openspec-archive.sh [--base <ref>] [--warn] [--json]
#     --base <ref>  Base ref whose tree proves "merged" (default: origin/main;
#                   use HEAD for a post-merge run on main).
#     --warn        Report drift but exit 0 (advisory).
#     --json        Emit a machine-readable result on stdout.
#   Exit: 0 clean, 1 drift found, 2 usage error.
#
# Offline / tooling is tolerated the way base-check.sh tolerates it: if the base
# ref cannot be resolved or the `openspec` CLI is missing, warn and skip (0) so
# an environment problem never masquerades as drift.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

base="origin/main"
warn=0
json=0
while [ $# -gt 0 ]; do
  case "$1" in
    --base) base="${2:-}"; shift 2 ;;
    --warn) warn=1; shift ;;
    --json) json=1; shift ;;
    -h|--help) sed -n '2,45p' "$0"; exit 0 ;;
    *) echo "openspec-archive: unknown argument: $1" >&2; exit 2 ;;
  esac
done

if ! command -v openspec >/dev/null 2>&1; then
  echo "openspec-archive: WARNING 'openspec' CLI not on PATH; skipping" >&2
  exit 0
fi

# Resolve the base ref. HEAD always resolves; a remote ref may need a fetch,
# and no-network is not fatal.
if [ "$base" != "HEAD" ] && ! git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
  git fetch --quiet origin main || true
fi
if ! git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
  echo "openspec-archive: WARNING base '$base' unavailable; skipping" >&2
  exit 0
fi

list_json="$(mktemp)"
trap 'rm -f "$list_json"' EXIT
if ! openspec list --json >"$list_json" 2>/dev/null; then
  echo "openspec-archive: WARNING 'openspec list --json' failed; skipping" >&2
  exit 0
fi

# Filter candidates (complete changes), then keep only those present on the
# base ref. Python does the JSON work; `git cat-file` proves base membership.
set +e
OPENSPEC_ARCHIVE_BASE="$base" OPENSPEC_ARCHIVE_JSON="$json" \
python3 - "$list_json" <<'PY'
import json
import os
import subprocess
import sys

base = os.environ["OPENSPEC_ARCHIVE_BASE"]
as_json = os.environ.get("OPENSPEC_ARCHIVE_JSON") == "1"

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)

changes = data.get("changes", []) if isinstance(data, dict) else data

stale = []
for c in changes:
    total = c.get("totalTasks", 0)
    done = c.get("completedTasks", 0)
    if total <= 0 or done < total:
        continue
    name = c.get("name", "")
    # Present on the base ref => merged. tasks.md always exists for a change
    # that has tasks.
    probe = f"{base}:openspec/changes/{name}/tasks.md"
    if subprocess.run(
        ["git", "cat-file", "-e", probe],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    ).returncode == 0:
        stale.append(
            {
                "name": name,
                "completedTasks": done,
                "totalTasks": total,
                "lastModified": c.get("lastModified"),
            }
        )

stale.sort(key=lambda x: x["name"])

if as_json:
    print(json.dumps({"base": base, "count": len(stale), "stale": stale}, indent=2))
else:
    if not stale:
        print(f"openspec-archive: OK (no merged-but-un-archived changes on {base})")
    else:
        print(
            f"openspec-archive: {len(stale)} merged-but-un-archived change(s) on {base}:",
            file=sys.stderr,
        )
        for s in stale:
            print(
                f"  - {s['name']} ({s['completedTasks']}/{s['totalTasks']} tasks complete)",
                file=sys.stderr,
            )
        print("", file=sys.stderr)
        print("  Archive each (post-merge) and open one follow-up PR:", file=sys.stderr)
        for s in stale:
            print(f"    openspec archive {s['name']} --yes", file=sys.stderr)
        print("", file=sys.stderr)
        print(
            "  Or locally: task openspec:archive-check", file=sys.stderr
        )

sys.exit(1 if stale else 0)
PY
rc=$?
set -e

if [ "$rc" -ne 0 ] && [ "$warn" -eq 1 ]; then
  echo "openspec-archive: WARN (advisory mode; not failing)" >&2
  exit 0
fi

exit "$rc"
