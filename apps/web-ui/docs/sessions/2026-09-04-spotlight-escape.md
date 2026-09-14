# 2026-09-04 — Fix Spotlight Escape / ⌘K

## Goal

Make the Spotlight (⌘K command palette) respond to the Escape key (and ⌘K) — the "Esc" hint was rendered in the modal header, but pressing Escape did nothing.

## Outcome

Done. Escape now closes the palette; ⌘K / Ctrl+K now toggles it open (with focus moved to the input). Committed as `3e0d54d`.

## Decisions

- Fix in `gateway/spotlight.templ` (alfred-owned) rather than patching the pinned `go-daisy v0.9.0` dependency — durable, no module bump required, and the component bug is upstream's to fix separately.
- Bind a new `spotlightKeyboard` handler against the hardcoded `spotlight-toggle` / `spotlight-input` ids — mirrors the already-working `spotlightTrigger` click handler, so open/close semantics stay identical.
- Guard with `window._spotlightKeyboardInit` — avoids double-binding and doesn't collide with go-daisy's (broken, but still registered) `_paletteTriggerInit` listener.

## Changes

- `gateway/spotlight.templ` — added `spotlightKeyboard()` (document-level `keydown`: Escape unchecks `spotlight-toggle` when open; ⌘K/Ctrl+K toggles open + focuses `spotlight-input`) and call it from `spotlightSearch()` after `ui.CommandPalette`.
- `gateway/spotlight_templ.go` — regenerated via `templ generate` (gitignored; not committed).

## Root cause

go-daisy's `CommandPalette` (components/ui/command-palette.templ) emits a document-level `keydown` handler inside a `<script>` block, but templ does **not** interpolate `{ paletteID }` inside `<script>`/raw-text nodes. The shipped JS therefore calls `document.getElementById('{ paletteID }-toggle')` — a literal string that matches no element — so both Escape and ⌘K no-opped. Only the click path worked, because `spotlightTrigger` hardcodes `spotlight-toggle` in its `onclick`.

## Verification

- `templ generate` — success.
- `go build ./...` — success (clean).
- `golangci-lint run ./...` — 0 issues.
- `go test ./...` — ok (both packages pass).

## Open questions / follow-ups

- Upstream go-daisy bug remains: `CommandPalette` keyboard handler references an un-interpolated `{ paletteID }`. Fix upstream, bump go-daisy, then remove the `spotlightKeyboard` workaround. Tracked as task `godaisy-command-palette-keyboard`.

## Tasks

- [godaisy-command-palette-keyboard](../tasks/godaisy-command-palette-keyboard.md) — fix the go-daisy CommandPalette keyboard handler upstream, bump, drop the alfred workaround.
