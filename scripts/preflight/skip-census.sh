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
#
# Raised 2026-10-14 (337→338, 335→336): commit bdcbb7693 (#1793) added three
# live-probe skips to apps/server/domain/mcpregistry/oauth_live_test.go:
#   - :97  TestFigmaRegistrationRejected_Live — gate on MCPREGISTRY_LIVE_OAUTH_PROBE=1
#   - :111 TestFigmaRegistrationRejected_Live — defensive guard on metadata drift
#   - :133 TestLinearDiscoveryDCRAuthorize_Live — gate on MCPREGISTRY_LIVE_OAUTH_PROBE=1
# The :111 guard fires only when Figma stops advertising a registration endpoint
# (req.RegistrationEndpoint == ""); the live rejection assertion that follows it
# (:116-121) is real, executing coverage — Figma advertises a registration
# endpoint but rejects dynamic client registration with 403 (archived change
# 2026-10-13-add-mcp-oauth-connect, design.md:131). Each skip gates a live OAuth
# probe against a third-party endpoint (Figma/Linear) behind an opt-in env var so
# CI never depends on remote metadata. The baseline carried 2 skips of headroom
# (live tree 335/333 vs baseline 337/335), so these 3 sites overflowed it by
# exactly 1. No issue tracks these skips (a deliberate CI-hygiene gate, not debt
# to be paid down), so BASELINE_NO_REF moves with BASELINE_SKIPS.
#
# Lowered 2026-10-14 (338→334, 336→331): the four opt-in env gates in
# oauth_live_test.go now route through one non-test helper
# (skipUnlessLiveOAuthProbe), which the census excludes by rule, and the
# remaining drift guard carries a tracking reference (#1861). Net 4 fewer
# counted sites and 5 fewer unreferenced sites; the baseline is tightened to
# match so the headroom cannot be spent silently.
#
# Raised 2026-10-31 (334→335): #2231 adds two database-backed regression tests to
# apps/server/domain/graph/branch_db_test.go (fork description; unknown merge
# policy rejected before the merge reads anything). Both carry the same
# `if testing.Short() { t.Skip(...) }` gate as the three sites already in that
# file and the ~200 across the tree — short-mode gating is the dominant idiom
# here and no helper is used for it (the 2026-10-14 lowering above was for opt-in
# env-var gates, a different shape). Both messages name (#2231), so
# BASELINE_NO_REF does not move. The live tree was 333 against a baseline of 334,
# so the two new sites land at 335 and this is the count, not headroom.
#
# Raised 2026-11-01 (335→336): #2383 adds one database-backed test to
# apps/server/domain/schemas/schemas_list_db_test.go (deprecated schemas hidden
# by default, returned on request, and the flag composing with search). It takes
# the same `if testing.Short() { t.Skip(...) }` gate as the two tests already in
# that file, which is the prevailing idiom here by a wide margin, so no helper is
# introduced for it. The message names (#2383), so BASELINE_NO_REF does not move.
# This is the count, not headroom.
#
# Raised 2026-11-07 (336→337): #24.1 adds one database-backed test to
# apps/server/domain/schemas/schemas_list_db_test.go (the install picker must not
# offer a retired pack, while a pack already installed stays visible). It takes
# the same `if testing.Short() { t.Skip(...) }` gate as the three tests already
# in that file, which is the prevailing idiom here by a wide margin, so no
# helper is introduced for it. The message names (#24.1), so BASELINE_NO_REF
# does not move. This is the count, not headroom.
# Raised 2026-11-12 (337→338, 331→330): #624 adds one database-backed test to
# apps/server/domain/graph/reindex_embeddings_chunks_db_test.go (the chunk arm of
# ReindexEmbeddings: parent chunks excluded, NULL-only by default, no stacking on
# an active job, --chunks opt-in). It takes the same `if testing.Short()
# { t.Skip(...) }` gate every other database test in that package uses, so no
# helper is introduced for it. The message names (#624), which is why
# BASELINE_NO_REF moves *down* to 330 rather than up: the new site is
# ref-bearing and the live tree had no headroom in either direction. This is the
# count, not headroom.
BASELINE_SKIPS=338
BASELINE_NO_REF=330

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
