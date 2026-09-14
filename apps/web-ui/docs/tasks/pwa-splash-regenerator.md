# Commit the PWA splash / maskable asset generator

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-agent-cards-ios-pwa](../sessions/2026-09-09-agent-cards-ios-pwa.md)

## What
Commit a small Go generator (or script) that regenerates the iOS splash images
(`gateway/webui/static/splash/*`) and the maskable icon
(`gateway/webui/static/icon-maskable-512.png`) from `icon-512.png`.

## Why
The assets were generated once by a throwaway Go program in `/tmp` (stdlib
`image/png` + a hand-rolled bilinear scaler, since the host has no
ImageMagick/PIL). When the brand icon changes, there is currently no way to
reproduce the 40 splash images — they would silently drift out of date.

## Depends on
none

## Notes
- Source icon is `gateway/webui/static/icon-512.png`; background color is
  `#0E1017` (matches `html{background-color}` and manifest `background_color`).
- Splash geometry table (portrait + landscape, CSS size + DPR) is documented in
  `gateway/pwa.templ` and the session log; keep the generator in sync with
  `appleSplashLinks()`.
- Missing today: M4-era iPad sizes `2420×1668` (11") and `2752×2064` (13").
  Add them when regenerating.
- iOS requires image pixel size == physical resolution for the matched class;
  only cold launches show the splash (warm relaunch = iOS screenshot), and the
  user must re-add to Home Screen to pick up changed launch images.
