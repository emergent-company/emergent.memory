# Voice secrets UI-editable

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-password-reveal-toggle](sessions/2026-09-09-password-reveal-toggle.md)

## What

Decide whether the voice-provider secrets — Cartesia API key, Deepgram API key, LiveKit API secret — should be editable from the Settings → Voice UI instead of only env-set. Today they render as read-only `set` / `not set` rows in `project_settings.templ` (`voiceSecretRow`); the provider API-key field (the only masked credential input in the app) already got a reveal toggle via go-daisy `PasswordField`.

## Why

They are the only provider credentials in the web UI with no input surface, so the reveal-toggle treatment the rest of the app now has can't apply to them. If they stay env-driven, that's a defensible (secrets-out-of-DB) choice — but it should be explicit.

## Depends on

none

## Notes

- If they become editable, route them through the same secret-handling + reveal pattern as the provider API key (never return stored values; draft repopulation on save error; `autocomplete="off"`).
- Backend currently validates/reads these from env (`v.CartesiaKeySet` etc.) — editing would require a memory-side or gateway-side secret store change; size accordingly.
