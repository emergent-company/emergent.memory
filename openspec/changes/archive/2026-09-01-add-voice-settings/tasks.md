## 1. Voice settings model and storage helpers

- [x] 1.1 Define the `voice` settings category and per-field keys (enabled, tts_provider, tts_model, tts_voice, stt_provider, stt_model, stt_language, livekit_url, livekit_public_url, exit_keywords, goodbye_text, away_timeout, endpoint_min_delay, endpoint_max_delay, allow_interruptions, preemptive_generation) and extend `voiceSettings` into an editable form model. Verify `go build ./...` compiles.
- [x] 1.2 Add a helper that reads the project's voice settings via `GetProjectSetting` for each key and resolves the effective config by layering stored values over the env defaults in `s.cfg`, with `enabled` defaulting to true. Verify the dedicated unit test passes.
- [x] 1.3 Add a helper that persists voice settings via `SetProjectSetting`/`DeleteProjectSetting` for changed non-secret fields and never writes secrets. Verify the dedicated unit test passes.
- [x] 1.4 Write unit tests for resolution and persistence: unset field falls back to env default, `enabled` defaults true when absent, a saved field round-trips, and empty/absent secrets are skipped. Verify `go test ./...` passes.

## 2. Voice settings handler

- [x] 2.1 Implement `POST /settings/voice` following the existing PRG pattern (`uiProjectSettingsRemember`), validating away timeout and endpoint delays as non-negative numbers before persisting. Verify `go build ./...` compiles.
- [x] 2.2 Register the new `/settings/voice` route. Verify the route is reachable in a running gateway.
- [x] 2.3 Write handler unit tests: a valid save persists and redirects with `?updated=1`; an invalid numeric delay redirects with `?err=1` and persists nothing. Verify `go test ./...` passes.

## 3. Voice section template

- [x] 3.1 Replace the read-only `voiceGatewayPanel` in `project_settings.templ` with an editable Voice form: master enable toggle, text-to-speech provider/model/voice, speech-to-text provider/model/language, LiveKit URLs, exit keywords, goodbye text, away timeout, endpoint delays, and the interruption/preemptive toggles. Verify `templ generate` succeeds and `go build ./...` compiles.
- [x] 3.2 Render provider secrets (Cartesia key, Deepgram key, LiveKit secret) as set/not-set only, and render absent optional fields as a clear "not set" state. Verify the render unit test passes.
- [x] 3.3 Write/extend render tests for the Voice form in `project_settings_ui_test.go` covering the editable fields, the enabled toggle, and the set/not-set secret states. Verify `go test ./...` passes.

## 4. Chat page voice gating

- [x] 4.1 Thread a `VoiceEnabled` flag into `ChatPage` and conditionally omit the voice bar (call/mute buttons, status readout, level meter), the `<audio id="agent-audio">` element, and the `voice.js` script when disabled. Verify `templ generate` succeeds and `go build ./...` compiles.
- [x] 4.2 Resolve `VoiceEnabled` in the chat page handler from the project's stored voice settings (default enabled true). Verify `go build ./...` compiles.
- [x] 4.3 Write unit tests asserting a disabled chat page omits the voice controls and script, and an enabled one includes them. Verify `go test ./...` passes.

## 5. Build, lint, and verification

- [x] 5.1 Run `templ generate` (via `/root/go/bin/templ generate`) and `go build ./...` from `gateway/`. Verify both succeed.
- [x] 5.2 Run `task lint`. Verify it passes with no new findings.
- [x] 5.3 Run `go test ./...` from `gateway/`. Verify all suites pass.
- [x] 5.4 Manually verify via the DevTools browser: the Project Settings Voice section saves values and toggles; disabling voice removes the chat page's voice controls and enabling restores them.
