# 11 — Reuse from Diane

The Diane project (`/root/diane`, `diane-assistant/diane`) is architecturally similar to
Memory: a Go server + a SwiftUI app, both tapping Emergent Memory. This file records what
Memory can lift **directly**, what to **adapt**, and what to **skip**.

> Verdicts from a deep-dive of the Diane server + iOS app (2026-08).

## Summary table

| Diane piece | Verdict | Why |
|---|---|---|
| iOS SwiftUI shell (connect, nav, lists, components, models, HTTP client) | **TAKE (adapt)** | Liftable almost as-is; add chat UI + session methods (Diane has none) |
| Go API patterns (route registration, auth middleware, pairing, Go-time JSON) | **TAKE** | Directly copyable for Memory's gateway; matches the Swift client contract |
| Contexts (context→server→tool overrides) | **TAKE (adapt)** | Ready-made agent tool-group/allowlist model with Emergent storage |
| ACP session persistence pattern (graph objects + labels) | **TAKE (pattern)** | Maps 1:1 to Memory's memory chat persistence |
| MCP proxy (stdio/http/sse + prefixing) | **REFERENCE** | Redundant — memory's MCP registry is the tool source |
| ACP agent/session manager | **REFERENCE** | Bound to ACP coding agents; Memory's brain is memory's chat loop |
| Providers config (Vertex/OpenAI/Ollama + usage) | **SKIP** | Memory owns model config |
| Distributed MCP (master/slave, mTLS) | **SKIP** | Out of scope for a single gateway |

## What to take directly

### 1. iOS client shell (TAKE)

One Xcode project, two targets (`Diane` macOS + `DianeIOS` iphoneos), shared source tree:

- `Diane/Diane/Models/` — `Agent.swift`, `MCPServer.swift`, `Context.swift` Codable models
  mirroring Go JSON (snake_case keys, Go time). Reusable for Memory's agent/MCP models.
- `Diane/Diane/Components/` — `DetailSection`, `InfoRow`, `EmptyStateView` (shared Mac+iOS).
- `Diane/Diane/Services/DianeClientProtocol.swift` — protocol-DI client (mocking-friendly).
- `Diane/Diane/Services/DianeHTTPClient.swift` — iOS HTTP client (~60 endpoints, Bearer auth).
- `Diane/Diane/Views/iOS/` — `IOSContentView` (adaptive TabView/NavigationSplitView),
  `ServerSetupView`, agent/MCP/context list views, `StatusDashboardView`.

**Not liftable — Memory must build from scratch:**
- Chat/conversation UI (streaming) — Diane has **zero** chat UI.
- Voice/LiveKit audio handling — Diane is text-MCP only.
- Session list/resume — the server has `/sessions` but no Swift client uses it.
- Real write paths — the iOS client hardcodes `readOnlyMode` stubs for most writes.

### 2. Go API patterns (TAKE)

`server/internal/api/` + `internal/pairing/`:

- **Per-entity API structs** with `RegisterRoutes(mux)` — clean pattern for Memory's gateway.
- **Auth middleware**: `readOnlyMiddleware` (GET/HEAD if no key) vs `apiKeyAuthMiddleware`
  (full access with key). Copyable.
- **Pairing endpoint** (`POST /pair` → `{api_key}`): HMAC time-window 6-digit code, 30s
  window, rate-limited. A **better device-onboarding flow** than Memory's current
  QR-with-shared-key — candidate for iOS onboarding.
- **Go-time JSON convention** already matched by the Swift models (snake_case + Go time).

### 3. Contexts — agent tool scoping (TAKE, adapt)

Diane's 3-level model `context → mcp_server → per-tool overrides` is a ready-made
"which tools can this agent call" design, persisted as Emergent graph objects
(`context_emergent.go`, `mcp_server_emergent.go`).

Memory v1 uses memory's per-attachment allowlist + `tool_policies`. Contexts is a candidate
**future refinement** for grouping/reusing tool sets across agents — adopt the data model
when tool scoping grows beyond per-attachment allowlists.

### 4. Graph persistence pattern (TAKE)

Diane persists sessions/messages as Emergent graph objects with label-based lookup
(`acp_session` / `acp_session_message`, labels like `agent:`, `session_id:`, `status:`).
This is **exactly** how Memory should persist voice chat turns in memory (Memory delegates
the chat loop to memory anyway, so conversation persistence is memory's job — but the
label/typed-object convention is the reference for any Memory-side annotations).

## What to reference (not copy)

- **MCP proxy** (`internal/mcpproxy/proxy.go`): stdio/http/sse transports + `{server}_` tool
  prefixing + auto-restart. Redundant because memory's MCP registry aggregates tools. Copy
  only if the Memory gateway must aggregate local stdio MCP servers.
- **ACP agent/session manager** (`internal/acp/`): JSON-RPC-over-stdio + multi-turn manager
  + idle reaper. Well-built but bound to ACP coding agents; Memory's brain is memory's chat
  loop. Adapt the transport only if "delegate to OpenCode" becomes an agent tool.

## What to skip

- **Providers/usage config** — memory owns model config + usage.
- **Distributed MCP** (master/slave pairing, mTLS, node routing) — single gateway.

## Net effect on the spec

1. iOS client is a **partial lift**: shell + models + components + HTTP client from Diane;
   chat UI + voice + streaming + write paths are new (see 07-clients.md).
2. Go gateway copies Diane's API/pairing patterns (see 04-go-application.md).
3. Contexts is recorded as a future tool-scoping refinement (see 03-agent-model.md).
