#!/usr/bin/env bash
# Assert every go.mod in the repo (outside vendor/) is covered by a Go workspace,
# so workspace-wide commands (`go build ./...`, linters driven by the workspace)
# cannot silently skip a module. Also rejects stale `use` entries that point at a
# directory with no go.mod.
#
# A module is covered when either:
#   1. the root go.work lists it, or
#   2. the allowlisted isolated root's own go.work claims it — `isolated_roots`
#      below, currently only e2e (see
#      emergent.memory.infra:openspec/specs/server-agent-runtime/spec.md, "E2E module SHALL be isolated
#      from the workspace build list"). A deeper go.work inside the subtree does
#      NOT count: the isolated root's workspace is the one a command run at that
#      root resolves, so it is the one that must claim every module beneath it.
# Rule 2 is deliberately opt-in by editing the list: dropping a nested go.work
# somewhere must never be enough to escape the root build list, or an arbitrary
# module could silently stop being built — the very drift class (#923) this
# guard exists to catch.
#
# See issue: go.work drift lets a broken module go unnoticed (same class as
# #923). This guard is wired into Meta CI (ci.yml), which runs on every PR.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

entries_of() {
  # Print the `use` entries of a go.work file, one relative path per line.
  # Handles both `.` (the workspace's own module) and `./sub/dir` entries.
  local workfile="$1"
  awk '
    /^use[[:space:]]*\(/ { inuse = 1; next }
    inuse && /^[[:space:]]*\)/ { inuse = 0; next }
    inuse {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", $0)
      if ($0 != "") print $0
    }
  ' "$workfile"
}

root_entries="$(mktemp)"
trap 'rm -f "$root_entries"' EXIT
entries_of go.work >"$root_entries"

# Roots whose modules are allowed to keep their own workspace (rule 2). Each entry
# must be justified by a spec requirement — do not add one without one.
isolated_roots=(
  "./e2e"
)

# Print the allowlisted isolated root that owns $1 — the nearest (longest) match —
# or nothing when the module is not under one.
isolated_root_of() {
  local dir="$1" root best=""
  for root in "${isolated_roots[@]}"; do
    case "$dir" in
      "$root" | "$root"/*)
        if [ "${#root}" -gt "${#best}" ]; then best="$root"; fi
        ;;
    esac
  done
  if [ -n "$best" ]; then printf '%s\n' "$best"; fi
}

status=0

# 1. Every go.mod (outside vendor/) must be covered by a workspace.
while IFS= read -r modfile; do
  dir="${modfile%/go.mod}"

  if grep -qxF "$dir" "$root_entries"; then
    continue
  fi

  # Walk from the module directory up to the edge of its isolated root, looking
  # for a nested go.work that claims it. `rel` is the module path relative to that
  # go.work's directory (".", "./stubs/mailgun", …).
  covered=0
  iso_root="$(isolated_root_of "$dir")"
  if [ -n "$iso_root" ] && [ -f "$iso_root/go.work" ]; then
    if [ "$dir" = "$iso_root" ]; then
      rel="."
    else
      rel="${dir#"$iso_root"}"
      rel="./${rel#/}"
    fi
    if entries_of "$iso_root/go.work" | grep -qxF "$rel"; then
      covered=1
    fi
  fi

  if [ "$covered" -ne 1 ]; then
    printf 'go.work missing module: %s (not in the root go.work, and not claimed by an allowlisted isolated root go.work)\n' "$dir" >&2
    status=1
  fi
done < <(find . -name go.mod -not -path './vendor/*' -not -path '*/vendor/*' | sort)

# 2. No root go.work entry may point at a directory lacking a go.mod.
while IFS= read -r dir; do
  if [ ! -f "$dir/go.mod" ]; then
    printf 'go.work entry has no go.mod: %s\n' "$dir" >&2
    status=1
  fi
done <"$root_entries"

# 3. Nested go.work entries must resolve too (same stale-entry rule).
while IFS= read -r nested; do
  workdir="$(dirname "$nested")"
  while IFS= read -r entry; do
    if [ ! -f "$workdir/${entry#./}/go.mod" ]; then
      printf 'nested workspace entry has no go.mod: %s -> %s\n' "$nested" "$entry" >&2
      status=1
    fi
  done < <(entries_of "$nested")
done < <(find . -name go.work -not -path './vendor/*' -not -path '*/vendor/*' -not -path './go.work' | sort)

if [ "$status" -ne 0 ]; then
  echo "go.work coverage check FAILED" >&2
  exit 1
fi

echo "go.work coverage check OK: all modules accounted for"
