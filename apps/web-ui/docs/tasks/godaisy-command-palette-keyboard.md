# Fix go-daisy CommandPalette keyboard handler upstream

**Status:** done
**Created:** 2026-09-04
**Source:** [2026-09-04-spotlight-escape](../sessions/2026-09-04-spotlight-escape.md)

## Resolution (2026-09-09)

- Upstream go-daisy `command-palette.templ` fixed (`1edc808`): palette toggles
  carry `data-command-palette`, and the single global ⌘K/Escape handler (templ
  cannot interpolate inside `<script>`, so ids were emitted as the literal
  `{ paletteID }`) now resolves toggles at runtime — ⌘K opens the first palette,
  Escape closes any open one. Works for any palette id / multiple instances.
- Gateway pinned to the fix (`v0.10.1-…1edc8086bb46`) and the
  `spotlightKeyboard()` workaround removed from `gateway/spotlight.templ`
  (component now handles ⌘K + Escape for the `spotlight` palette). Full gateway
  suite + lint green.

## What

Fix the keyboard handling in `github.com/emergent-company/go-daisy` `components/ui/command-palette.templ` so the ⌘K open + Escape close listener binds against the *actual* palette id, not the literal string `{ paletteID }`. Then bump go-daisy in `gateway/go.mod` and remove the `spotlightKeyboard()` workaround from `gateway/spotlight.templ`.

## Why

templ does not interpolate `{ paletteID }` inside `<script>` blocks, so the component's `getElementById('{ paletteID }-toggle')` never matches and both Escape and ⌘K silently no-op for every consumer. alfred works around it locally (commit `3e0d54d`), but the component remains broken for any other user.

## Depends on

none

## Notes

- The `paletteID` string must reach the emitted JS. Options: emit a `templ.Raw`-style expression that templ actually evaluates, or read the id from a `data-*` attribute / the input's `id` at runtime (e.g. derive `-toggle` from `input.id.replace('-input', '-toggle')`), mirroring how `filterPaletteItems` already derives `-results` from `input.id`.
- Verify against the local checkout `/root/go-daisy` (same source as the `v0.9.0` module cache); publish a new tag and update `gateway/go.mod` before dropping the alfred workaround.
- Keep the `_paletteTriggerInit` guard semantics so multiple palettes on one page still bind correctly.
