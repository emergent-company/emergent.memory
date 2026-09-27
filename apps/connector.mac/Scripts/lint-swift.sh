#!/usr/bin/env bash
# lint-swift.sh — SwiftFormat + SwiftLint coverage for the Mac connector.
#
# Scope is a *ratchet*: only the files passed as arguments, or (when none are
# passed) the Swift files changed on this branch relative to BASE (default:
# origin/main), are checked. The connector carries pre-existing SwiftFormat and
# SwiftLint debt, so whole-tree enforcement would fail on untouched files; the
# ratchet mirrors golangci-lint's `--new-from-rev` usage elsewhere in this repo
# (`lefthook.yml`) and keeps *new* drift out without a mass reformat. Pay the
# debt down in a dedicated follow-up, then widen this to `git ls-files`.
#
# Usage:
#   Scripts/lint-swift.sh [file ...]      # lint the given Swift files
#   BASE=<ref> Scripts/lint-swift.sh      # lint files changed since <ref>
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

config_dir="apps/connector.mac"

if ! command -v swiftformat >/dev/null 2>&1 || ! command -v swiftlint >/dev/null 2>&1; then
  echo "swiftformat/swiftlint not installed, skipping Mac connector lint"
  echo "  install: brew install swiftformat swiftlint"
  echo "  (CI installs the Linux release binaries; see .github/workflows/connector.yml)"
  exit 0
fi

if [ "$#" -gt 0 ]; then
  files=("$@")
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
  mapfile -t files < <(git diff --name-only --diff-filter=ACMR "${base}...HEAD" -- '*.swift' || true)
fi

swift_files=()
for f in "${files[@]}"; do
  case "$f" in
    *.swift) [ -f "$f" ] && swift_files+=("$f") ;;
  esac
done

if [ "${#swift_files[@]}" -eq 0 ]; then
  echo "lint-swift: no Swift files to check"
  exit 0
fi

echo "lint-swift: checking ${#swift_files[@]} file(s)"
printf '  %s\n' "${swift_files[@]}"

echo "— swiftformat --lint"
swiftformat --lint --config "${config_dir}/.swiftformat" "${swift_files[@]}"

echo "— swiftlint"
swiftlint lint --strict --config "${config_dir}/.swiftlint.yml" "${swift_files[@]}"
