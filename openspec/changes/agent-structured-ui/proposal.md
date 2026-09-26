## Why

Agents today can only answer with markdown text. Every structured thing an agent wants to do — propose a blueprint/skill/agent/MCP-server/provider/object change, gate a tool call, request structured input, or return graph data — is either rendered as a raw JSON/YAML fence or pushed through the `proposal` envelope that only the web gateway's templ renderer understands. There is no single declarative contract an agent can emit that renders natively on web, on iOS, and to an external A2A client.

A2UI (Google's Apache-2.0 Agent-to-UI protocol, https://a2ui.org) is that contract: an agent emits declarative JSON component descriptions (a flat adjacency list + a JSON-Pointer data model) and a client renders them with its own native widgets. "Safe like data, expressive like code" — no executable code crosses the wire, and the client only ever renders components from a pre-approved catalog.

This change introduces a first-class `agent-structured-ui` capability: a catalog of the cards our agents already emit semantically (proposal, approval, question, code, entity, object-form, todo, result), transport for those surfaces over our existing chat SSE and A2A message flow, and native renderers on web and iOS.

## What Changes

- Add an **A2UI v0.9.1 catalog** (JSON Schema) defining our component/card set as the single source of truth, plus Go envelope types and a validator for the four messages (`createSurface`, `updateComponents`, `updateDataModel`, `deleteSurface`).
- Teach the **executor** to recognize A2UI output (fenced ```a2ui blocks and/or a structured tool output) and emit a new `StreamEventA2UI`; invalid output degrades to text and never fails the run.
- Carry A2UI over the **chat SSE** path as a new `ui` event type, and over **A2A** as `data` parts with MIME `application/a2ui+json`; advertise the A2UI extension on the AgentCard.
- Add a **web component registry** (Lit `@a2ui/lit` island inside the templ gateway) and an **iOS SwiftUI renderer** that consume the same payload.
- Wire the **action round-trip**: a button/input event returns to the run with its `surfaceId` + action and resumes with updated surfaces.
- Generalize the existing `agent-proposals` card registry so a proposal card is also representable as an A2UI `proposal` component.

## Capabilities

### New Capabilities

- `agent-structured-ui`: A2UI catalog, executor emission, chat-SSE + A2A transport contract, web + iOS rendering, action round-trip, and the security model.

### Modified Capabilities

- `a2a-message-flow`: represent A2UI surface messages as A2A `data` parts and resume on surface actions.
- `a2a-discovery`: advertise the A2UI extension and accept A2UI client capabilities on the AgentCard.
- `web-chat-streaming`: forward structured `ui` events alongside text deltas and the markdown snapshot.
- `agent-proposals`: make the proposal card A2UI-representable so A2UI-capable clients render it natively.

## Impact

- `apps/server/pkg/a2ui/` (new) — catalog JSON Schema, Go envelope/component types, validator.
- `apps/server/domain/agents/executor.go` — new `StreamEventA2UI` + extraction/validation.
- `apps/server/pkg/sse/events.go` — new `ui` event type; `apps/server/domain/chat/handler.go` `streamCallback` emits it.
- `apps/server/domain/agents/a2a_stream.go` + `a2a_dto.go` — translate `StreamEventA2UI` → data-part `artifactUpdate`; discovery advertises the extension.
- `apps/web-ui/gateway/sse_markdown.go` (`rewriteChatStream`) + `webui/static/js/chat-host.js` (`makeHandleEvent`) + a Lit `@a2ui/lit` island in `chat.templ`.
- `apps/web-ui/gateway/proposal.go` — expose the proposal registry through the A2UI catalog (additive).
- `apps/ios/VoiceAgent/Chat/ChatEvent.swift` + `ChatView.swift` — SwiftUI A2UI surface renderer.
- No breaking change to the existing markdown/token stream; new event types are additive and unhandled events are ignored by old clients.

## Out of scope (deferred)

- Charts/maps/canvas via MCP Apps (`ui://` iframe) — a separate follow-up; the A2UI basic catalog has no chart component.
- Multi-agent surface-ownership stripping of `a2uiClientDataModel` — required only if the server later orchestrates sub-agents; the server is a leaf today.
- A2UI v1.0 migration (`actionResponse`, `theme`→`surfaceProperties`) — tracked as a follow-up once v1.0 stabilizes.
