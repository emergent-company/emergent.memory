#!/usr/bin/env bash
# lint-swift.sh — SwiftFormat + SwiftLint coverage for the Mac connector.
#
# Scope is a *ratchet*: only the files passed as arguments, or (when none are
# passed) the Swift files changed on this branch relative to BASE (default:
# origin/main), are checked — always constrained to apps/connector.mac so an
# unrelated tree (e.g. apps/ios) is never linted with the connector's config.
# The connector carries pre-existing SwiftFormat and SwiftLint debt, so
# whole-tree enforcement would fail on untouched files; the ratchet mirrors
# golangci-lint's `--new-from-rev` usage elsewhere in this repo
# (`lefthook.yml`) and keeps *new* drift out without a mass reformat. Pay the
# debt down in a dedicated follow-up, then widen this to `git ls-files`.
#
# Usage:
#   Scripts/lint-swift.sh [file ...]      # lint the given Swift files
#   BASE=<ref> Scripts/lint-swift.sh      # lint files changed since <ref>
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

connector_dir="apps/connector.mac"

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

# Args (e.g. lefthook `{staged_files}`) are validated with the same pathspec
# scope, so a caller cannot widen the lint to another tree. Dedup: lefthook may
# hand the same file once per matching glob pattern.
swift_files=()
declare -A seen=()
for f in "${candidates[@]}"; do
  f="${f#./}"
  case "$f" in
    "${connector_dir}"/*.swift)
      [ -f "$f" ] || continue
      [ -n "${seen[$f]:-}" ] && continue
      seen[$f]=1
      swift_files+=("$f")
      ;;
  esac
done

if [ "${#swift_files[@]}" -eq 0 ]; then
  echo "lint-swift: no Swift files to check"
  exit 0
fi

echo "lint-swift: checking ${#swift_files[@]} file(s)"
printf '  %s\n' "${swift_files[@]}"

echo "— swiftformat --lint"
swiftformat --lint --config "${connector_dir}/.swiftformat" "${swift_files[@]}"

echo "— swiftlint"
swiftlint lint --strict --config "${connector_dir}/.swiftlint.yml" "${swift_files[@]}"
