#!/usr/bin/env bash
# Verify the brand system: tokens are DTCG-conformant, every generated artefact
# matches its source, every contrast pair meets WCAG 2.2 AA, every logo asset
# satisfies the grid contract in design-system/logo/mark/DIRECTION.md, and the generated
# Go package actually compiles.
#
# The work happens in design-system/build.py --check, which fails on drift, on a
# contrast regression, and on a glyph that breaks the mark's grid contract. This
# wrapper exists so it is reachable from preflight/all.sh and from lefthook
# without any caller needing to know where the generator lives.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

if [ ! -f design-system/build.py ]; then
  # Fail closed. "No generator" is only fine when there is no brand to check;
  # if the token source is present, a missing validator means the gate has been
  # deleted or renamed, and reporting PASS would silently disable drift,
  # contrast and grid enforcement for every future commit.
  if [ -d design-system/tokens ]; then
    echo "brand: design-system/tokens/ exists but design-system/build.py is missing — the gate cannot run" >&2
    exit 1
  fi
  echo "brand: no brand source — nothing to check"
  exit 0
fi

python3 design-system/build.py --check

# The generated Go file is committed and nothing else in the build compiles it
# (every other Go job is rooted in its own module), so a syntax error or an
# unused import would otherwise land silently. `go vet` needs the brand module,
# which is registered in go.work.
go vet ./design-system
