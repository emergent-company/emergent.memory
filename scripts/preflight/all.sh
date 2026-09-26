#!/usr/bin/env bash
# Run every preflight check; exit non-zero if any fails.
set -euo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(git rev-parse --show-toplevel)"
cd "$root"

status=0
for script in base-check.sh migration-number.sh gofmt-tracked.sh; do
  echo "==> $script"
  if bash "$dir/$script"; then
    echo "==> $script PASSED"
  else
    echo "==> $script FAILED"
    status=1
  fi
done

if [ "$status" -ne 0 ]; then
  echo "preflight: FAILED"
  exit 1
fi

echo "preflight: OK (base, migration numbering, gofmt)"
