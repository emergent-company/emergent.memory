## Context

Alfred's voice flow is a LiveKit-based call: the gateway's chat page embeds a browser client (`voice.js`) that connects to a LiveKit room, while a per-agent bridge worker (`alfred_bridge`) provides Cartesia text-to-speech and Deepgram speech-to-text. Today every voice parameter is read from environment variables — the gateway (`gateway/config.go`) and the worker (`alfred_bridge/config.py`) read the same env — and the Project Settings page only *displays* these values read-only via `voiceSettings` / `voiceGatewayPanel` (`gateway/settings_handlers.go`, `gateway/project_settings.templ`). Secrets are surfaced only as set/unset booleans.

The Project Settings page already persists per-project configuration through the Memory service's generic project-settings API (`GetProjectSetting` / `SetProjectSetting` / `DeleteProjectSetting`, category + key + a small `map[string]any`), used today for the remember pipeline, dedup threshold, and assistant agent. This is the natural storage mechanism for the new Voice section.

## Goals / Non-Goals

**Goals:**
- Add an editable Voice section to Project Settings, replacing the read-only panel.
- Store per-project voice settings in Memory, layered over env defaults.
- Add a master enable/disable toggle.
- Gate the chat page's voice controls on that toggle (server-side).

**Non-Goals:**
- Wiring per-project provider overrides into the bridge worker's runtime voice flow. The worker remains env-configured for the actual TTS/STT; this change stores and surfaces the settings plus delivers the enable/disable gating. Making the worker read per-project settings at session start is a separate follow-up.
- Storing provider API keys per project. Secrets stay environment-managed and are shown set/not-set only.

## Decisions

### Store voice settings in Memory under a `voice` category, one key per field

Per-project voice settings live in the Memory service via the existing generic settings API, using category `voice` and one key per field: `enabled`, `tts_provider`, `tts_model`, `tts_voice`, `stt_provider`, `stt_model`, `stt_language`, `livekit_url`, `livekit_public_url`, `exit_keywords`, `goodbye_text`, `away_timeout`, `endpoint_min_delay`, `endpoint_max_delay`, `allow_interruptions`, `preemptive_generation`.

- *Why over a single JSON blob key:* matches the existing remember/dedup/assistant pattern, keeps reads/writes partial and idempotent, and reuses the already-built `MemoryClient` settings methods.
- *Alternative considered:* one `voice` category with a single `config` key holding the whole object. Rejected — inconsistent with the surrounding code and makes partial edits noisier.

### Keep provider secrets environment-only

Cartesia API key, Deepgram API key, and the LiveKit API key/secret are NOT stored per project. They remain env-provided and are rendered set/not-set, matching the current security posture.

- *Why:* the generic settings API stores plaintext; these secrets are consumed by a shared worker that reads env. Exposing or duplicating them per project is an unnecessary risk.
- *Alternative considered:* storing encrypted secrets per project. Rejected as out of scope and unsupported by the current settings API.

### Resolve effective config by layering project settings over env defaults

The gateway reads the project's stored settings on render and for save; a small helper merges stored values over the existing `s.cfg` env defaults so an unset field falls back to the env default (and `enabled` defaults to `true`, preserving today's always-on behavior).

- *Why:* keeps a single effective-config path and avoids regressions for existing deployments that have only env configured.
- *Alternative considered:* requiring every field to be explicitly stored. Rejected — noisy writes and breaks existing env-only setups.

### Gate voice controls server-side, not in JS

The chat page (`ChatPage`) receives a `VoiceEnabled bool`; when false it omits the voice bar (call/mute buttons, status readout, level meter), the `<audio id="agent-audio">` element, and the `voice.js` script, and renders text-only copy. `voice.js` already no-ops when the call button is absent, so server-side omission is sufficient.

- *Why:* avoids a flash of controls and a client-side round-trip; `voice.js` is already defensive about a missing button.
- *Alternative considered:* always render and hide with JS after checking a config endpoint. Rejected — more moving parts and a visible flash.

### Reuse the PRG form pattern from the existing settings handlers

The Voice section submits via a new `POST /settings/voice` handler following the existing `uiProjectSettingsRemember` / `uiProjectSettingsAssistant` PRG pattern (redirect with `?updated=1` / `?err=1`), and a new `MemoryClient` method pair to read/write the voice category.

- *Why:* consistency with the page's other forms and its existing tests.
- *Alternative considered:* an htmx/partial-update form. Rejected — the page is a classic PRG form surface.

## Risks / Trade-offs

- **[Per-project provider edits do not yet change worker behavior]** → The worker still reads env for TTS/STT. The toggle and UI gating are fully effective now; provider overrides are persisted and surfaced for a follow-up that teaches the worker to read them. Documented as a non-goal to avoid silent spec drift.
- **[Default-enabled preserves current behavior]** → New projects with no stored setting keep voice on, matching today's always-on UI and avoiding surprise removal of controls.
- **[Secret leakage via settings API]** → Mitigated by never storing secrets per project and rendering only set/not-set.
- **[Unreachable Memory on the chat page]** → The enabled lookup degrades to the env/current default so the chat page still renders; the Voice section shows an error state without crashing the page.

## Migration Plan

No schema or data migration: new `voice` category keys are created lazily on first save, and reads of absent keys fall back to env defaults. Rollback is a straightforward revert — the unused `voice` settings keys are inert.

## Open Questions

None.
