#!/usr/bin/env bash
# lint-swift.sh — SwiftFormat + SwiftLint coverage for the iOS app.
#
# Scope is a *ratchet*, and it is hard-scoped to apps/ios: every candidate path
# is validated against apps/ios/**/*.swift before it reaches a linter, so an
# unrelated tree (e.g. apps/connector.mac) can never be linted with the iOS
# config even if it is staged or passed as an argument.
#
# The iOS app carries pre-existing SwiftFormat and SwiftLint debt, so whole-tree
# enforcement of both would fail on untouched files. The two tools ratchet
# differently:
#
#   * SwiftFormat is checked only over the files that changed on this branch /
#     in the working tree (or the explicit argument list). Touch a file and it
#     must be format-clean; untouched debt is left alone so this PR does not
#     land a mass reformat. See .swiftlint.baseline's sibling debt note below.
#   * SwiftLint runs over the WHOLE apps/ios tree with
#     --baseline apps/ios/.swiftlint.baseline. The baseline records the current
#     structural violations (long files/types/functions, cyclomatic
#     complexity, identifier/nesting/mechanical rules) so they are ignored,
#     while any NEW violation anywhere in apps/ios fails. Lower the baseline as
#     the debt is refactored — never raise it. Regenerate it (from the repo
#     root) after an intentional change to the remaining debt:
#
#       swiftlint lint --write-baseline apps/ios/.swiftlint.baseline \
#         --config apps/ios/.swiftlint.yml apps/ios
#
# Current recorded debt (see PR/commit): SwiftFormat 33/63 files require
# formatting; SwiftLint 36 violations (32 warnings, 4 errors) in 13 files.
#
# Usage:
#   Scripts/lint-swift.sh [file ...]      # lint the given Swift files
#   BASE=<ref> Scripts/lint-swift.sh      # SwiftFormat vs <ref> (default origin/main)
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

ios_dir="apps/ios"

if ! command -v swiftformat >/dev/null 2>&1 || ! command -v swiftlint >/dev/null 2>&1; then
  echo "swiftformat/swiftlint not installed, skipping iOS lint"
  echo "  install: brew install swiftformat swiftlint"
  echo "  (CI installs the Linux release binaries; see .github/workflows/ios.yml)"
  exit 0
fi

# Swift pathspecs, scoped to the iOS tree. `**/*.swift` covers nested files and
# `*.swift` the files directly under apps/ios; both are listed so neither shape
# is missed (`**` also matches zero directories, but being explicit is
# version-proof).
swift_pathspecs=(
  "${ios_dir}/**/*.swift"
  "${ios_dir}/*.swift"
)

# Build the candidate list. With explicit args (lefthook `{staged_files}`) those
# are the candidates; otherwise gather everything that changed relative to BASE
# plus uncommitted/untracked working-tree files, so the ratchet also catches new
# files before they are committed.
candidates=()
if [ "$#" -gt 0 ]; then
  candidates=("$@")
else
  base="${BASE:-origin/main}"
  # An unset/zero BASE (e.g. a brand-new branch push) is not a usable commit;
  # fall back to origin/main before giving up.
  if ! git rev-parse -q --verify "${base}^{commit}" >/dev/null 2>&1; then
    echo "lint-swift: BASE '$base' is not a known commit, falling back to origin/main"
    base="origin/main"
  fi
  if git rev-parse -q --verify "${base}^{commit}" >/dev/null 2>&1; then
    mapfile -t committed < <(git diff --name-only --diff-filter=ACMR "${base}...HEAD" -- "${swift_pathspecs[@]}" || true)
  else
    echo "lint-swift: no usable BASE ref, skipping SwiftFormat ratchet"
    committed=()
  fi
  mapfile -t unstaged < <(git diff --name-only --diff-filter=ACMR -- "${swift_pathspecs[@]}" || true)
  mapfile -t staged < <(git diff --cached --name-only --diff-filter=ACMR -- "${swift_pathspecs[@]}" || true)
  mapfile -t untracked < <(git ls-files --others --exclude-standard -- "${swift_pathspecs[@]}" || true)
  candidates=("${committed[@]}" "${unstaged[@]}" "${staged[@]}" "${untracked[@]}")
fi

# Resolved (physical) scope root — the canonical reference every candidate must
# live under. Resolving symlinks here means a symlinked or `..`-laden path
# cannot smuggle a file from another tree past the scope check.
ios_root="$(cd "${ios_dir}" && pwd -P)" || {
  echo "lint-swift: ${ios_dir} not found, skipping iOS lint"
  exit 0
}

# Physical absolute path for an existing file, following symlinks. Prefer
# realpath; fall back to resolving the containing directory so macOS/BSD without
# `realpath -m` still works (the file itself must exist for the caller).
resolve_path() {
  local resolved=""
  if command -v realpath >/dev/null 2>&1; then
    resolved="$(realpath -- "$1" 2>/dev/null || true)"
  fi
  if [ -z "$resolved" ]; then
    resolved="$(cd "$(dirname -- "$1")" && printf '%s/%s' "$(pwd -P)" "$(basename -- "$1")")"
  fi
  printf '%s' "$resolved"
}

# Validate args/candidates by RESOLVED path, not lexical prefix: `case` globs
# let `*` cross `/`, so `apps/ios/../connector.mac/Foo.swift` starts with
# `apps/ios/` lexically yet physically escapes the tree. Every candidate is
# canonicalized and compared against the resolved scope root; anything outside
# is refused (and, fail-closed, fails the run). Dedup is on the resolved path so
# lefthook handing the same file once per glob pattern lints it once.
swift_files=()
declare -A seen=()
refused=0
for f in "${candidates[@]}"; do
  f="${f#./}"

  # Reject any `..` path component outright (belt-and-braces with resolution).
  case "/$f/" in
    */../*)
      echo "lint-swift: refusing out-of-scope path (contains '..'): $f" >&2
      refused=$((refused + 1))
      continue
      ;;
  esac

  [ -f "$f" ] || continue

  resolved="$(resolve_path "$f")"
  case "$resolved" in
    "${ios_root}"/*.swift)
      ;;
    "${ios_root}"/*)
      # In-tree but not a Swift file: ignore (e.g. a non-Swift staged path).
      continue
      ;;
    *)
      echo "lint-swift: refusing out-of-scope path (resolves outside ${ios_dir}): $f -> $resolved" >&2
      refused=$((refused + 1))
      continue
      ;;
  esac

  [ -n "${seen[$resolved]:-}" ] && continue
  seen[$resolved]=1
  swift_files+=("$f")
done

if [ "$refused" -gt 0 ]; then
  echo "lint-swift: $refused path(s) outside ${ios_dir} refused; not linting" >&2
  exit 1
fi

# ── SwiftFormat — changed/new files only (untouched debt ignored) ────────────
if [ "${#swift_files[@]}" -eq 0 ]; then
  echo "lint-swift: no changed Swift files; skipping swiftformat"
else
  echo "lint-swift: swiftformat on ${#swift_files[@]} changed file(s)"
  swiftformat --lint --config "${ios_dir}/.swiftformat" "${swift_files[@]}"
fi

# ── SwiftLint — whole tree for the no-arg ratchet, explicit files otherwise ──
if [ "$#" -gt 0 ]; then
  lint_targets=("${swift_files[@]}")
  if [ "${#lint_targets[@]}" -eq 0 ]; then
    echo "lint-swift: no Swift files to lint"
    exit 0
  fi
else
  lint_targets=("${ios_dir}")
fi

echo "— swiftlint (baseline: ${ios_dir}/.swiftlint.baseline)"
swiftlint lint --strict --config "${ios_dir}/.swiftlint.yml" \
  --baseline "${ios_dir}/.swiftlint.baseline" \
  "${lint_targets[@]}"
