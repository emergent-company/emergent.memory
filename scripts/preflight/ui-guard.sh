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
#   1. No raw Tailwind palette class (bg-red-500, text-gray-700, bg-white, …).
#      Colour is a theme token or a component variant; a palette class bypasses
#      the theme and breaks in the dark surface. Every colour utility prefix the
#      product could use is covered (bg/text/border/from/via/to/ring/ring-offset/
#      divide/fill/stroke/accent/caret/placeholder/decoration/outline/shadow),
#      and black/white count as palette values. daisyUI theme tokens are not
#      palette values: `bg-neutral`, `text-neutral-content`, `bg-primary` pass,
#      while Tailwind's neutral scale (`bg-neutral-500`) does not. A handful of
#      deliberate uses are allowlisted BY VALUE below — a modal scrim is
#      genuinely black, and a QR code's quiet zone is genuinely white, so neither
#      has a theme token. The allowlist is a ratchet: an entry is removed when the
#      site goes, and a new entry needs the same justification a raised baseline
#      needs.
#   2. components/ holds no htmx request attributes (hx-get/post/put/delete/
#      patch). The components package is presentation-only — page templates own
#      the requests — which is what keeps a component renderable in a unit test
#      and reusable on a page that fetches differently. At 0, so a lock.
#   3. No UI fragment rendered on context.Background(). main.go's
#      componentAnnotations middleware flags every request as go-daisy dev mode,
#      which is what makes component roots emit data-component; a fragment
#      rendered outside a request silently loses that annotation and the overlay
#      falls back to a layout ancestor. A render outside a request has to say so
#      explicitly (devmode.WithDevMode(context.Background())). Scanned in .go and
#      .templ files alike: a templ file's Go block can render too.
#   4. data-testid values duplicated inside one templ file (budget). Counted as
#      *excess copies* — a value appearing three times counts 2, so adding a
#      third copy of an id that is already twice is not free.
#   5. Inline event handlers: onclick=, onchange=, onsubmit=, … (budget).
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
# A check must also survive its own zero: counting is done so that "no matches"
# is a count of 0, never a grep exit status that `set -e` turns into a silent
# failure (a budget reaching 0 is the *goal*, not an error).
#
# ── Known gaps (deliberate) ──────────────────────────────────────────────────
#   - Check 1 is a text scan: a colour assembled in Go (typeColorStyle) or a
#     palette class spelled through a variable is invisible. The allowlist is by
#     value, so an allowlisted class used on a *new* site is also invisible.
#   - Check 4 is per file. It cannot tell a value rendered in two mutually
#     exclusive branches (the usual case in datasources.templ) from one rendered
#     twice on the same page; it catches the *new* duplicate, not the existing
#     ambiguity.
#   - Checks 6–8 are text scans; a table built from a component is invisible.
#   - Check 3 matches the literal `context.Background()`. A background-derived
#     context passed through a variable, or a helper that renders internally
#     (approvals.templ reaches a render through renderProposalHTML), is not
#     caught.
#
# Run from anywhere; resolves the repo root from the script's own location (not
# from `git rev-parse --show-toplevel`, which answers the hook's cwd when a git
# hook exports GIT_DIR/GIT_WORK_TREE — that made every count silently 0 the first
# time this ran from pre-commit).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

# ── Baselines ────────────────────────────────────────────────────────────────
# Recorded 2026-11-14 against dev @ abfda1693 while auditing the web-ui against
# the ui-patterns rules. Each is a count that may only fall.
BASELINE_DUP_TESTIDS=14  # tightened from 15: #865 retired board.templ, whose two board notices were the duplicate copy this baseline carried
BASELINE_INLINE_HANDLERS=19
BASELINE_HEX_COLOURS=8
BASELINE_STYLE_ATTRS=14  # +2: components.BadgeWithClass reproduces ui.Badge's two `style={ accentStyle(props.Color) }` branches (span + <a href>) so the wrapper stays parity-identical to upstream; remove both when go-daisy #150 adds BadgeProps.Class (#855)
BASELINE_RAW_TABLES=14
#
# INLINE_HANDLERS was 10 in the first cut of this script and is 19 here. That is
# a correction, not new debt: the first regex required a literal space before the
# attribute, so the 9 handlers written on their own (tab-indented) line were
# never counted. The count is now every `on<event>=` attribute, whatever its
# indentation.

# ── Palette allowlist (check 1) ──────────────────────────────────────────────
# Value-pinned, for the sites where a raw palette value is the correct answer:
#   bg-black/40, bg-black/45  modal / chat-rail / sidepanel scrims — a scrim is
#                             black whatever the theme is.
#   bg-white                  a QR code's quiet zone must be white to scan, in
#                             both themes.
#   text-white                the glyph on top of that black scrim.
PALETTE_ALLOWLIST=(bg-black/40 bg-black/45 bg-white text-white)

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

# Never report a zero you did not measure. Every check below counts grep matches
# over these paths, and grep's "No such file" is swallowed by `2>/dev/null` — so a
# wrong working directory (a git hook can set GIT_DIR/GIT_WORK_TREE such that
# `git rev-parse --show-toplevel` answers the hook's cwd instead of the repo
# root) would print a clean bill of health for files that were never opened.
if [ ! -f "${templ_files[0]}" ]; then
  echo "ui-guard: FAIL cannot read ${templ_files[0]} from $root — refusing to report a zero it did not measure" >&2
  exit 2
fi

fail=0

# lines <pattern> <files…> — "file:line: text" for every match.
lines() {
  local pattern="$1"
  shift
  grep -HnE "$pattern" "$@" 2>/dev/null || true
}

# occurrences <pattern> <files…> — number of matches, not lines. The `|| true`
# inside the pipeline is load-bearing: without it, zero matches makes grep exit
# 1, pipefail propagates it, and `set -e` kills the script with no message at
# exactly the moment a ratchet reaches its goal.
occurrences() {
  local pattern="$1"
  shift
  { grep -ohE "$pattern" "$@" 2>/dev/null || true; } | wc -l | tr -d ' '
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

# 1. Design tokens only — no raw palette class, minus the pinned allowlist.
# Colour utilities whose colour comes from the palette rather than the theme.
# `neutral` is deliberately NOT a family here: bare `bg-neutral` / `text-neutral` /
# `text-neutral-content` are daisyUI THEME tokens (the correct choice), while
# Tailwind's neutral *scale* (`bg-neutral-500`) is a palette value — so the second
# alternative requires a shade.
PALETTE_PREFIX='bg|text|border|from|via|to|ring|ring-offset|divide|fill|stroke|accent|caret|placeholder|decoration|outline|shadow'
PALETTE_FAMILY='red|blue|green|gray|grey|slate|zinc|stone|amber|yellow|indigo|purple|pink|cyan|teal|emerald|orange|rose|violet|fuchsia|lime|sky|black|white'
PALETTE_RE="\\b(${PALETTE_PREFIX})-(${PALETTE_FAMILY})(-[0-9]{1,3})?(/[0-9]{1,3})?|\\b(${PALETTE_PREFIX})-neutral-[0-9]{1,3}(/[0-9]{1,3})?"

palette_hits=""
palette_allowed=0
for f in "${templ_files[@]}"; do
  while IFS= read -r hit; do
    [ -z "$hit" ] && continue
    tok="${hit##*:}"
    allowed=0
    for a in "${PALETTE_ALLOWLIST[@]}"; do
      if [ "$tok" = "$a" ]; then
        allowed=1
        break
      fi
    done
    if [ "$allowed" -eq 1 ]; then
      palette_allowed=$((palette_allowed + 1))
    else
      palette_hits="${palette_hits}${hit}"$'\n'
    fi
  done < <(grep -HnoE "$PALETTE_RE" "$f" 2>/dev/null || true)
done

if [ -n "$palette_hits" ]; then
  echo "ui-guard: FAIL raw Tailwind palette classes (must stay 0)" >&2
  printf '%s' "$palette_hits" | sed 's/^/  /' >&2
  echo "ui-guard:      use a theme token (bg-primary, text-base-content/55) or a component variant" >&2
  echo "ui-guard:      a deliberate palette value (a black scrim, a QR quiet zone) goes in PALETTE_ALLOWLIST with a reason" >&2
  fail=$((fail + 1))
else
  echo "ui-guard: ok   raw Tailwind palette classes (0; $palette_allowed allowlisted: ${PALETTE_ALLOWLIST[*]})"
fi

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
  "${go_files[@]}" "${templ_files[@]}"

# 4. data-testid duplicated inside one templ file, counted as excess copies.
dup_total=0
dup_report=""
for f in "${templ_files[@]}"; do
  # `|| true` inside the substitution: no testid at all is a normal file.
  counts="$(grep -ohE 'data-testid="[^"]+"' "$f" 2>/dev/null | sort | uniq -c || true)"
  excess=0
  while read -r c tok; do
    [ -z "${c:-}" ] && continue
    [ -z "${tok:-}" ] && continue
    if [ "$c" -gt 1 ]; then
      excess=$((excess + c - 1))
    fi
  done <<< "$counts"
  [ "$excess" -eq 0 ] && continue
  dup_total=$((dup_total + excess))
  dup_report="${dup_report}${excess} ${f}"$'\n'
done
if [ "$dup_total" -gt "$BASELINE_DUP_TESTIDS" ]; then
  echo "ui-guard: FAIL data-testid excess copies inside one file grew from $BASELINE_DUP_TESTIDS to $dup_total" >&2
  printf '%s' "$dup_report" | sed 's/^/  /' >&2
  echo "ui-guard:      a duplicated id makes getByTestId ambiguous on that route" >&2
  fail=$((fail + 1))
elif [ "$dup_total" -lt "$BASELINE_DUP_TESTIDS" ]; then
  echo "ui-guard: ok   data-testid excess copies $dup_total <= $BASELINE_DUP_TESTIDS (lower the baseline to $dup_total)"
else
  echo "ui-guard: ok   data-testid excess copies $dup_total <= $BASELINE_DUP_TESTIDS"
fi

# 5–8. Budgets.
# Any inline `on<event>=` on the element, at any indentation — the leading
# `[^[:alnum:]_-]` is what makes a tab-indented attribute visible (a literal
# space here silently skipped 9 of the 19 that exist).
budgeted "inline event handlers" "$BASELINE_INLINE_HANDLERS" \
  '(^|[^[:alnum:]_-])on[a-z]+=' \
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
