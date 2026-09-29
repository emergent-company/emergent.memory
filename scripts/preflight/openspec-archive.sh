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
#   A change is stale when ALL hold:
#     1. its directory exists on the base ref (`--base`, default `origin/main`)
#        at `openspec/changes/<name>/` — proof the change was merged, since an
#        in-flight change lives only on a feature branch, not on main;
#     2. it is not yet under `openspec/changes/archive/` (archiving moves the
#        directory there, so a merged change still at the top level is
#        un-archived); AND
#     3. it DECLARES itself archive-ready (see ARCHIVE-READY below).
#
#   Condition 1 alone is NOT readiness. This repo commits many change dirs to
#   `main` before (or without) any implementation — spec-only/design PRs, openspec
#   re-homing, in-progress-work commits. Treating "dir exists on base" as
#   "archivable" would flag ~all active changes and, if archived, sync
#   requirements describing unimplemented behaviour (the #906 bug). Readiness is
#   therefore DECLARED per change, never inferred from base membership.
#
# ARCHIVE-READY (replaces the old `completed == total` gate):
#   A merged, un-archived change is stale when EITHER:
#     (i) every task in its `tasks.md` is ticked
#         (`totalTasks > 0 && completedTasks == totalTasks`) — preserves the
#         long-standing behaviour for well-ticked changes; OR
#     (ii) its `tasks.md` carries an explicit readiness marker
#          (`<!-- openspec:archive-ready -->`), for implementations that merged
#          with the checkboxes un-backfilled. This is the escape hatch that
#          un-strands changes the tick gate used to hide (#1210/#1211 lineage).
#
#   The guard always REPORTS the tick state (`completedTasks`/`totalTasks`) in
#   both output modes, but ticks are evidence, not the sole gate.
#
#   Condition 1 is what keeps the guard from false-positiving on an in-flight
#   change whose PR is still open: on a feature branch that change does NOT
#   exist on `origin/main`, so it is not flagged. A just-merged ready change IS
#   flagged — deliberately: that is the post-merge follow-up window this guard
#   exists to surface. Run it from the scheduled/post-merge workflow, not as a
#   pre-merge gate (a pre-merge gate would fire on every legitimately-deferred
#   archive).
#
# SAFETY VALVE (opt-out for deliberately-incomplete merged changes):
#   A merged change whose remaining tasks are intentionally deferred can opt out
#   of the drift report with a marker comment anywhere in its `tasks.md`:
#
#     <!-- openspec:archive-hold: <reason> -->
#
#   Hold wins over archive-ready. The guard skips held changes and LISTS them,
#   with their reason, in both the human and `--json` output, so it does not nag
#   about a change whose milestones are deliberately deferred — e.g.
#   `unify-scope-authority`, whose §1–§6 shipped but §7/§8 are deferred by
#   decision (#1161), so archiving now would sync requirements describing
#   unimplemented behaviour. Markers are read from the working-tree `tasks.md`.
#
# Usage:
#   scripts/preflight/openspec-archive.sh [--base <ref>] [--warn] [--json]
#     --base <ref>  Base ref whose tree proves "merged" (default: origin/main;
#                   use HEAD for a post-merge run on main).
#     --warn        Report drift but exit 0 (advisory).
#     --json        Emit a machine-readable result on stdout (stale + held).
#   Exit: 0 clean, 1 drift found, 2 usage error.
#
#   Declare readiness with `<!-- openspec:archive-ready -->` and opt out with
#   `<!-- openspec:archive-hold: <reason> -->`, both in `tasks.md`. Held changes
#   are still listed for visibility. See ARCHIVE-READY / SAFETY VALVE above.
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
    -h|--help) sed -n '2,76p' "$0"; exit 0 ;;
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

# Keep changes that are merged on the base ref, un-archived, DECLARED
# archive-ready, and not held. Python does the JSON work; `git rev-parse`
# proves base membership of the change directory.
set +e
OPENSPEC_ARCHIVE_BASE="$base" OPENSPEC_ARCHIVE_JSON="$json" \
python3 - "$list_json" <<'PY'
import json
import os
import re
import subprocess
import sys

base = os.environ["OPENSPEC_ARCHIVE_BASE"]
as_json = os.environ.get("OPENSPEC_ARCHIVE_JSON") == "1"

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)

changes = data.get("changes", []) if isinstance(data, dict) else data

READY_RE = re.compile(r"<!--\s*openspec:archive-ready\s*-->")
HOLD_RE = re.compile(r"<!--\s*openspec:archive-hold:\s*(.*?)\s*-->", re.S)


def merged_on_base(name):
    """True when the change dir exists on the base ref (=> merged).

    Archived changes live under `openspec/changes/archive/`, so a top-level
    `openspec/changes/<name>` on the base ref means merged-but-un-archived.
    """
    probe = f"{base}:openspec/changes/{name}"
    return subprocess.run(
        ["git", "rev-parse", "--verify", "--quiet", probe],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    ).returncode == 0


def read_tasks(name):
    """Working-tree tasks.md text, or '' when absent."""
    path = f"openspec/changes/{name}/tasks.md"
    try:
        with open(path, encoding="utf-8") as fh:
            return fh.read()
    except OSError:
        return ""


def hold_reason(text):
    """Return the opt-out reason when tasks.md carries the hold marker."""
    match = HOLD_RE.search(text)
    if not match:
        return None
    return match.group(1).strip() or "(no reason given)"


def is_ready(text, done, total):
    """A change is archivable when all tasks are ticked, or it declares ready."""
    return (total > 0 and done >= total) or bool(READY_RE.search(text))


stale = []
held = []
for c in changes:
    name = c.get("name", "")
    if not name or not merged_on_base(name):
        continue
    done = c.get("completedTasks", 0)
    total = c.get("totalTasks", 0)
    entry = {
        "name": name,
        "completedTasks": done,
        "totalTasks": total,
        "lastModified": c.get("lastModified"),
    }
    text = read_tasks(name)
    reason = hold_reason(text)
    if reason is not None:
        held.append({**entry, "reason": reason})
    elif is_ready(text, done, total):
        stale.append(entry)

stale.sort(key=lambda x: x["name"])
held.sort(key=lambda x: x["name"])

if as_json:
    print(
        json.dumps(
            {
                "base": base,
                "count": len(stale),
                "stale": stale,
                "heldCount": len(held),
                "held": held,
            },
            indent=2,
        )
    )
else:
    if not stale:
        print(f"openspec-archive: OK (no merged-but-un-archived ready changes on {base})")
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
    if held:
        print(
            f"openspec-archive: {len(held)} merged change(s) HELD (exempt via marker):",
            file=sys.stderr,
        )
        for h in held:
            print(
                f"  - {h['name']} ({h['completedTasks']}/{h['totalTasks']} tasks): {h['reason']}",
                file=sys.stderr,
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
