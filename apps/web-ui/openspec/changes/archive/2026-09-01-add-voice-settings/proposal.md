## Why

Alfred's voice features (LiveKit voice calls, Cartesia text-to-speech, Deepgram speech-to-text) are configured entirely through server environment variables. The Project Settings page only *displays* these values read-only, so a user cannot turn voice on or off, choose providers, or adjust voice behavior without restarting the gateway with new env. Exposing a Voice section on the Project Settings page — including a master enable/disable toggle — closes that gap and lets the user hide voice controls from the chat UI when voice is off.

## What Changes

- Add a **Voice** section to the Project Settings page that edits voice configuration: text-to-speech provider and settings, speech-to-text provider and settings, and the remaining voice-flow options (LiveKit endpoint, exit keywords, goodbye text, away timeout, endpoint delays, interruption/preemptive toggles).
- Add a **master "enable voice" toggle** to that section.
- Store per-project voice settings in the Memory service using the existing generic project-settings API, layered over the current environment defaults.
- When voice is disabled, the chat page SHALL omit the voice controls (call button, mute button, mic level meter, status readout, audio element, and the voice client script) and reflect a text-only UI.
- Extend `MemoryClient` to read and write the voice settings.

## Capabilities

### New Capabilities

- `project-voice-settings`: a Voice settings section on the Project Settings page for configuring the text-to-speech and speech-to-text providers plus other voice options, a master enable/disable toggle, and the chat UI's behavior of hiding voice controls when voice is disabled.

### Modified Capabilities

<!-- No existing capability requirements change. -->

## Impact

- **gateway/** (Go + templ): new/updated Voice settings form and handler, new `MemoryClient` methods, a voice-enabled flag threaded into the chat page, and removal of the read-only voice panel in favor of the editable section.
- **Memory service REST API** (read/write dependency, no service change required): the existing `/api/projects/:projectId/settings/:category/:key` endpoints.
- **Chat UI** (`chat.templ`, `voice.js`): conditional rendering of the voice bar and script based on the enabled flag.
- **Tests**: gateway unit tests for the new handler, `MemoryClient` methods, and the chat page's voice-control gating (TDD), following the existing `settings_handlers_test.go` / `project_settings_ui_test.go` patterns.
