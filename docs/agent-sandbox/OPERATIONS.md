# Agent Sandbox Operations Guide

Operational procedures for spotting and reclaiming abandoned sandbox resources.

## Identifying an abandoned hosted MCP server

Hosted MCP servers are persistent: they have no `expires_at`, so the ephemeral
TTL never touches them. A server that was created and then abandoned keeps its
container, volume, and memory until it is reclaimed.

### Inspect the hosted MCP listing

```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://api.emergent-company.ai/api/v1/mcp/hosted
```

Each entry exposes `last_used_at` (updated on every JSON-RPC call). Compute the
idle age from that timestamp to decide whether a server is abandoned. The
listing does not require the idle policy to be enabled.

### Reclaim an individual server

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://api.emergent-company.ai/api/v1/mcp/hosted/<workspace-id>
```

Explicit deletion destroys the container and removes the row regardless of the
idle policy.

### Reclaim automatically (opt-in)

Set `WORKSPACE_PERSISTENT_IDLE_TTL_DAYS=N` to let the cleanup cycle destroy
servers idle for more than N days. Keep `N=0` (default) to disable the policy.
The cleanup cycle logs a summary each pass:

```text
idle MCP reclamation cycle complete  reclaimed=2 skipped=5 failed=1
```

- `reclaimed` — container destroyed and row deleted.
- `skipped` — not eligible: in-flight `creating`/`stopping`, used within the
  window, or touched (a call landed between the candidate scan and reclamation)
  so reclamation was abandoned.
- `failed` — destroy, row deletion, or the pre-reclaim re-read failed; the row
  is kept for the next pass.

Each per-server destroy and row delete is bounded by a 30-second timeout, so a
hung provider cannot stall the cleanup cycle.

Per-server log lines identify the server and its idle age, for example:

```text
reclaimed idle persistent MCP server  sandbox_id=... idle_days=42
```

## Identifying an un-torn-down sandbox

Every agent run tears down its sandbox exactly once on every exit path,
including panic. If a sandbox still appears non-stopped, look for one of these:

1. **The owning run is still active** — a run in `submitted` (queued),
   `working` (running), or `cancelling` still owns its sandbox. No action.
2. **The owning run is no longer active but the row is still non-stopped** —
   the startup recovery hook should have logged:

   ```text
   recovered orphaned sandbox row on startup  sandbox_id=... previous_status=ready reason="owning run is no longer active after restart"
   ```

   Recovery transitions the row to `stopped`; the row's container and volume
   are then reclaimed by the label-based orphan reconciler. Recovery is
   idempotent and logs each row it transitions.

### Inspect sandbox rows

```bash
# Non-stopped agent sandboxes and their owning run status.
psql "$DATABASE_URL" -c "
  SELECT s.id, s.status, s.provider_workspace_id, r.status AS run_status
  FROM kb.agent_sandboxes s
  LEFT JOIN kb.agent_runs r ON r.id = s.agent_session_id
  WHERE s.status NOT IN ('stopped','error')
  ORDER BY s.created_at;"
```

Look at startup logs for `recovered orphaned sandbox row on startup` to confirm
the recovery hook ran. A row that is `stopped` but whose container still exists
is reclaimed by the orphan reconciler, not by the teardown path.
