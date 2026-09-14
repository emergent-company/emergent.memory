## Context

The iOS client already talks to one backend endpoint today: the token mint on the admin server (`agent/admin.py`, port 8080, `X-API-Key` = `TOKEN_API_KEY`), whose URL and key are delivered to the app via the QR config. The agent config (including which MCP servers each agent has) lives in `data/agents.db`, readable through `agent.api.store.Store`. Memory itself lives in the Emergent Memory service (reachable from home2 at `MEMORY_URL` = `http://100.75.139.71:5300`, auth `Bearer MEMORY_TOKEN`, scoped by `MEMORY_PROJECT_ID`). See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Expose a read-only, memory-aware API the iOS app can call with the credentials it already has.
- Add a native SwiftUI browsing surface (NavigationStack + List) for reading an agent's memories.
- Keep the change additive and non-breaking; no new client secret or endpoint discovery mechanism.

**Non-Goals:**
- No memory editing/creation from the app (read-only, per spec).
- No change to how the agent itself writes memory (`remember`/`entity-create` stay as-is).
- No migration of the memory service onto home2 (out of scope; it remains at mcj-one).

## Decisions

### 1. Memory API lives in `agent/admin.py` (port 8080), not the FastAPI control plane

The iOS app already knows `admin.py`'s URL and key (the token endpoint). Adding `/api/memories` there means zero new client config — the app calls the same host with the same `X-API-Key`.

- **Alternative considered:** add it to `agent/api/main.py` (FastAPI, port 8081), which is the more "agent info"-shaped server. Rejected because the app has no knowledge of :8081 or its `API_KEY`, and it would force a second endpoint + key into `AlfredConfig` and the QR payload for a single read-only feature.
- `admin.py` already imports `Store` (used by the DB-backed prompt editor), so it can read agent config and MCP servers without new plumbing.

### 2. Memory capability = the agent references the `memory` MCP server

"Does agent X have memory?" is answered by checking the agent's `tools.mcp` for a ref to the MCP server whose `name == "memory"` (the same signal `factory.build_tools` uses to wire Diane's tools). No new flag or schema field is needed.

- **Alternative considered:** a dedicated `has_memory` boolean on the agent. Rejected as redundant — the MCP ref is already the source of truth.

### 3. Proxy via the memory service's REST search, not MCP

The browser is read-only, so it proxies a search/recall call directly to the memory service's REST search endpoint (the same primitive the OpenCode plugin uses for `search-hybrid`), using `MEMORY_URL` + `Bearer MEMORY_TOKEN` + `MEMORY_PROJECT_ID` from env.

- **Alternative considered:** reuse the MCP endpoint (`/api/mcp`) with an MCP client and call `search-hybrid`. Rejected — standing up an MCP session per request is heavier than a single REST call, and MCP stays reserved for the agent's live tool use.
- Note: the browsing path uses search/recall, which works today; the heavyweight `remember` extraction pipeline (currently 503) is not involved.

### 4. iOS uses native SwiftUI navigation + list, gated on capability

A `NavigationStack` with a `List` (`.searchable` for filtering) and a detail push for full content. A small `AgentInfoClient` calls the same host as the token endpoint. The memories entry point is shown only when the selected agent is memory-capable (fetched once per agent selection).

- **Alternative considered:** reusing the existing `AgentToConnect`/`AlfredConfig` plumbing to carry a separate memory endpoint URL. Rejected — deriving the memory URL from the already-known token endpoint host keeps config surface unchanged.

## Risks / Trade-offs

- **Cross-host dependency (mcj-one memory)** — admin.py on home2 reaches memory over Tailscale. Same risk as Diane; a memory outage makes the memories list error (not crash). → Mitigation: render an error state in the list view; browsing is non-critical.
- **`remember` pipeline is down (503)** — memories written via `remember` won't be produced until the provider is fixed, but already-stored memory content is unaffected. → Mitigation: out of scope here; surfaced as a known memory-service gap.
- **Auth surface** — the memory endpoint reuses the token endpoint's key, so anyone with `TOKEN_API_KEY` can read memories. → Mitigation: same Tailscale-only trust boundary as today; no widening.
- **admin.py is legacy stdlib** — adding a feature to it grows the legacy server. → Trade-off accepted for the zero-client-config win; noted for the eventual Go control-plane UI.

## Migration Plan

1. Add `/api/memories` (+ capability) to `admin.py`, read-only, auth via `X-API-Key`.
2. Deploy: restart `alfred-admin.service` on home2 (no DB migration — additive endpoint).
3. Add iOS views + client, rebuild the app.
4. Rollback: stop/rollback the app binary; the endpoint is additive and can be reverted independently.

## Open Questions

None — the remaining unknowns (exact REST path, pagination/limit) are implementation details that don't change the specs or task breakdown.
