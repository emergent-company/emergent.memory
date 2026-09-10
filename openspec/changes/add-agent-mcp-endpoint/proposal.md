## Why

A user wants to hand a single configured Memory agent to an external LLM as an MCP server: point the client at a URL with one generated key and get exactly one tool that sends the agent a message and returns its reply. Today the project MCP endpoint exposes the whole tool catalog, agent-execution tools cannot be granted through a share, and there is no per-agent credential — so there is no way to expose one agent on its own.

## What Changes

- Add a per-agent MCP endpoint `/api/mcp/agents/:agentId` exposing exactly one tool, `call_agent`, over the existing MCP JSON-RPC transport (`initialize` / `tools/list` / `tools/call`).
- Add a per-agent share credential stored in a new `core.agent_mcp_shares` table: an API token bound to one project + agent, created, listed, revoked, and rotated by project admins.
- `call_agent` runs the bound agent synchronously for a single `message` argument and returns the assistant's reply text. Each call is an independent run (stateless).
- Authenticate the endpoint with the existing `X-API-Key` / `Authorization: Bearer` mechanism; a request is authorized only if its token is actively bound to the agent named in the URL.
- Bound the run with a capped step/time budget so a call fits within an MCP client's `tools/call` timeout; map run failures and human-in-the-loop pauses to structured tool errors.
- **No breaking change** to the existing `/api/mcp` project endpoint or its share instances.

## Capabilities

### New Capabilities

- `agent-mcp-endpoint`: the per-agent MCP endpoint contract — a single `call_agent` tool, synchronous stateless replies, and agent-bound credential authentication.
- `agent-mcp-shares`: lifecycle (create/list/revoke/rotate) of per-agent MCP share credentials.

### Modified Capabilities

<!-- None: the project MCP endpoint and its share instances are unchanged. -->

## Impact

- New migration `apps/server/migrations/00145_create_agent_mcp_shares.sql` (new `core.agent_mcp_shares` table), mirrored in `apps/server/internal/testutil/schema.sql`.
- `apps/server/domain/mcp/`: new agent-endpoint handler and share store/service, route registration; reuses the existing JSON-RPC response/error helpers.
- `apps/server/domain/agents/`: a blocking "run once and return reply text" helper reusing the executor and run-message extraction.
- Consumed by the gateway/UI change `add-agent-mcp-share-ui` in `memory.web-ui`.
- Stacks on the in-flight `add-mcp-share-instances` change (shares migration `00144`) to keep migration numbering sequential.
