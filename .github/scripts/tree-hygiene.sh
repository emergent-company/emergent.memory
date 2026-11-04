#!/usr/bin/env bash
# Tree hygiene: fail on internal hostnames and private-repo references in
# tracked files.
#
# Extracted from the workflow's `run:` body (#2343) so the pattern and the
# exemption rules can be tested. The gate cannot carry its own fixtures: it scans
# *tracked* files, so a fixture holding a fake leak would fail the gate it is
# meant to test. `.github/scripts/tree-hygiene.test.sh` instead runs this script
# over a temporary directory, one reference form per file, and is itself run from
# `ci.yml`'s meta job -- so a loosened pattern reds CI with a named case instead
# of being noticed in production or by a reviewer reading the spellings off a
# diff.
#
# Dependency-free on purpose (bash + git + grep): it runs on a hosted runner
# before any toolchain step, and the workflow deliberately has no setup.
#
# Coverage of the scan (case-insensitive, whitespace-flexible):
#   - tailnet names / public DNS of our own hosts (.ts.net, dev.emergent-company.ai)
#   - rig, dev, prod and Mac build host names (`memory-hetzner` = prod,
#     `memory-dev` = dev) and their addresses, plus the `mcj` machine, runner
#     and build-target names as a family. The word-boundary form catches the
#     bare name and the `mcj-` prefix, not just the particular machine names
#     known today, without matching a longer word such as `mcjones`.
#   - the agent-rig filesystem layout (~/.paseo, /Users/mcj, and rig checkouts
#     under /root: emergent, blueprints, workspace-memory, doc-processing,
#     files-memory)
#   - private repo names that must never be named from the product repo (the
#     infra repo under both its current and its former name, and the fleet repo)
#   - prefixed issue numbers in those repos, which resolve to unrelated issues
#     here: `infra #123`, `infra issue #123`, `infra tracking issue #123`, plus
#     the spellings a case-sensitive, single-space pattern let through -- any
#     capitalisation (`Infra`, `INFRA`), any run of spaces or a tab where one
#     space was expected (`infra  #123`, `infra<TAB>issue<TAB>#123`), a colon
#     separator (`infra: #123`) and no separator at all (`infra#123`). The whole
#     scan is case-insensitive, not just this alternative: `MEMORY-HETZNER` is
#     the same leak as `memory-hetzner`. "Infra #123" in prose is therefore a
#     violation too -- reword it. A bare `#123` cannot be distinguished from a
#     product issue, so the gate cannot catch it: reword or drop such references
#     by hand.
#   - a tenant name that leaked once and must not come back
#   - private OpenSpec-process pointers (#2443): a path segment `openspec/`
#     (including a backticked `` `openspec/` ``), the `` `openspec` `` code
#     span, and a backticked kebab-case change slug on a line that also names
#     openspec/OpenSpec -- the shape #2249 reintroduced with
#     `` `documents-list-filters` `` ("OpenSpec change `documents-list-filters`").
#     The slug alternative is anchored to that co-occurrence on purpose: a bare
#     `openspec` word, the word "specification" and an unrelated backticked
#     kebab token (`` `make-foo-bar` ``) are all legitimate prose and stay clean.
#     A change slug without backticks or an openspec mention is indistinguishable
#     from an ordinary identifier, so the gate cannot catch it: the hand-run
#     publication scan still owns that residue.
#
# Raw tailnet IPs are not hostnames, so they need their own alternatives. The
# 100.64.0.0/10 range appears legitimately in SSRF guards and their tests, so
# match the specific leaked hosts, not the whole range.
set -euo pipefail

# Fixed locations inside the repository. The allowlist and the baseline stay
# where they are: they are referenced from .github/ and the grandfathered
# entries are path-scoped, so moving them would silently change what they cover.
ALLOWLIST=.github/tree-hygiene-allowlist.txt
BASELINE=.github/tree-hygiene-baseline.txt
ROOT=
EXTRA_SELF=()

# Paths that are exempt because of what they are, not where they are. The scan,
# the workflow that calls it, and the test all quote the alternatives on purpose
# -- a probe literal is the material these files are made of -- so they would
# fail the gate they implement. Nothing else is exempt for being part of the
# gate: the allowlist and the baseline are exempt as files too (an entry's inline
# comment may name a host), by the resolved paths below.
SELF_PATHS=(
  .github/workflows/tree-hygiene.yml
  .github/scripts/tree-hygiene.sh
  .github/scripts/tree-hygiene.test.sh
)

usage() {
  cat <<'EOF'
Usage: tree-hygiene.sh [--root DIR] [--allowlist FILE] [--baseline FILE] [--self PATH]

Scans the tracked files of the current git work tree -- or, when DIR is not
inside one, every file under DIR -- for internal hostnames and private-repo
references. Paths listed in the allowlist or the baseline, the scan's own files
and anything passed to --self are exempt.

  --root DIR         directory to scan (default: the work tree root, scanning its
                     tracked files). A subdirectory root scans the tracked files
                     under it, with DIR-relative paths; outside git every file
                     under it is read
  --allowlist FILE   legitimate permanent references (default: .github/tree-hygiene-allowlist.txt)
  --baseline FILE    grandfathered pre-existing entries, a ratchet that may only
                     shrink (default: .github/tree-hygiene-baseline.txt)
                     Both are read relative to the work tree root (DIR when DIR is
                     not in one). Their entries are repository paths and are
                     matched against scan paths, which are DIR-relative.
  --self PATH        an additional path to exempt, relative to DIR or to the
                     work tree root (both are accepted, as for a list entry)

Exit 0 when clean, 1 on violations (listed as path:line:text), 2 on usage error.
EOF
}

while (($#)); do
  case "$1" in
    --root) ROOT=${2:?--root needs a directory}; shift 2 ;;
    --allowlist) ALLOWLIST=${2:?--allowlist needs a file}; shift 2 ;;
    --baseline) BASELINE=${2:?--baseline needs a file}; shift 2 ;;
    --self) EXTRA_SELF+=("${2:?--self needs a path}"); shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$ROOT" ]]; then
  ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "tree-hygiene: not in a git work tree and no --root given" >&2
    exit 2
  }
fi
if [[ ! -d "$ROOT" ]]; then
  echo "tree-hygiene: no such directory: $ROOT" >&2
  exit 2
fi

# Absolute and canonical: the exemption below strips "$ROOT/" off the paths it
# compares, and `--root .` or `--root dir/` would leave every comparison
# unmatched (#2358). `pwd -P` also resolves a symlinked root, so a list file
# named through a different spelling still compares as the same file (#2370).
ROOT=$(cd "$ROOT" && pwd -P)

# Inside a work tree the gate's own files and its list files are the
# repository's, even when DIR is a subdirectory of it, while scan paths are
# relative to DIR -- so root_prefix is what turns one form into the other ("" at
# the top of the work tree, "a/b/" below it).
root_prefix=""
list_root=$ROOT
if top=$(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null); then
  list_root=$(cd "$top" && pwd -P)
  root_prefix=$(git -C "$ROOT" rev-parse --show-prefix)
fi

# The two list files are read relative to list_root (the repository root for the
# gate, a fixture tree for the tests).
allow_file=$ALLOWLIST
[[ $allow_file = /* ]] || allow_file="$list_root/$allow_file"
base_file=$BASELINE
[[ $base_file = /* ]] || base_file="$list_root/$base_file"

# The scan's own regex list is single-quoted so the shell passes it through
# verbatim; the backticks in the OpenSpec alternatives are regex literals, not
# command substitution.
# shellcheck disable=SC2016
PATTERN='\.ts\.net\b|emergent-dev|10\.10\.10\.|100\.69\.175\.118|100\.91\.139\.28|emergent\.memory\.infra|emergent-infra|b4ng-link|\.dev\.emergent-company\.ai|memory-home|memory-hetzner|\bmemory-dev\b|\bmemory-prod\b|\bmcj\b|/Users/mcj|\bmcj@|Mac\.banglab|\bbanglab\b|100\.118\.32\.29|100\.119\.120\.71|100\.121\.213\.124|100\.84\.125\.23|100\.67\.61\.62|95\.216\.38\.217|195\.201\.196\.144|46\.4\.253\.156|emergent\.memory\.mac|emergent\.memory\.website|emergent\.memory-archive|/root/(emergent|blueprints|workspace-memory|doc-processing|files-memory)|/opt/emergent-dev|\.paseo/|legalplant|infra([[:space:]]*:[[:space:]]*|[[:space:]]+)?(issue[[:space:]]+|tracking[[:space:]]+issue[[:space:]]+)?[[:space:]]*#[0-9]+|\bopenspec/|`openspec`|openspec.*`[a-z][a-z0-9]*(-[a-z0-9]+)+`|`[a-z][a-z0-9]*(-[a-z0-9]+)+`.*openspec'

# canon_path <path> -- the path with its directory resolved to the physical
# spelling, with the basename joined back so the file itself need not exist (the
# tests pass /dev/null as a baseline). Two spellings of one list file, or a list
# file and the scan path that names it, only compare equal once both sides are
# resolved (#2370).
canon_path() {
  printf '%s/%s\n' "$(cd "$(dirname -- "$1")" && pwd -P)" "$(basename -- "$1")"
}

exempt() {
  local path="$1" p ref as_root="$path"
  # The gate's own files, the list files and --self are exempt. Each is tried in
  # the form the scan reports and in the form it was given: scan paths are
  # relative to ROOT, the gate's own files are written repository-relative, and
  # a list file or --self may be repository-relative, DIR-relative or absolute.
  # `as_root` re-prefixes a subdirectory scan's DIR-relative path into the
  # repository-relative form; that is what lets one comparison cover both
  # spellings of --self and a repository-relative list entry. Absolute spellings
  # are then compared canonically, so a list file or the root may be reached
  # through a symlink (#2358, #2370).
  local self_refs=("${SELF_PATHS[@]}")
  for ref in "$ALLOWLIST" "$BASELINE" "${EXTRA_SELF[@]}"; do
    self_refs+=("$ref" "${ref#"$ROOT"/}")
  done
  # Each is accepted as given and as the repository-relative form of the scan
  # path: a subdirectory root reports DIR-relative paths, so both spellings name
  # the same repository file. One comparison covers the gate's own files, the list
  # files, --self and the list entries below.
  [[ -n "$root_prefix" ]] && as_root="$root_prefix$path"
  for p in "${self_refs[@]}"; do
    [[ "$path" == "$p" || "$as_root" == "$p" ]] && return 0
  done
  # The string comparisons above cannot see through a symlinked spelling, so the
  # absolute forms are compared again after resolving both directories.
  local scan_abs
  scan_abs=$(canon_path "$ROOT/$path")
  for ref in "$allow_file" "$base_file" "${EXTRA_SELF[@]}"; do
    [[ $ref = /* ]] || continue
    [[ "$scan_abs" == "$(canon_path "$ref")" ]] && return 0
  done
  # Allowlist = legitimate permanent references. Baseline = pre-existing
  # internal dev-host debt, a ratchet: entries may be removed, never added.
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    p="${line%%#*}"        # drop inline comment
    p="${p//[[:space:]]/}" # drop surrounding whitespace
    p="${p%/}"             # accept a directory prefix with or without a trailing slash
    [[ -z "$p" ]] && continue
    if [[ "$path" == "$p" || "$path" == "$p"/* || "$as_root" == "$p" || "$as_root" == "$p"/* ]]; then
      return 0
    fi
  done < <(cat "$allow_file" "$base_file" 2>/dev/null || true)
  return 1
}

# The file list is explicit rather than `git grep -- .` so the scan can run over a
# directory that is not a work tree (the test fixtures). In a work tree the list
# is the index, which is what "tracked files" means for this gate.
files=()
if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  # Inside a work tree the list is the index -- what "tracked files" means for
  # this gate -- and `git -C DIR ls-files` prints paths relative to DIR, so a
  # subdirectory root scans only its own tracked files with matching paths.
  mapfile -t files < <(git -C "$ROOT" ls-files)
else
  mapfile -t files < <(cd "$ROOT" && find . -type f | sed 's|^\./||' | LC_ALL=C sort)
fi

raw=""
if ((${#files[@]})); then
  # -H: always prefix the path. grep omits it when a single file matches, and
  # the exemption and the report both split on that prefix (a one-file tree is
  # exactly what the test fixtures are). -I: skip binary files; -i: the whole
  # scan is case-insensitive; -E: the pattern is an alternation. A no-match grep
  # exits 1, which is not an error.
  raw=$(cd "$ROOT" && grep -H -n -I -i -E "$PATTERN" -- "${files[@]}" 2>/dev/null || true)
fi

violations=""
while IFS= read -r hit; do
  [[ -z "$hit" ]] && continue
  path="${hit%%:*}"
  if exempt "$path"; then
    continue
  fi
  violations+="${hit}"$'\n'
done <<<"$raw"

if [[ -n "$violations" ]]; then
  echo "::error::Internal hostnames / private-repo references found in tracked files:"
  printf '%s' "$violations"
  exit 1
fi
echo "Tree hygiene OK — no internal hostnames / private-repo references in tracked files."
