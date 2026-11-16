#!/usr/bin/env bash
# Verify the brand system: tokens are DTCG-conformant, every generated artefact
# matches its source, every contrast pair meets WCAG 2.2 AA, and every logo
# asset satisfies the grid contract in brand/logo/mark/DIRECTION.md.
#
# The work happens in brand/build.py --check, which fails on drift, on a
# contrast regression, and on a glyph that breaks stroke = gap = 1u. This wrapper
# exists so it is reachable from preflight/all.sh and from lefthook without any
# caller needing to know where the generator lives.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

if [ ! -f brand/build.py ]; then
  echo "brand: no brand/build.py — nothing to check"
  exit 0
fi

python3 brand/build.py --check
