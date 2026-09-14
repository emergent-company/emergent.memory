## Context

The iOS app (`client/ios/VoiceAgent`) is voice-first and single-screen: `VoiceAgentApp` → `AgentSessionRoot` → `AppView`, whose body is a `ZStack` that swaps between `StartView` (idle) and the interaction views (connected). Agent choice is hardcoded in `AgentSelection.curatedAgents` (Alfred, Diane) and persisted under `alfred.agentName`. A QR scanner (`AlfredQRScannerView`) currently writes connection settings.

Server side already has everything needed for agent management: the FastAPI control plane (`agent/api/main.py`, port `8081`) exposes `GET/POST/PUT/DELETE /api/agents` plus `/activate`/`/deactivate`/`/status`, authenticated by an `X-API-Key` header. The app already holds the API key (`alfred.apiKey`) and already has an authenticated HTTP client pattern (`AgentInfoClient` + `MemoryStore`).

Session history depends on the session-log API planned in the `browse-session-logs` change. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**

- Two-level navigation: first level = agent picker (large panels) + Settings; second level (per selected agent) = Conversation, Sessions, Agent settings.
- Drive the agent list and agent mutations entirely from the control-plane API.
- Repurpose QR scan as app-to-backend authentication (server URL + API key), not agent configuration.
- Reuse the existing `X-API-Key` auth and `AlfredConfig` plumbing rather than invent a new credential path.

**Non-Goals:**

- No backend changes to the control plane (it already supports the needed CRUD).
- No offline persistence or caching of the agent list — only the selected agent name stays persisted.
- No session-log mutation from the app (read-only), and no cross-account/tenant model.
- Not implementing the session-log API itself here — that is `browse-session-logs`; this change only consumes it.

## Decisions

### 1. Root shell: agent picker first, then a per-agent second level

Replace the single-screen root with a two-level hierarchy. Level 1 is the main screen: an agent picker rendered as large tappable panels (one per agent), plus a Settings entry point and an add-agent affordance (and an empty-state CTA when no agents exist). Choosing an agent pushes a second level scoped to that agent, showing three selectable destinations — Conversation (start/stop), Sessions, and Agent settings. Conversation is the default landing destination of the second level and hosts the existing `AppView` connect flow unchanged.

- *Alternative considered:* a flat four-tab `TabView` (Voice / Sessions / Agents / Settings). Rejected: the user wants the agent picker to be the first screen; a flat tab bar buries that hierarchy and splits per-agent context across tabs.
- *Alternative considered:* a single `NavigationStack` with a destination list. Rejected: loses the "big panels" picker affordance and mixes levels into one flat stack.

### 2. Second level: segmented destinations within the agent's scope

The per-agent second level uses a `NavigationStack` with a scoped switcher (segmented control or tab) for Conversation / Sessions / Agent settings, so each destination can push further detail (Sessions list → Session detail). The selected agent's name persists in the existing `alfred.agentName` key and binds this level's scope.

- *Alternative considered:* separate pushed screens for each destination with no switcher. Rejected: a persistent scoped switcher matches "second level menu" and keeps Conversation reachable in one tap.

### 3. Control-plane client: `ControlPlaneClient` mirroring `AgentInfoClient`

Add a `ControlPlaneClient` struct modeled on the existing `AgentInfoClient`: holds an `AlfredConfig`, builds URLs from a new `apiBaseURL`, sends `X-API-Key`, and uses `URLSession.shared` with typed `ControlPlaneError` (`.httpStatus`, `.transport`, `.decode`, `.invalidConfiguration`). Methods: `listAgents()`, `createAgent(...)`, `updateAgent(...)`, `deleteAgent(id:)`, `activate(id:)`, `deactivate(id:)`. Companion `Agent`/`AgentBackend` `Decodable` models mirror the control-plane response shape.

- *Alternative considered:* a generic shared `HTTPClient`. Rejected: the existing codebase uses small purpose-built clients; matching that style keeps review simple.

### 4. New config key: `alfred.apiBaseURL`

The control plane runs on `8081`, a different port from the token endpoint (`8080`), so deriving the URL from `tokenEndpoint` (as the memory client does) would be wrong. Add `apiBaseURL` (`alfred.apiBaseURL`, default `http://100.69.175.118:8081`) to `AlfredConfig`, alongside the existing `apiKey`.

- *Alternative considered:* derive from `tokenEndpoint` by swapping the port. Rejected: brittle, and the ports are genuinely independent deployments.

### 5. Replace hardcoded selection with an `AgentStore`

Replace `AgentSelection.curatedAgents` with an `AgentStore` (`@MainActor ObservableObject`) that fetches `GET /api/agents` into a published `[Agent]`, tracks the selected agent name (still persisted under `alfred.agentName`), and exposes add/remove/enable/disable mutations with a load state and stale-result guards like `MemoryStore`. The picker renders the fetched list as large panels. Selection both enters the second level and drives the voice connect flow.

- *Alternative considered:* keep `AgentSelection` and bolt on the list. Rejected: the curated list is exactly what must go away; a single store owns both the list and the selection.

### 6. QR scan = backend authentication, not agent config

Repurpose `AlfredQRScannerView`: the QR payload carries the server URL and API key (auth), and scanning writes `alfred.serverURL` + `alfred.apiKey` (+ `alfred.apiBaseURL` where present). It no longer reads or writes an agent name or dispatches an agent. The add-agent form remains a separate, explicit flow.

- *Alternative considered:* keep the QR as a full "setup" bundle including an agent. Rejected per user direction: onboarding authenticates the app to the server; agents are then added explicitly.

### 7. Session history as a thin per-agent consumer of `browse-session-logs`

`SessionsView` + `SessionDetailView` live inside the selected agent's second level and consume the session-log API contract from `browse-session-logs`, scoped to the selected agent (list endpoint + per-session timeline with turns/tool calls). Implement a `SessionLogClient` against that contract; read-only, shared `X-API-Key`.

- *Alternative considered:* bundling the session-log API into this change. Rejected: it already has its own change; duplicating the spec would fork the work.

### 8. Minimal add-agent form

The control-plane `AgentBase` schema is a rich discriminated union (three backend types, tools, MCP refs, sub-agents). The iOS add form captures the essential subset — name, backend type, model, base URL — and sends a valid `AgentBase` with sensible defaults for the rest. Advanced fields (MCP refs, sub-agents) are out of scope for the first pass.

- *Alternative considered:* a full form mirroring every field. Rejected: large surface for little first-pass value; defaults keep the payload valid while the backend remains the source of truth.

## Risks / Trade-offs

- [Control plane on a separate port, no auth in dev] → If `API_PORT` differs from `8081` or a production `API_KEY` is set, the app needs `apiBaseURL`/`apiKey` configured correctly; mitigations: new `alfred.apiBaseURL` setting, QR auth, and the existing settings screen edits `apiKey`.
- [Removing the hardcoded list breaks offline bootstrap] → First launch against an unreachable API shows an empty/error state; mitigation: the empty-state CTA doubles as the recovery path (re-auth via QR), and a persisted selected agent name lets a previously chosen agent still be used.
- [Add form can't express the full `AgentBase` schema] → Some agents can only be fully configured via the web/admin surface; mitigation: minimal valid form now, advanced editing deferred (documented non-goal).
- [Session screen depends on an unapplied change] → `browse-session-logs` must land first; mitigation: `SessionLogClient` is coded to that change's documented contract and the screen is feature-gated, so the rest of this change stays shippable independently.
- [QR payload schema must be agreed with the server] → The auth QR needs a defined payload (server URL + API key); mitigation: keep the payload minimal and document it so the server-side generator matches.
