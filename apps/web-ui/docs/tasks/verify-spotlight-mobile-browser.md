# Verify mobile spotlight behavior in browser

**Status:** done
**Created:** 2026-09-08
**Superseded by:** [2026-09-08-p0-e2e-coverage](../sessions/2026-09-08-p0-e2e-coverage.md)
Superseded by e2e `spotlight-mobile.spec.ts` (mobile sheet, X/Esc close, filter).
**Source:** [2026-09-08-spotlight-mobile](../sessions/2026-09-08-spotlight-mobile.md)

## What

Manually verify the mobile spotlight/search behavior in a real browser (DevTools device emulation or a phone):

1. Topbar search field is a square icon-only button at `< md` (768px) width — no "Search…" text, no ⌘K kbd.
2. Tapping the icon opens the spotlight palette **full-screen** (edge-to-edge, no centered `max-w-lg` dialog), input row pinned top with notch safe-area clearance, results list scrolls and fills the screen.
3. The close button shows an **X** (not "Esc") and closes the palette.
4. At `md`+ the trigger is the full pill (icon + "Search…" + ⌘K) and the palette is the centered dialog with the "Esc" label — desktop unchanged.

## Why

The three commits (`6a6d881`, `add7993`, `774e349`) were verified only by `templ generate` + `go build` + lint. The dev environment's `localhost` resolves to the user's machine, so no live browser check was possible during the session.

## Depends on

none

## Notes

- Breakpoint is `max-width: 767px` (Tailwind `md` = 768px).
- Full-screen rules are gated on `#spotlight-toggle:checked + .modal`; the X replaces the "Esc" label via `font-size: 0` + `::after { content: "✕" }` (screen readers still get "Esc").
- Also confirm the avatar in the topbar no longer renders a chevron-down icon.
