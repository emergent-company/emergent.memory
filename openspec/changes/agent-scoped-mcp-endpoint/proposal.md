## Why

Today an agent can be exposed over MCP only through a single-credential binding stored in `core.agent_mcp_shares`: one endpoint per agent, exactly one tool (`call_agent`), exactly one key, and no way to continue a conversation across calls. The product needs the agent to own its MCP endpoint from its own configuration surface, with many independently labeled keys, a fixed minimal session tool set (`call_agent` plus opt-in persistent sessions), and one-shot behavior preserved for every existing client. Separately, "choosing an agent" means two different things today — an allowlist filter on the project-wide tool server (`core.mcp_share_instances.allowed_agents`) versus a real credential binding (`core.agent_mcp_shares.agent_id`) — and that ambiguity is the discrepancy this change resolves.

## What Changes

- Make the per-agent endpoint agent-owned: one active endpoint per agent (`core.agent_mcp_endpoints`), created and revoked from the agent's own configuration surface, with `ON DELETE CASCADE` on `kb.agents` replacing today's missing FK on the hard-deleted agent row.
- Replace the single `token_id` FK with many labeled keys per endpoint (`core.agent_mcp_keys`): each key is individually labeled, revocable, rotatable, and last-used tracked. `token_id UUID NOT NULL UNIQUE` preserves the existing "one credential cannot authorize two agents" guarantee.
- Expose a fixed minimal session tool set on the endpoint — `call_agent` (unchanged one-shot) plus `start_session`, `continue_session`, `get_session`, `list_sessions` — with **no per-endpoint tool picking**; the agent configures its own internal tools. Session tools use the existing MCP result envelope, while `call_agent` stays byte-identical for back-compat.
- Add `core.agent_mcp_sessions` for session ownership and metadata; message history stays in the existing ADK session store under `session:<projectID>:<sessionRef>`, reusing the executor's already-implemented cross-run continuity keyed by `ExecuteRequest.SessionID`. Continue is a fresh `Execute` carrying a stable `SessionID`, never `Resume`.
- Keep session ids out of the authorization path: every session tool re-authorizes the key and verifies the session row belongs to it.
- Backfill `core.agent_mcp_endpoints` + `core.agent_mcp_keys` from active `core.agent_mcp_shares` rows (tokens and scopes untouched), so live keys keep working through the cutover; drop the old table in a later migration.
- Remove the agent allowlist/picker from project-scoped share instances: a project share instance scopes **tools only**, and users are directed to the agent-scoped endpoint for agent sharing. All other project-share behaviour is unchanged.
- Register the currently-unreachable agent-share gateway routes and add key and session management UI.

## Capabilities

### New Capabilities

- `agent-mcp-keys`: agent-owned endpoint lifecycle and the many-labeled-key lifecycle (create/list/revoke/rotate, last-used tracking, one-time secret reveal).
- `agent-mcp-sessions`: session lifecycle — start, continue, get, list — with key scoping, expiry, busy rejection, cumulative budget signalling, and interruption status.

### Modified Capabilities

- `agent-mcp-endpoint`: the tool catalog becomes the fixed five-tool session set; `call_agent` stays stateless while the session tools provide opt-in continuity; authorization moves to endpoint + key resolution with an `agent.Enabled` fast fail.
- `agent-mcp-shares`: the single-credential share lifecycle becomes the many-labeled-keys lifecycle on one endpoint; `core.agent_mcp_shares` is superseded and backfilled.
- `mcp-share-instances`: project share instances scope tools only; the agent allowlist and agent picker are removed and users are directed to the agent-scoped endpoint.
- `mcp-share-agent-scoping`: retired. The whole agent-allowlist capability — its discovery/execution gating, write validation, degrade-safely behavior, reference resolution, and run-inspection/definition/discovery filtering — is removed with the allowlist it governs.

## Impact

- New migration after `00148` (and after the in-flight `00150`), creating `core.agent_mcp_endpoints`, `core.agent_mcp_keys`, and `core.agent_mcp_sessions`, and backfilling the first two from `core.agent_mcp_shares`; mirrored in `apps/server/internal/testutil/schema.sql`.
- `apps/server/domain/mcp/`: endpoint and key stores, `AuthorizeAgentEndpoint`, session store, five-tool catalog and dispatch table, envelope results for the session tools; updates to `agent_endpoint_handler.go`, `agent_mcp_share.go`, `routes.go`.
- `apps/server/domain/agents/`: a session-aware run entry point (a new function — not an overload of the existing four-argument `RunAgentOnce`) and the continue path built on fresh `Execute` + `SessionID`.
- `apps/server/domain/mcp/share_instance.go`: remove `normalizeAgentAllowlist`, the `allowed_agents` plumbing, and all agent-allowlist enforcement (`InstanceDeniesTool` agent gating, `agentDeniedByAllowlist`, `filterAgentResult`, and the run-inspection/discovery filtering that existed only for the allowlist).
- **Capability retirement:** the `mcp-share-agent-scoping` capability is removed (REMOVED delta) in the same change that removes the project-instance agent allowlist, so no contradictory live requirements remain after archive.
- `apps/web-ui/gateway`: register the agent-share routes in `main.go` and add key and session management surfaces.
- **Stacking prerequisite:** this change stacks on the implemented-but-unarchived changes `add-agent-mcp-endpoint` and `add-mcp-share-instances`. Because `openspec/specs/` does not yet contain `agent-mcp-endpoint`, `agent-mcp-shares`, or `mcp-share-instances`, the MODIFIED deltas validate as valid but archive will refuse until those changes are archived first. Migration numbering MUST remain sequential after `00148` and `00150`.
