## Why

Issue #1026's second requirement: the gateway has no `--color-muted` token. Muted text and icons are
expressed ad-hoc through ~14 near-identical `--color-base-content` opacities (`text-base-content/20/25/30/
35/40/42/45/50/55/60/65/70/75/80/85/90`) in gateway templates and JS, plus another ~15 `color:
color-mix(in oklab, var(--color-base-content) N%, transparent)` foreground rules in `app.css`. This is the
~500-site sweep that #890 §5.2 deferred, with its "no opacity outside the scale" rule.

## What Changes

- Define a single muted scale of five steps derived from the existing `--color-base-content` token —
  `muted-subtle` (25%), `muted-faint` (40%), `muted` (50%), `muted-strong` (65%), `muted-bright` (80%) —
  declared once as `@theme` colors in `webui/css/app.css` so Tailwind generates `text-muted-*` utilities
  and `app.css` can reference the same `--color-muted-*` variables.
- Migrate every gateway-owned muted foreground (`text-base-content/N` in `.templ`/`.go`/`.js`, the
  `color: color-mix(...)` foreground rules, the rail-active selector, and the four standalone `opacity-N`
  muted icons/text) to the scale. Content-tier dimming (85/90/92%) normalises to the full
  `--color-base-content` token.
- Leave go-daisy's own muted classes (`ui.Eyebrow` `/45`, `nav.PageHeading` `/55`, `form.FormControl`
  `label-text-alt /60`, the empty-state `/20` `/50`) untouched — that is the go-daisy upstream track
  (#890 §7), out of scope here.

## Capabilities

### Modified Capabilities

- `web-ui-css`: re-add the "Muted text and icon sizes come from a scale" requirement that #890 deferred,
  restated as "Muted text/icon foreground comes from a scale" scoped to gateway-owned sources.

## Impact

- Gateway web UI only: `webui/css/app.css` (scale definition + foreground-token migration + rail selector),
  ~45 `.templ`/`.go` files and ~9 `webui/static/js/*.js` files (class migration), and `refactor_exact_test.go`
  golden strings for gateway-owned components (go-daisy-pinned strings left as-is).
- No Go logic, API, schema, or migration change. No visual change beyond the intended normalisation of
  near-identical opacities onto canonical steps (documented in the PR body).
