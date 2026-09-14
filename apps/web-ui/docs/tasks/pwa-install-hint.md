# iOS "Add to Home Screen" install hint

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-agent-cards-ios-pwa](../sessions/2026-09-09-agent-cards-ios-pwa.md)

## What
Add an in-app "Add to Home Screen" hint for iOS Safari, shown only when the app
is running in the browser (not already standalone).

## Why
iOS has no install prompt — users must manually Share → Add to Home Screen to
get the standalone (chrome-free) experience built in D32. The new
`data-standalone` flag on `<html>` (see `gateway/webui/static/js/app.js`)
makes it cheap to detect "in Safari, not installed" and surface a one-time hint.

## Depends on
- D32 (PWA standalone) — detection already shipped; only the UI remains.

## Notes
- Gate on `navigator.standalone === false` / absence of `data-standalone`, and
  only for iOS Safari (not desktop); dismiss + persist via `localStorage`.
- Keep it minimal — this is polish, not a required flow.
