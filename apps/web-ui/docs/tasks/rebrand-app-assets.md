# Rebrand app assets (manifest + PWA icons)

**Status:** done
**Created:** 2026-09-04
**Source:** [2026-09-04-rebrand-page-titles](../sessions/2026-09-04-rebrand-page-titles.md)

## Resolution (2026-09-09)

- `manifest.webmanifest` stale `description` removed (matches the HTML
  subtitle deletion; name/short_name were already "Memory").
- PWA icons regenerated to the "M" monogram at native sizes (apple-touch 180,
  192, 512): full-square diagonal gradient matching the in-app
  `memory-monogram` (oklch 82%.14/85 → 62%.16/62, ink oklch 16%.03/60),
  bold "M" centered. Generator: `/tmp/opencode/gen_icons.py` (Pillow, run via
  `uv run --with pillow`); colors converted oklch→sRGB in-script.

## What

Finish the "M" rebrand at the asset level:

1. `gateway/webui/static/manifest.webmanifest` still has `"description": "Voice · chat agents"` — remove it (the subtitle was deleted from the HTML, so the manifest now drifts from the UI).
2. Regenerate the PWA icons to show the "M" monogram instead of "A":
   - `gateway/webui/static/apple-touch-icon.png`
   - `gateway/webui/static/icon-192.png`
   - `gateway/webui/static/icon-512.png`

## Why

The HTML rebrand (logo letter `A` → `M`, subtitle removal) shipped in commit `190cfc5`, but the standalone-app manifest and icons still carry the old subtitle text and (likely) the old letter, so "Add to Home Screen" / favicon surfaces still show stale branding.

## Depends on

none

## Notes

- Icons dated 2026-09-01 predate the rename and rebrand — confirm the current glyph before regenerating.
- Match the `memory-monogram` visual (rounded square, `font-bold`, the letter) in `gateway/ui.templ`.
- Also reconcile any other brand string: `manifest` `name`/`short_name` are already `"Memory"` (correct); only `description` is stale.
