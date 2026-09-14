# 2026-09-08 — go-daisy CSS @source scan fix (hamburger icon + spotlight checkbox)

## Goal

User reported two top-bar regressions after the go-daisy CSS consolidation
(commit `18b6ee6`): (1) the sidebar-toggle hamburger icon no longer renders in
the topbar, and (2) a visible checkbox (`#spotlight-toggle`) sits at the very
top of the page. Diagnose and fix without re-introducing the 473KB monolithic
bundle.

## Outcome

Done. Root cause was a Tailwind `@source` scoping bug, not a template change.
One commit pushed: `9fa687f`.

- **Hamburger icon blank**: go-daisy `layout.Navbar` renders
  `lucide--menu` / `lucide--panel-left-close` icon spans, but those classes
  never compiled, so the spans had no SVG mask.
- **`#spotlight-toggle` visible**: daisyUI's `.modal-toggle` base rule
  (`appearance:none; opacity:0; width:0; height:0; position:fixed`) was never
  emitted, so the CommandPalette checkbox rendered as a native control.
- Both trace to one line: `@source not "../../vendor/**"` cancelling the
  vendored go-daisy `@source`. Fixed; verified in the live dev server's DOM
  (desktop + mobile viewport).

## Root cause (verified empirically)

`gateway/webui/css/app.css` declared:

```css
@source "../../vendor/github.com/emergent-company/go-daisy";
@source not "../../vendor/**";
```

Tailwind v4 treats `@source not` globs as **global exclusions over the final
candidate set, regardless of declaration order** (confirmed by building four
variants: dropping the `not` line and scoping it away from go-daisy both
restored the classes; reordering the two lines did not). So the broad exclusion
silently swallowed the go-daisy sources, and every class whose literal string
exists *only* in vendored go-daisy templates never compiled. The gateway's own
templates happen to mention most shared classes, which is why the regression
was localized to go-daisy-only icons and the `.modal-toggle` utility.

## Decisions

- **Scan `go-daisy/components`, exclude other vendor deps per top-level dir** —
  the shared `vendor/**` exclusion is unusable with go-daisy under
  `vendor/github.com/…`; per-dir exclusions keep protobuf/otel/etc. out of the
  scan while go-daisy sources are included. Rationale captured in a comment
  block at the `@source` declarations so it survives refactors.
- **Leave the compiled sheet larger (283KB vs the buggy 188KB)** — the 188KB
  sheet measured by the consolidation session was class-starved (95 lucide
  icons; this fix compiles the full go-daisy component set → 154 icons). Still
  ~40% under the old 473KB bundle; correctness over minimalism. The Phase-3
  codegen tool (derive the exact module set from Go imports) is the follow-up
  that would slim it again.
- **No template/JS changes** — the bug was pure CSS compilation; markup was
  always correct.

## Changes

- `gateway/webui/css/app.css` — replaced
  `@source ".../go-daisy"; @source not "../../vendor/**";` with
  `@source ".../go-daisy/components";` + one `@source not` per other top-level
  vendor dep (`buf.build`, `cel.dev`, `go.opentelemetry.io`, `go.uber.org`,
  `go.yaml.in`, `golang.org`, `google.golang.org`, `gopkg.in`), plus a NOTE
  explaining why.
- `gateway/webui/static/css/app.css` — regenerated (`283,386` bytes): now
  contains `.lucide--menu`, `.lucide--panel-left-close`, and the
  `.modal-toggle` base rule.
- Commit `9fa687f` pushed to `master` (`ab4c805..9fa687f`).

## Verification

- Four-`@source`-variant Tailwind builds to `/tmp` — baseline reproduces the
  bug (no `lucide--menu` / `.modal-toggle{appearance…`); dropping or
  re-scoping the exclusion restores both; reordering alone does not.
- `node_modules/.bin/tailwindcss -i webui/css/app.css -o webui/static/css/app.css --minify`
  — clean (one cosmetic lightningcss comment warning in vendored custom.css).
- `go test -run 'CompiledCSS|CSSDeterministic|PageHead' .` (gateway) — `ok`
  (`TestCompiledCSSExcludesUnusedDaisyUIModules`, `…Deterministic`,
  `…OmitsMonolithicGoDaisyCSS`).
- Live dev server (air auto-rebuilt `./memory`, port 8095): served
  `/assets/css/app.css` = 283KB containing both fix markers.
- Browser DOM checks (user's Chrome via DevTools MCP, Agents page):
  - `#_topbar-menu-icon` computed `mask-image` = the hamburger SVG;
    `display:block` at mobile width (390px), desktop shows `#_topbar-close-icon`.
  - `#spotlight-toggle` → `0×0` rect, `opacity:0`, `appearance:none`,
    `position:fixed` (invisible).
  - Screenshots captured but the model cannot read images — DOM evidence used.

## Open questions / follow-ups

- Residual CSS bloat: scanning all of `go-daisy/components` also emits classes
  /icons for the library's variant/demo components the gateway never renders
  (topbar-variants, sidebar-variants, profile-menu, …; ~59 extra icons). This
  interim superset is technically out of step with the
  `openspec/specs/web-ui-css` "SHALL NOT include styles for go-daisy components
  or icons the gateway does not use" clause — full compliance is the tracked
  codegen task [godaisy-codegen-css-tool](../tasks/godaisy-codegen-css-tool.md)
  (derive the exact module set from the Go import graph). Not re-opened here.
- `docs/spec/00-vision.md` D22 still quoted the pre-fix "188KB / 30KB gz"
  figure — updated this session to the real post-fix 283KB / 43KB gz (see
  spec update).
- Parallel-session WIP left untouched (not mine): `gateway/org_members_ui_templ.go`
  and a regenerated `gateway/webui/static/css/app.css` (283,751B) were dirty at
  session end. The regenerated sheet retains the fix (content verified).

## Tasks

- [css-godaisy-scan-regression-guard](../tasks/css-godaisy-scan-regression-guard.md) — assert go-daisy-only CSS (hamburger icons, `.modal-toggle`) in the compiled-sheet test
