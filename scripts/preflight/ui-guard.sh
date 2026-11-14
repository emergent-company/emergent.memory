#!/usr/bin/env bash
# UI guardrails: the invariants the web-ui design system and the element-level
# feedback overlay rely on, enforced as a census over tracked sources.
#
# Why a preflight and not a rendering test: the Go suite for apps/web-ui runs on
# the promotion PR (web-ui.yml), so a regression introduced in a feature PR
# reaches `dev` and only fails a promotion later. These checks are textual, cost
# ~1s, and run in the branch-protection-required `ci` job through
# scripts/preflight/all.sh — the PR that introduces the regression is the one
# that fails, not the promotion a week later.
#
# What is checked and why each one is here:
#
#   1. No raw Tailwind palette classes (bg-red-500, text-gray-700, …). Colour is
#      a theme token or a component variant; a palette class bypasses the theme
#      and breaks in the dark surface. The tree is at 0, so this is a lock.
#   2. components/ holds no htmx request attributes (hx-get/post/put/delete/
#      patch). The components package is presentation-only — page templates own
#      the requests — which is what keeps a component renderable in a unit test
#      and reusable on a page that fetches differently. At 0, so a lock.
#   3. No UI fragment rendered on context.Background(). main.go's
#      componentAnnotations middleware flags every request as go-daisy dev mode,
#      which is what makes component roots emit data-component; a fragment
#      rendered outside a request silently loses that annotation and the overlay
#      falls back to a layout ancestor. A render outside a request has to say so
#      explicitly (devmode.WithDevMode(context.Background())).
#   4. data-testid values duplicated inside one templ file (budget).
#   5. Inline event handlers: onclick=, onchange=, … (budget).
#   6. Hardcoded #rrggbb colours in templ (budget).
#   7. style={…} attributes in templ (budget).
#   8. Raw <table> elements (budget).
#
# ── Baseline rule ────────────────────────────────────────────────────────────
# A budget may only fall. If a check counts *below* its baseline the script does
# not fail, but says so, because a stale baseline is headroom someone can spend
# later without noticing. Raising one requires the same justification any other
# ratchet needs: say in the PR why the new instance exists and what would remove
# it. Counts are occurrences, not lines, so two classes on one element count
# twice (check 1 only ever needs to be zero).
#
# ── Known gaps (deliberate) ──────────────────────────────────────────────────
#   - Check 4 is per file. It cannot tell a value rendered in two mutually
#     exclusive branches (the usual case in datasources.templ) from one rendered
#     twice on the same page; it catches the *new* duplicate, not the existing
#     ambiguity.
#   - Checks 6–8 are text scans: a colour assembled in Go (`typeColorStyle`) or a
#     table built from a component are invisible, as are palette classes spelled
#     through a variable.
#   - Check 3 matches the literal `context.Background()`. A background-derived
#     context passed through a variable is not caught.
#
# Run from anywhere; resolves the repo root itself.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

# ── Baselines ────────────────────────────────────────────────────────────────
# Recorded 2026-11-14 against dev @ abfda1693 while auditing the web-ui against
# the ui-patterns rules. Each is a count that may only fall.
BASELINE_DUP_TESTIDS=15
BASELINE_INLINE_HANDLERS=10
BASELINE_HEX_COLOURS=8
BASELINE_STYLE_ATTRS=12
BASELINE_RAW_TABLES=14

templ_files=()
while IFS= read -r f; do
  templ_files+=("$f")
done < <(git ls-files 'apps/web-ui/gateway/*.templ' | grep -v '/vendor/' || true)

go_files=()
while IFS= read -r f; do
  go_files+=("$f")
done < <(git ls-files 'apps/web-ui/gateway/*.go' | grep -v '/vendor/' | grep -v '_test\.go$' || true)

if [ "${#templ_files[@]}" -eq 0 ]; then
  echo "ui-guard: no tracked templ sources under apps/web-ui/gateway; nothing to check"
  exit 0
fi

fail=0

# lines <pattern> <files…> — "file:line: text" for every match.
lines() {
  local pattern="$1"
  shift
  grep -HnE "$pattern" "$@" 2>/dev/null || true
}

# occurrences <pattern> <files…> — number of matches, not lines.
occurrences() {
  local pattern="$1"
  shift
  grep -ohE "$pattern" "$@" 2>/dev/null | wc -l | tr -d ' '
}

# locked <label> <pattern> <hint> <files…> — the count must stay 0.
locked() {
  local label="$1" pattern="$2" hint="$3"
  shift 3
  local hits
  hits="$(lines "$pattern" "$@")"
  if [ -n "$hits" ]; then
    echo "ui-guard: FAIL $label (must stay 0)" >&2
    printf '%s\n' "$hits" | sed 's/^/  /' >&2
    echo "ui-guard:      $hint" >&2
    fail=$((fail + 1))
  else
    echo "ui-guard: ok   $label (0)"
  fi
}

# budgeted <label> <baseline> <pattern> <hint> <files…>
budgeted() {
  local label="$1" baseline="$2" pattern="$3" hint="$4"
  shift 4
  local n
  n="$(occurrences "$pattern" "$@")"
  if [ "$n" -gt "$baseline" ]; then
    echo "ui-guard: FAIL $label grew from $baseline to $n" >&2
    lines "$pattern" "$@" | sed 's/^/  /' >&2
    echo "ui-guard:      $hint" >&2
    echo "ui-guard:      if the new instance is justified, raise BASELINE with a reason in the PR" >&2
    fail=$((fail + 1))
  elif [ "$n" -lt "$baseline" ]; then
    echo "ui-guard: ok   $label $n <= $baseline (lower the baseline to $n)"
  else
    echo "ui-guard: ok   $label $n <= $baseline"
  fi
}

templ_files_count="${#templ_files[@]}"
echo "ui-guard: scanning $templ_files_count templ file(s), ${#go_files[@]} non-test Go file(s)"

# 1. Design tokens only — no raw palette class.
locked "raw Tailwind palette classes" \
  '\b(bg|text|border|from|to|ring)-(red|blue|green|gray|slate|zinc|stone|amber|yellow|indigo|purple|pink|cyan|teal|emerald|orange|rose|violet|fuchsia|lime|sky)-[0-9]{2,3}\b' \
  "use a theme token (bg-primary, text-base-content/55) or a component variant" \
  "${templ_files[@]}"

# 2. components/ is presentation-only: pages own the requests.
component_files=()
while IFS= read -r f; do
  component_files+=("$f")
done < <(printf '%s\n' "${templ_files[@]}" | grep '/components/' || true)
if [ "${#component_files[@]}" -gt 0 ]; then
  locked "htmx request attributes under gateway/components/" \
    'hx-(get|post|put|delete|patch)=' \
    "keep the request in the page template: components take data and an action component" \
    "${component_files[@]}"
fi

# 3. A UI render outside a request must be explicit about dev mode.
locked "UI renders on context.Background()" \
  '\.Render\(context\.Background\(\)' \
  "render on the request context, or devmode.WithDevMode(context.Background()) if there is none" \
  "${go_files[@]}"

# 4. data-testid duplicated inside one templ file.
dup_total=0
dup_report=""
for f in "${templ_files[@]}"; do
  dups="$(grep -ohE 'data-testid="[^"]+"' "$f" 2>/dev/null | sort | uniq -d || true)"
  [ -z "$dups" ] && continue
  n="$(printf '%s\n' "$dups" | wc -l | tr -d ' ')"
  dup_total=$((dup_total + n))
  dup_report="${dup_report}${n} ${f}"$'\n'
done
if [ "$dup_total" -gt "$BASELINE_DUP_TESTIDS" ]; then
  echo "ui-guard: FAIL data-testid duplicated inside one file grew from $BASELINE_DUP_TESTIDS to $dup_total" >&2
  printf '%s' "$dup_report" | sed 's/^/  /' >&2
  echo "ui-guard:      a duplicated id makes getByTestId ambiguous on that route" >&2
  fail=$((fail + 1))
elif [ "$dup_total" -lt "$BASELINE_DUP_TESTIDS" ]; then
  echo "ui-guard: ok   data-testid duplicates $dup_total <= $BASELINE_DUP_TESTIDS (lower the baseline to $dup_total)"
else
  echo "ui-guard: ok   data-testid duplicates $dup_total <= $BASELINE_DUP_TESTIDS"
fi

# 5–8. Budgets.
budgeted "inline event handlers" "$BASELINE_INLINE_HANDLERS" \
  ' (onclick|onchange|onsubmit|oninput|onload)=' \
  "prefer htmx or a delegated handler in the page's JS; inline handlers block a CSP without 'unsafe-inline'" \
  "${templ_files[@]}"

budgeted "hardcoded #rrggbb colours in templ" "$BASELINE_HEX_COLOURS" \
  '#[0-9a-fA-F]{6}\b' \
  "take the colour from a token, a variant, or the type-colour helper" \
  "${templ_files[@]}"

budgeted "style={…} attributes in templ" "$BASELINE_STYLE_ATTRS" \
  'style=\{' \
  "prefer a utility class or a component variant; inline style bypasses the theme" \
  "${templ_files[@]}"

budgeted "raw <table> elements" "$BASELINE_RAW_TABLES" \
  '<table' \
  "use components.TableCard, which owns the scroll container, header and responsive behaviour" \
  "${templ_files[@]}"

if [ "$fail" -ne 0 ]; then
  echo "ui-guard: FAILED ($fail check(s))" >&2
  exit 1
fi

echo "ui-guard: OK (palette-free, htmx-free components, request-context renders, budgets held)"
