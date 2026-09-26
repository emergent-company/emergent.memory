#!/usr/bin/env bash
# Fail when any tracked .go file is not gofmt-clean.
#
# Over *tracked* files (`git ls-files`), so generated/untracked artifacts do
# not produce noise. Mirrors the Meta CI check (see emergent-company/emergent.memory#1058).
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

count="$(git ls-files '*.go' | wc -l | tr -d ' ')"
echo "gofmt-tracked: checked $count tracked .go files"

unformatted="$(git ls-files '*.go' | xargs gofmt -l)"

if [ -n "$unformatted" ]; then
  echo "gofmt-tracked: FAIL unformatted tracked Go files:" >&2
  printf '%s\n' "$unformatted" >&2
  echo "gofmt-tracked: fix with: gofmt -w <file>" >&2
  exit 1
fi

echo "gofmt-tracked: OK (all tracked Go files are gofmt-clean)"
