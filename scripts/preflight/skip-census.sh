#!/usr/bin/env bash
# Skip-census ratchet: make test skips visible and stop the total growing.
#
# A `t.Skip` is a loan against future coverage. This repo has repeatedly lost
# real coverage to silent skips (#911 the integration suite never ran, #969 a
# regression reported as a skip, #933 seventeen permanently-skipped tests), and
# nothing made the total visible or stopped it growing. This gate:
#
#   1. Counts skip *call sites* per package across the Go tree.
#   2. Fails when the total grows past the checked-in baseline, so adding a skip
#      becomes a deliberate, reviewed act.
#   3. Fails when the count of skip call sites whose message carries no issue
#      reference (`#NNN`) grows past its own checked-in baseline, so a *new* skip
#      has to name why it exists and what tracks it (a warning nobody acts on is
#      decoration, not a gate).
#
# ── Counting rule (the exact line drawn) ─────────────────────────────────────
# Counted: a direct `.Skip(` / `.Skipf(` invocation that appears lexically inside
# a test function body — a function whose declared name starts with
# `Test` / `Benchmark` / `Fuzz` / `Example` (including testify methods such as
# `func (s *Suite) TestFoo()`). Anonymous closures inherit their enclosing
# function, so a `t.Skip` inside a `t.Run` closure still counts.
#
# Excluded (helper *definitions*, which merely propagate a skip):
#   - apps/server/internal/testdb/gate.go         SkipOrFatal
#   - apps/server/internal/testutil/suite.go      SkipInExternalMode, SkipIfExternalServer
#   - tools/opencode-test-suite/internal/harness  SkipIfServerDown
#   - every `skipIf*` / `require*` / `skipWithout*` helper in *_test.go files
#   These bodies contain a `t.Skipf`/`t.Skip` but are not themselves a test; the
#   skip decision happens at their call site, so counting the body would double
#   count. This is done mechanically: a `.Skip`/`.Skipf` is counted only when the
#   enclosing named function is a test function (rule above), which excludes
#   every helper regardless of how it is named.
#
# Known gaps (deliberate, documented):
#   - A skip invoked *through a helper* (e.g. `SkipOrFatal(t, ...)` or
#     `skipIfServerDown(t, rl)`) is NOT counted: the `.Skip` lives in the helper
#     body (excluded) and the call site has no `.Skip` literal. The census is
#     therefore a lower bound on skip decisions; it catches the new bare
#     `t.Skip("later")` this ratchet exists to stop.
#   - The issue-reference check inspects only the source line carrying the skip;
#     a `#NNN` on another line (multi-line string, `const` message) is missed.
#
# Run from anywhere; resolves the repo root itself. Wired into CI via
# scripts/preflight/all.sh (the branch-protection-required `ci` job). Runtime is
# a single awk pass over tracked *_test.go files (~1s).
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

# ── Baseline ─────────────────────────────────────────────────────────────────
# Skip call sites recorded 2026-09-26 (see #1088). Lower either number as debt
# is paid down — never raise one to make CI green. Raising requires an explicit
# justification in the PR body.
BASELINE_SKIPS=337
BASELINE_NO_REF=335

mapfile -t files < <(git ls-files '*_test.go')
if [ "${#files[@]}" -eq 0 ]; then
  echo "skip-census: no tracked *_test.go files; nothing to census"
  exit 0
fi

# awk emits:
#   SITE <file>:<line>: <trimmed text>          one per no-ref skip site
#   PKG <count> <dir>                           per-package skip totals
#   NOREFPKG <count> <dir>                      per-package no-ref totals
#   TOTAL <count>
#   NOREFTOTAL <count>
report="$(
  awk '
    function reldir(f,  n, a, i, s) {
      n = split(f, a, "/")
      s = ""
      for (i = 1; i < n; i++) s = s (i > 1 ? "/" : "") a[i]
      return s
    }
    FNR == 1 { pkg = reldir(FILENAME); sub(/^\.\//, "", pkg) }
    {
      line = $0
      # A function declaration begins a (possibly method / closure) body. Extract
      # the declared name for named funcs/methods; anonymous closures inherit the
      # enclosing function.
      if (line ~ /^[[:space:]]*func[[:space:]]/) {
        rest = line
        sub(/^[[:space:]]*func[[:space:]]+/, "", rest)
        if (rest ~ /^\(/) {
          if (match(rest, /\)[[:space:]]*/)) rest = substr(rest, RSTART + RLENGTH)
          else rest = ""
        }
        fn = ""
        if (rest != "" && match(rest, /^[A-Za-z_][A-Za-z0-9_]*/)) {
          cand = substr(rest, RSTART, RLENGTH)
          after = substr(rest, RSTART + RLENGTH)
          if (after ~ /^[[:space:]]*\(/) fn = cand
        }
        if (fn != "") {
          curfunc = fn
          is_test = (fn ~ /^(Test|Benchmark|Fuzz|Example)/) ? 1 : 0
        }
      }
      cnt = gsub(/\.(Skip|Skipf)\(/, "&", line)
      if (cnt > 0 && is_test) {
        total += cnt
        cnt_pkg[pkg] += cnt
        if (line !~ /#[0-9]/) {
          no_ref += cnt
          no_ref_pkg[pkg] += cnt
          sub(/^[[:space:]]+/, "", $0)
          printf "SITE %s:%d: %s\n", FILENAME, FNR, $0
        }
      }
    }
    END {
      for (p in cnt_pkg) print "PKG", cnt_pkg[p], p
      for (p in no_ref_pkg) print "NOREFPKG", no_ref_pkg[p], p
      print "TOTAL", total + 0
      print "NOREFTOTAL", no_ref + 0
    }
  ' "${files[@]}"
)"

total="$(printf '%s\n' "$report" | awk '$1 == "TOTAL" { print $2 }')"
no_ref="$(printf '%s\n' "$report" | awk '$1 == "NOREFTOTAL" { print $2 }')"

echo "skip-census: skip call sites by package"
printf '%s\n' "$report" | awk '$1 == "PKG" { printf "  %4d  %s\n", $2, $3 }' | sort -k1 -rn

fail=0
if [ "$total" -gt "$BASELINE_SKIPS" ]; then
  echo "skip-census: FAIL skip count grew from $BASELINE_SKIPS to $total"
  echo "skip-census:       a new t.Skip/t.Skipf was added. Remove it, or — if it is"
  echo "skip-census:       genuinely unavoidable — justify raising the baseline in the PR."
  fail=1
else
  echo "skip-census: ok skip count $total <= baseline $BASELINE_SKIPS"
fi

if [ "$no_ref" -gt "$BASELINE_NO_REF" ]; then
  echo "skip-census: FAIL no-ref skip count grew from $BASELINE_NO_REF to $no_ref"
  echo "skip-census:       a new t.Skip/t.Skipf was added without a #NNN issue reference."
  echo "skip-census:       Add the tracking issue number, or — if unreferenced is genuinely"
  echo "skip-census:       unavoidable — justify raising the baseline in the PR."
  fail=1
else
  echo "skip-census: ok no-ref skip count $no_ref <= baseline $BASELINE_NO_REF"
fi

if [ "$no_ref" -gt 0 ]; then
  echo "skip-census: $no_ref skip call site(s) carry no issue reference (#NNN)"
  echo "skip-census:       per package:"
  printf '%s\n' "$report" | awk '$1 == "NOREFPKG" { printf "  %4d  %s\n", $2, $3 }' | sort -k1 -rn
  echo "skip-census:       sites:"
  printf '%s\n' "$report" | awk '$1 == "SITE" { sub(/^SITE /, ""); print "    " $0 }'
  echo "skip-census:       each skip should name why it exists and what tracks it."
fi

exit "$fail"
