#!/usr/bin/env bash
# Hermetic test for design-system.sh and the gate it fronts (#1071).
#
# WHY. design-system.sh owns real policy — it is the only thing that can fail a
# commit for a stale brand artefact, a contrast regression, a broken mark
# contract or a page that points at an asset which moved — and every other
# guard that owns policy this size carries its own suite (base-check.test.sh,
# go-decl-collision.test.sh, release-tag.test.sh, version-drift.test.sh). This
# one is where the wrapper's three branches are pinned, plus the three rules
# that used to be published and never asserted:
#
#   * fail-closed: design-system/tokens/ present but build.py missing is the
#     "the gate has been deleted or renamed" case. preflight/all.sh hit it for
#     real during #1069, when the brand.sh -> design-system.sh rename left a
#     dangling entry.
#   * no brand source: nothing to check is a PASS, not a silent failure.
#   * genuine drift: one byte changed in a generated file.
#   * logo.mark.clearSpace must equal side / 4 — it is published in the token
#     file, logo/manifest.json and the tile README, and used to pass at any
#     value because nothing compared it to the side.
#   * every src=/href=/url() in a design-system page must resolve on disk.
#     1069's merge blocker was preview.html pointing at mark/*.svg after the
#     files moved; six broken images, found by a human.
#
# Each guard case is a throwaway git repository holding a copy of the shipped
# design-system/ tree, mutated once. Every case exits before the wrapper's
# `go vet`, so the suite needs no Go toolchain and no network.
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)
script="$here/design-system.sh"
all_sh="$here/all.sh"

for cmd in git python3 grep; do
  command -v "$cmd" >/dev/null 2>&1 || {
    printf 'FAIL [design-system]: %s is required\n' "$cmd" >&2
    exit 2
  }
done

failures=0
checks=0
ok() {
  checks=$((checks + 1))
  printf 'ok   [design-system]: %s\n' "$1"
}
no() {
  checks=$((checks + 1))
  failures=$((failures + 1))
  printf 'FAIL [design-system]: %s\n' "$1" >&2
  if [ -n "${out:-}" ]; then
    printf '%s\n' "$out" | sed 's/^/    /' >&2
  fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
work="$tmp/repo"

# fresh: a git repository whose design-system/ is exactly the shipped tree. The
# wrapper resolves the repository root with `git rev-parse`, so every case has
# to be a repository for the same reason a caller would be.
fresh() {
  rm -rf "$work"
  mkdir -p "$work"
  git -C "$work" init -q -b dev
  cp -a "$root/design-system" "$work/design-system"
}

out=""
status=0
run_guard() {
  status=0
  out=$(cd "$work" && bash "$script" 2>&1) || status=$?
}
run_build() {
  status=0
  out=$(cd "$work" && python3 design-system/build.py --check 2>&1) || status=$?
}

expect_status() {
  if ((status == $1)); then
    ok "$2"
  else
    no "$2 (exit ${status}, expected $1)"
  fi
}

expect_contains() {
  if grep -Fq -- "$2" <<<"$out"; then
    ok "$1"
  else
    no "$1 (output does not contain: $2)"
  fi
}

# ---------------------------------------------------------------------------
# 1. Fail closed: the token source is there, the gate is not.
# ---------------------------------------------------------------------------
fresh
rm "$work/design-system/build.py"
run_guard
expect_status 1 "tokens without a generator fail closed"
expect_contains "the failure names the missing gate" "build.py is missing"
expect_contains "the failure says what it could not run" "the gate cannot run"

# ---------------------------------------------------------------------------
# 2. No brand source at all: nothing to check, and that is a pass.
# ---------------------------------------------------------------------------
fresh
rm -rf "$work/design-system"
run_guard
expect_status 0 "a checkout with no brand source passes"
expect_contains "the pass says what it skipped" "no brand source"

# ---------------------------------------------------------------------------
# 3. Genuine drift: one byte changed in a generated file. `--check`'s negative
#    path otherwise only runs when the live token values happen to be wrong.
# ---------------------------------------------------------------------------
fresh
printf '\n' >>"$work/design-system/dist/css/brand.tokens.css"
run_guard
expect_status 1 "one extra byte in a generated file fails the gate"
expect_contains "the drift is named" "stale generated file"
expect_contains "the drift names the file" "brand.tokens.css"

# ---------------------------------------------------------------------------
# 4. logo.mark.clearSpace is a rule, not decoration: it is published in the
#    token file, logo/manifest.json and the tile README, and 999 used to pass
#    because the generator only rendered it.
# ---------------------------------------------------------------------------
fresh
python3 - "$work/design-system/tokens/memory.tokens.json" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path) as handle:
    doc = json.load(handle)
doc["logo"]["mark"]["clearSpace"]["$value"] = 999
with open(path, "w") as handle:
    json.dump(doc, handle, indent=2)
    handle.write("\n")
PY
run_guard
expect_status 1 "clearSpace = 999 fails the gate"
expect_contains "the failure quotes the value" "clearSpace = 999"
expect_contains "the failure names the rule" "expected side/4 = 16"

# ---------------------------------------------------------------------------
# 5. A page that points at an asset which moved. This is #1069's merge blocker
#    reproduced exactly: preview.html lived beside the tiles and still asked for
#    them under mark/.
# ---------------------------------------------------------------------------
fresh
python3 - "$work/design-system/logo/mark/preview.html" <<'PY'
import sys

path = sys.argv[1]
with open(path) as handle:
    text = handle.read()
assert 'src="mark-v1-tile.svg"' in text, "preview.html no longer references the tile it previews"
with open(path, "w") as handle:
    handle.write(text.replace('src="mark-v1-tile.svg"', 'src="mark/mark-v1-tile.svg"'))
PY
run_guard
expect_status 1 "a page pointing at a moved asset fails the gate"
expect_contains "the broken reference is named" "page asset"
expect_contains "the page is named" "logo/mark/preview.html"
expect_contains "the reference itself is quoted" "mark/mark-v1-tile.svg"

# ---------------------------------------------------------------------------
# 6. No false positives: the shipped tree passes its own gate. Anchors
#    (href="#colour") and SVG paint servers (url(#plate4)) are not assets, and
#    the catalogue is full of both.
# ---------------------------------------------------------------------------
fresh
run_build
expect_status 0 "the shipped tree passes --check"
expect_contains "the pass reports what it verified" "generated files match"

# ---------------------------------------------------------------------------
# 7. The lattice glyph is one path, never abutting rects. A rectangle
#    decomposition cannot draw an L-shaped stroke without two rects meeting
#    along an edge, and a shared edge is where a hairline seam shows at 200px.
# ---------------------------------------------------------------------------
if grep -q "<rect" "$root/design-system/logo/mark/mark-v1.svg"; then
  out=$(grep -o "<rect[^>]*>" "$root/design-system/logo/mark/mark-v1.svg")
  no "mark-v1.svg draws the glyph as a path, not as rects"
else
  ok "mark-v1.svg draws the glyph as a path, not as rects"
fi
paths=$(grep -o "<path" "$root/design-system/logo/mark/mark-v1.svg" | wc -l | tr -d ' ')
if [ "$paths" = 1 ]; then
  ok "mark-v1.svg is one path"
else
  no "mark-v1.svg is one path (found $paths)"
fi

# ---------------------------------------------------------------------------
# 8. The path has to paint the letter. Check every glyph's outline against its
#    ASCII cells at even-odd fill, including an L-shaped stroke (the shape a
#    rect decomposition cannot draw seam-free) and the M's counter.
# ---------------------------------------------------------------------------
out=""
if python3 - "$root/design-system/build.py" <<'PY'
import importlib.util
import sys

spec = importlib.util.spec_from_file_location("brand_build", sys.argv[1])
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)

failures = 0


def check(label, cells, width, height):
    global failures
    loops = build.glyph_outline(cells)
    if not loops:
        print(f"{label}: traced no outline", file=sys.stderr)
        failures += 1
        return
    for y in range(height):
        for x in range(width):
            painted = build.outline_paints(loops, x + 0.5, y + 0.5)
            if painted != ((x, y) in cells):
                print(f"{label}: outline disagrees about cell {x},{y}", file=sys.stderr)
                failures += 1


for name, art in build.GLYPH_SAMPLES.items():
    rows = art.strip("\n").split("\n")
    cells = {
        (x, y) for y, row in enumerate(rows) for x, ch in enumerate(row) if ch == "#"
    }
    check(name, cells, len(rows[0]), len(rows))

# An L-shaped stroke, and the M's counter: the two shapes that broke the rect
# decomposition (abutting rects) and that only even-odd fill can draw.
check("L", {(0, 0), (1, 0), (0, 1)}, 2, 2)
check("ring", {(x, y) for x in range(3) for y in range(3)} - {(1, 1)}, 3, 3)

sys.exit(1 if failures else 0)
PY
then
  ok "every glyph outline paints exactly its ASCII cells"
else
  no "every glyph outline paints exactly its ASCII cells"
fi

# ---------------------------------------------------------------------------
# 9. Wiring: the suite must run where the required `ci` job can see it, and
#    both scripts must parse.
# ---------------------------------------------------------------------------
if grep -Fq "design-system.test.sh" "$all_sh"; then
  ok "all.sh runs this suite"
else
  no "all.sh runs this suite"
fi
if bash -n "$script"; then
  ok "design-system.sh parses"
else
  no "design-system.sh parses"
fi
if bash -n "${BASH_SOURCE[0]}"; then
  ok "design-system.test.sh parses"
else
  no "design-system.test.sh parses"
fi

if ((checks > 0)); then
  printf '\n'
fi
if ((failures > 0)); then
  printf 'FAIL [design-system]: %d of %d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'ok [design-system]: %d checks passed\n' "$checks"
