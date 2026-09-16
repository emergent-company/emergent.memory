## 1. Config + control-plane API client

- [x] 1.1 Add `apiBaseURL` to `AlfredConfig` (`alfred.apiBaseURL`, default `http://100.69.175.118:8081`) — property, `UserDefaults` key, build-time default, and entries in `save()`/`reset()`
- [x] 1.2 Add a `ControlPlaneClient` struct (mirroring `AgentInfoClient`) with `listAgents()`, `createAgent(_:)`, `updateAgent(id:_:)`, `deleteAgent(id:)`, `activate(id:)`, `deactivate(id:)`, sending the configured `X-API-Key` header and throwing typed `ControlPlaneError` (`.httpStatus`, `.transport`, `.decode`, `.invalidConfiguration`)
- [x] 1.3 Add `Agent` and `AgentBackend` `Codable` models matching the control-plane `/api/agents` response shape (id, name, enabled, backend, plus the subset the UI needs)

## 2. Two-level navigation shell

- [x] 2.1 Add an `AppShellView` presenting the first level: a large-panel agent picker plus a Settings entry point
- [x] 2.2 Add the per-agent second level (Conversation / Sessions / Agent settings), pushed when an agent is chosen, with Conversation as the default destination
- [x] 2.3 Move the existing `AppView` connect flow into the Conversation destination unchanged
- [x] 2.4 Ensure an active voice session survives navigating within and back out of the second level (session objects are not torn down)

## 3. Agent picker + management (API-driven)

- [x] 3.1 Add an `AgentStore` (`@MainActor ObservableObject`) that fetches `GET /api/agents` into a published `[Agent]` with load/error state and stale-result guards
- [x] 3.2 Render the picker as large tappable panels from `AgentStore`, replacing the hardcoded `AgentSelection.curatedAgents`; keep the selected name persisted under `alfred.agentName`
- [x] 3.3 Show an empty-agent call-to-action that opens the add-agent flow when the list is empty
- [x] 3.4 Add an add-agent form (name + backend type + model + base URL) that POSTs a valid `AgentBase` to `POST /api/agents` and surfaces the 409 duplicate-name error for correction
- [x] 3.5 Add a remove action with confirmation that calls `DELETE /api/agents/{id}` and refreshes the list
- [x] 3.6 Add enable/disable actions that call `/api/agents/{id}/activate` and `/deactivate` and reflect the updated state
- [x] 3.7 Add an Agent settings destination in the second level showing the agent's name, backend, and enabled state, with edit/remove/enable/disable actions
- [x] 3.8 Add new localization keys to `Localizable.xcstrings` for the picker, add-agent form, and agent settings

## 4. QR authentication

- [x] 4.1 Repurpose `AlfredQRScannerView` to read a QR payload carrying the server URL and API key, writing `alfred.serverURL`, `alfred.apiKey`, and (where present) `alfred.apiBaseURL` — no agent configuration
- [x] 4.2 Report malformed/missing-field QR codes and keep the previous configuration on failure
- [x] 4.3 Add `apiBaseURL` to the admin `_qr_payload()` so the auth QR fully configures the control-plane base URL (host + `API_PORT`, default 8081)

## 5. Session history

- [x] 5.1 Add a `SessionLogClient` against the session-log API contract from `browse-session-logs` (list endpoint + per-session timeline with turns and tool calls), read-only and using the shared `X-API-Key`
- [x] 5.2 Add a `SessionsView` listing the selected agent's recorded sessions most recent first, with an empty-state when none exist
- [x] 5.3 Add a `SessionDetailView` showing the chronological timeline (user/assistant turns, expandable tool-call details: name, arguments, result, error flag)
- [x] 5.4 Treat an unknown session as an empty timeline and expose no create/edit/delete actions
- [x] 5.5 Add new localization keys to `Localizable.xcstrings` for the Sessions screens

## 6. Verification

- [x] 6.1 Rebuild the iOS app in Xcode (or the iOS taskfile) and confirm it compiles with no errors
- [ ] 6.2 Confirm the first level shows the agent picker (or the empty-agent CTA) and Settings, and that choosing an agent opens its Conversation/Sessions/Agent settings second level
- [ ] 6.3 Confirm the Voice flow still connects to the selected agent and that an active session survives navigating away from Conversation and back
- [ ] 6.4 Confirm list, add, remove, enable, and disable all work against `GET/POST/DELETE /api/agents` (+ `/activate`/`/deactivate`), and that QR scan authenticates the app to the backend without configuring an agent
- [ ] 6.5 Confirm the Sessions destination lists the selected agent's sessions and opens a session timeline; confirm no regression to the existing memory and settings flows
