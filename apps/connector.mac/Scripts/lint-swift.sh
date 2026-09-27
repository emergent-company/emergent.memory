#!/usr/bin/env bash
# lint-swift.sh — SwiftFormat + SwiftLint coverage for the Mac connector.
#
# Scope is a *ratchet*: only the files passed as arguments, or (when none are
# passed) the Swift files changed on this branch relative to BASE (default:
# origin/main), are checked — always constrained to apps/connector.mac so an
# unrelated tree (e.g. apps/ios) is never linted with the connector's config.
# The ratchet mirrors golangci-lint's `--new-from-rev` usage elsewhere in this
# repo (`lefthook.yml`) and keeps *new* drift out.
#
# Hard scope: every candidate path is canonicalised (symlinks and `..`
# resolved) and the RESOLVED path must live under the resolved
# apps/connector.mac root. Anything outside is refused with a non-zero exit
# and is never linted — a lexical prefix match alone is escapable because `*`
# crosses `/` (e.g. `apps/ios/../connector.mac/...`).
#
# SwiftFormat debt is fully paid down — the whole tree is format-clean, so any
# touched file must stay clean. SwiftLint still carries a small set of
# structural violations (long files/types/functions, high cyclomatic
# complexity) that need refactors, not formatting; those are recorded in
# `.swiftlint.baseline` and ignored, so only NEW violations fail. Lower that
# baseline as the remaining debt is refactored — never raise it. Regenerate it
# (from the repo root) after an intentional change to the remaining debt:
#   swiftlint lint --write-baseline apps/connector.mac/.swiftlint.baseline \
#     --config apps/connector.mac/.swiftlint.yml apps/connector.mac
#
# Usage:
#   Scripts/lint-swift.sh [file ...]      # lint the given Swift files
#   BASE=<ref> Scripts/lint-swift.sh      # lint files changed since <ref>
set -euo pipefail

repo_root="$(cd "$(git rev-parse --show-toplevel)" && pwd -P)"
cd "$repo_root"

connector_dir="apps/connector.mac"
# Resolved scope root — the single source of truth for the scope decision.
connector_root="$(cd "${connector_dir}" && pwd -P)"

# Canonicalise a candidate path to an absolute, symlink-resolved form. Rejects
# `..`/`.` components outright, then resolves with `realpath -m` when available
# (GNU / modern BSD), falling back to resolving the parent directory with
# `cd` + `pwd -P`. Prints the resolved path on success; non-zero when the path
# cannot be resolved. Callers must make the scope decision on this output.
canonical_path() {
  local path="$1" resolved dir base
  case "/${path}/" in
    *"/../"* | *"/./"*) return 1 ;;
  esac
  if command -v realpath >/dev/null 2>&1 && resolved="$(realpath -m "${path}" 2>/dev/null)"; then
    printf '%s\n' "${resolved}"
    return 0
  fi
  dir="$(dirname "${path}")"
  base="$(basename "${path}")"
  if resolved="$(cd "${dir}" 2>/dev/null && pwd -P)"; then
    printf '%s/%s\n' "${resolved}" "${base}"
    return 0
  fi
  return 1
}

if ! command -v swiftformat >/dev/null 2>&1 || ! command -v swiftlint >/dev/null 2>&1; then
  echo "swiftformat/swiftlint not installed, skipping Mac connector lint"
  echo "  install: brew install swiftformat swiftlint"
  echo "  (CI installs the Linux release binaries; see .github/workflows/connector.yml)"
  exit 0
fi

# Swift pathspecs, scoped to the connector tree. `**/*.swift` covers nested
# files and `*.swift` the files directly under the connector root; both are
# listed so neither shape is missed (`**` also matches zero directories, but
# being explicit is version-proof).
swift_pathspecs=(
  "${connector_dir}/**/*.swift"
  "${connector_dir}/*.swift"
)

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
  if ! git rev-parse -q --verify "${base}^{commit}" >/dev/null 2>&1; then
    echo "lint-swift: no usable BASE ref, skipping"
    exit 0
  fi
  mapfile -t candidates < <(git diff --name-only --diff-filter=ACMR "${base}...HEAD" -- "${swift_pathspecs[@]}" || true)
fi

# Scope is decided on the CANONICAL path (symlinks + `..` resolved), never the
# raw string: a lexical `case` prefix match is escapable because `*` crosses
# `/`. Anything resolving outside the connector root is refused (fail closed)
# and never linted. Applies to explicit args AND `{staged_files}`/BASE alike.
# Dedup: lefthook may hand the same file once per matching glob pattern, and
# different spellings can resolve to one file.
swift_files=()
declare -A seen=()
refused=0
for f in "${candidates[@]}"; do
  f="${f#./}"
  if ! abs="$(canonical_path "$f")"; then
    echo "lint-swift: REFUSED (unresolvable/unsafe path): ${f}" >&2
    refused=1
    continue
  fi
  case "$abs" in
    "${connector_root}"/*.swift) ;;
    *)
      echo "lint-swift: REFUSED (outside ${connector_dir}): ${f}" >&2
      refused=1
      continue
      ;;
  esac
  [ -f "$abs" ] || continue
  # Lint with a repo-root-relative path so SwiftLint's --baseline (whose
  # entries are repo-root-relative) keeps matching.
  rel="${abs#"${repo_root}/"}"
  [ -n "${seen[$rel]:-}" ] && continue
  seen[$rel]=1
  swift_files+=("$rel")
done

if [ "$refused" -ne 0 ]; then
  echo "lint-swift: refusing to continue with out-of-scope arguments" >&2
  exit 2
fi

if [ "${#swift_files[@]}" -eq 0 ]; then
  echo "lint-swift: no Swift files to check"
  exit 0
fi

echo "lint-swift: checking ${#swift_files[@]} file(s)"
printf '  %s\n' "${swift_files[@]}"

echo "— swiftformat --lint"
swiftformat --lint --config "${connector_dir}/.swiftformat" "${swift_files[@]}"

echo "— swiftlint"
swiftlint lint --strict --config "${connector_dir}/.swiftlint.yml" \
  --baseline "${connector_dir}/.swiftlint.baseline" \
  "${swift_files[@]}"
