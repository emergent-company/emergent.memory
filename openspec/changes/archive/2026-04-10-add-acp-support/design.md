## Context

Memory agents currently expose a project-scoped REST API at `/api/projects/:projectId/agents/` and internal MCP tools — both require knowledge of Memory's internal project IDs and authentication model. The ACP (Agent Communication Protocol) v0.2.0 defines a vendor-neutral HTTP interface for agent discovery, run management, and streaming. This design adds ACP as a top-level route prefix, reusing existing executor infrastructure, with parallel SDK/CLI and MCP tool support.

Key existing infrastructure:
- `AgentDefinition.Visibility` field (`external`/`project`/`internal`) already gates discoverability
- `AgentDefinition.ACPConfig` (JSONB) already exists with `DisplayName`, `Description`, `Capabilities`, `InputModes`, `OutputModes`
- `AgentExecutor.ExecuteWithRun()` and `Resume()` already support `StreamCallback` for real-time events
- `pkg/sse.Writer` handles SSE header management and flushing
- `AgentQuestion` implements human-in-the-loop pause/resume
- All run statuses (`queued`, `running`, `paused`, `success`, `error`, `cancelled`) are already tracked

## Goals / Non-Goals

**Goals:**
- Expose externally-visible agents via the ACP v0.2.0 HTTP interface at `/acp/v1/`
- Support all three run modes: `sync` (block until done), `async` (fire-and-forget 202), `stream` (inline SSE on POST response body)
- Persist ACP SSE events to enable `GET /runs/:runId/events` (full event log as JSON)
- Add thin session grouping (no cross-run context injection)
- Provide a Go SDK client (`pkg/sdk/acp/`) and CLI command group (`memory acp`)
- Register 3 MCP tools for agent-to-agent ACP invocation
- Zero impact on existing `/api/` routes, CLI commands, and MCP tools

**Non-Goals:**
- `.well-known/agent.yml` discovery document (defer to v2)
- `CitationMetadata` on message parts
- Message Artifacts (named parts) — all parts are unnamed
- Session `state` URI scratchpad — sessions track run history only
- Cross-run context injection (session runs are independent)
- ACP push notifications / webhooks for run status changes
- Multi-tenant federation (agents are scoped to a single Memory project)

## Decisions

### 1. Route prefix: `/acp/v1/` — top-level mount

**Decision:** Mount ACP routes at `/acp/v1/` directly on the Echo root, not nested under `/api/`.

**Rationale:** ACP clients expect a protocol-standard URL scheme. Nesting under `/api/` would mix Memory's internal API with the protocol spec. A clean prefix also allows independent versioning when ACP v0.3+ ships.

**Alternative considered:** `/api/acp/v1/` — rejected because it suggests the ACP routes are a subset of Memory's API rather than a protocol implementation.

### 2. Auth: reuse existing API token middleware

**Decision:** ACP endpoints use the same `Bearer emt_*` token auth as existing routes, requiring `agents:read` for GET endpoints and `agents:write` for run create/resume/cancel.

**Rationale:** No new auth mechanism needed. External integrators already create API tokens via the admin UI. The existing `auth.RequireAuth()` + `RequireAPITokenScopes()` middleware chain handles everything.

**Alternative considered:** ACP-specific API keys — rejected as unnecessary duplication. The `emt_*` tokens already support fine-grained scopes.

### 3. Agent naming: slug-normalized from definition name

**Decision:** ACP agent `name` (used in URLs like `/acp/v1/agents/:name`) is derived by normalizing `AgentDefinition.Name` to RFC 1123 DNS label format (lowercase, alphanumeric + hyphens, max 63 chars). The slug is stored alongside the definition for stable lookups.

**Rationale:** ACP requires DNS-label agent names for URL routing. Memory's definition names are free-form strings. A deterministic slug function ensures stable URLs.

**Implementation:** Add `ACPSlug` computed field to `AgentDefinition`. The slug normalizer: lowercases, replaces non-alphanumerics with hyphens, collapses consecutive hyphens, trims leading/trailing hyphens, truncates to 63 chars.

### 4. Event persistence: new `kb.acp_run_events` table

**Decision:** Create a dedicated `kb.acp_run_events` table with columns: `id`, `run_id`, `event_type`, `data` (JSONB), `created_at`. Events are written during streaming and also persisted for async/sync runs.

**Rationale:** ACP's `GET /runs/:runId/events` returns the complete event history as a JSON array. This requires persistent storage rather than a transient channel. A dedicated table avoids bloating `agent_run_messages` with protocol-level events.

**Alternative considered:** Store events as a JSONB array column on `agent_runs` — rejected because it creates unbounded row growth and makes concurrent writes dangerous.

### 5. SSE event bus: in-process `sync.Map` of channels

**Decision:** `ACPHandler` maintains a `sync.Map[string, chan ACPSSEEvent]` keyed by run ID. For `mode=stream`, the handler creates a channel, starts the run in a goroutine with a `StreamCallback` that publishes events to the channel (and persists to DB), and streams from the channel inline on the POST response.

**Rationale:** Simple, zero-dependency, works for single-server deployments. Memory runs are CPU-bound (LLM API calls), so the channel buffer only needs to handle event publication rate, not message backlog.

**Trade-off:** Does not support multi-server streaming (stream must be consumed from the same server that runs the agent). This is acceptable for v1 since Memory is single-server.

### 6. `cancelling` intermediate status

**Decision:** Add `RunStatusCancelling AgentRunStatus = "cancelling"` to entity.go. `DELETE /runs/:runId` sets status to `cancelling`, then the executor's context cancellation causes it to transition to `cancelled`.

**Rationale:** ACP defines a two-step cancel: `cancelling` (intent acknowledged) → `cancelled` (execution actually stopped). Memory currently goes directly to `cancelled`. The intermediate state improves observability and matches the ACP spec.

### 7. ACP status mapping

**Decision:**

| Memory status | ACP status |
|---|---|
| `queued` | `submitted` |
| `running` | `working` |
| `paused` | `input-required` |
| `success` | `completed` |
| `error` | `failed` |
| `cancelling` | `cancelling` |
| `cancelled` | `cancelled` |
| `skipped` | `completed` (with skip metadata) |

**Rationale:** Direct mapping except `queued` → `submitted` (ACP has no "queued" state, `submitted` is the closest pre-execution state) and `skipped` → `completed` (ACP has no "skipped" state; skip reason is included in metadata).

### 8. Session model: thin history-only

**Decision:** `kb.acp_sessions` stores `id`, `project_id`, `created_at`, `updated_at`. `kb.agent_runs` gets an `acp_session_id` nullable FK. `GET /sessions/:id` returns the session with `history` as a list of run URIs.

**Rationale:** ACP sessions group related runs. Memory does not need cross-run context injection for v1 — each run is independent. The thin model avoids complexity while satisfying the ACP session spec.

### 9. CLI: new `memory acp` command group

**Decision:** Add `tools/cli/internal/cmd/acp.go` with subcommands mirroring ACP endpoints. The CLI calls the Go SDK client at `pkg/sdk/acp/client.go`, which makes HTTP calls to `/acp/v1/` endpoints.

**Rationale:** The CLI is the primary interface for developers interacting with Memory. ACP CLI commands let developers test agent discovery and runs from the terminal without crafting raw HTTP requests.

### 10. MCP tools: 3 new tools in `mcp_tools.go`

**Decision:** Add `acp-list-agents`, `acp-trigger-run`, `acp-get-run-status` to the existing `MCPToolHandler`. These call internal service methods directly (not HTTP), same pattern as existing MCP tools.

**Rationale:** Enables Memory agents to discover and invoke other agents via ACP-style semantics through MCP. Using internal service calls (not HTTP) avoids network overhead and auth token management for agent-to-agent calls.

### 11. Message format translation

**Decision:** Translate Memory's internal message format (`{text: "...", function_calls: [...]}` JSONB) to ACP `Message` with `parts: []MessagePart` in the DTO layer. Text content maps to `{content_type: "text/plain", content: "..."}`. Tool calls map to parts with `TrajectoryMetadata`.

**Rationale:** Clean separation between internal storage format and protocol wire format. Translation happens only in `acp_dto.go`, keeping the rest of the codebase unchanged.

## Risks / Trade-offs

**[Single-server streaming]** → SSE streaming via in-process channels only works when the client connects to the server running the agent. Mitigation: acceptable for v1; future horizontal scaling would require Redis pub/sub or similar.

**[Slug collisions]** → Two agent definitions with similar names could produce the same slug. Mitigation: the slug normalizer will be deterministic, and a unique constraint on `(project_id, acp_slug)` in the DB will catch collisions at definition create/update time.

**[Event table growth]** → `kb.acp_run_events` will grow linearly with run count and event volume. Mitigation: add a retention policy (e.g., 90-day TTL) as a follow-up, and index on `(run_id, created_at)`.

**[ACPConfig backward compatibility]** → Expanding the `ACPConfig` struct adds new optional fields. Existing definitions with the old format will have zero values for new fields. Mitigation: all new fields are optional with `omitempty`; the manifest endpoint fills in sensible defaults from the definition's base fields.

**[ACP spec drift]** → ACP v0.2.0 may evolve. Mitigation: the `/acp/v1/` prefix allows us to add `/acp/v2/` later without breaking existing clients.

## Migration Plan

1. Deploy migration `00082_acp_sessions.sql` — creates 2 new tables, adds 1 column. Purely additive, no existing data changes.
2. Deploy server code — new routes are registered but have no traffic until clients start using them. Existing routes unchanged.
3. CLI update — new `memory acp` commands ship with the next CLI binary. Backward-compatible (no existing commands changed).
4. Rollback: drop `acp_session_id` column from `agent_runs`, drop `kb.acp_run_events` and `kb.acp_sessions` tables. New routes simply return 404 if code is rolled back.

## Open Questions

1. **Agent slug storage:** Should the ACP slug be stored as a new column on `kb.agent_definitions`, or computed on-the-fly in the DTO layer? A stored column enables DB-level unique constraint and faster lookups, but requires a backfill migration for existing definitions.
2. **Event retention:** What TTL should `kb.acp_run_events` rows have? 30 days? 90 days? Or follow the same retention as `agent_runs`?
3. **Rate limiting:** Should ACP endpoints have their own rate limits, or inherit the global API rate limiter?
