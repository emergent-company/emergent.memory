## Why

The iOS app is a single-screen voice client with a hardcoded two-agent picker. The main screen should lead with the agents themselves — a first-level picker of large panels plus Settings — and reveal per-agent actions (Conversation, Sessions, Agent settings) only after an agent is chosen. Agents and sessions already live server-side (the control-plane DB and the session log), so the app should read and mutate the live backend over the control-plane API instead of baking in a static list.

## What Changes

- Replace the single-screen `ZStack` root with a two-level native navigation hierarchy: a first level that is an agent picker of large panels plus Settings, and a per-agent second level exposing Conversation (start/stop), Sessions, and Agent settings.
- Replace the hardcoded `curatedAgents` list with an API-driven agent list fetched from `GET /api/agents`.
- Show a call-to-action to add the first agent when no agents exist.
- Add agent add, remove, enable, and disable through the control-plane API (`POST/PUT/DELETE /api/agents`, `/activate`, `/deactivate`).
- Add a per-agent Sessions screen that lists the selected agent's sessions and shows a conversation timeline via the session-log API.
- Introduce an iOS control-plane API client authenticated with the existing `alfred.apiKey` `X-API-Key` header.
- Repurpose QR-code init: scanning a QR authenticates the app to the backend (server URL + API key); it no longer configures an agent.

## Capabilities

### New Capabilities

- `ios-native-navigation`: a two-level native shell — first level is the agent picker (large panels) plus Settings; choosing an agent opens a per-agent second level (Conversation, Sessions, Agent settings).
- `ios-agent-management`: API-driven agent management — list agents as a picker, add, remove, enable, disable, an empty-agent call-to-action, and QR-based backend authentication.
- `ios-session-history`: browse the selected agent's recorded sessions and view a session's conversation timeline from the iOS app.

### Modified Capabilities

None.

## Impact

- iOS: `client/ios/VoiceAgent/` — two-level shell, large-panel agent picker, per-agent conversation/sessions/agent-settings screens, a control-plane API client + models, and rewiring of `AgentSelection`/`StartView` to the API-driven list.
- Backend: `agent/api/main.py` — consumed read-only (existing agent CRUD endpoints); no backend changes required for agent management.
- Session-log API: consumed read-only by `ios-session-history`; the endpoint itself is delivered by the `browse-session-logs` change and is a prerequisite for the Sessions screen.
- Config: reuses the existing `alfred.apiKey`; adds `alfred.apiBaseURL` (control-plane `http://…:8081`); QR scan now writes server URL + API key for authentication.
- No breaking changes to the voice-call flow.
