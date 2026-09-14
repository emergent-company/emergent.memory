# CSS regression guard for go-daisy-only classes

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-godaisy-css-scan](sessions/2026-09-08-godaisy-css-scan.md)
**Closed by:** [2026-09-08-p0-e2e-coverage](../sessions/2026-09-08-p0-e2e-coverage.md)

## What

Extend `gateway/css_consolidation_test.go`'s compiled-sheet test
(`TestCompiledCSSExcludesUnusedDaisyUIModules`) so it asserts the presence of
go-daisy-*only* CSS that a `@source` regression silently drops — at minimum
the `.lucide--menu` icon var, `.lucide--panel-left-close`, and the
`.modal-toggle{appearance` base rule.

## Why

The `@source not` scoping bug (Tailwind treats `@source not` globs as global,
order-independent exclusions) stripped the vendored go-daisy scan from the CSS
build without any compile error: the topbar hamburger icon went blank and the
`#spotlight-toggle` command-palette checkbox rendered as a visible native
control. Existing assertions only cover gateway-template classes (e.g.
`sidebar-menu-item`), which is exactly why the regression slipped through — the
missing rules were referenced only by go-daisy templates.

## Depends on

none

## Notes

- **Closed:** the regression guard now lives in the browser e2e suite as
  `tests/e2e/specs/css-go-daisy-scan.spec.ts`, which asserts the compiled sheet
  actually ships the go-daisy rules on a real page: the `#_topbar-menu-icon`
  hamburger computes a non-`none` `mask-image` (the blank-icon bug) and
  `#spotlight-toggle` stays 0×0/hidden (the visible-checkbox bug), both at a
  390px viewport. The original Go-level compiled-sheet assertion remains a
  valid optional hardening; reopen if the e2e guard proves insufficient.
- Add markers that exist only in go-daisy-sourced classes, never in the
  gateway's own templates, so the guard fails precisely when the go-daisy
  `@source` is lost.
- `@source` declarations in `gateway/webui/css/app.css` carry the explanatory
  NOTE; keep it in sync if the scan set changes.
- Do not assert on the raw byte size (sheet is expected to grow/shrink as the
  module/icon set changes).
