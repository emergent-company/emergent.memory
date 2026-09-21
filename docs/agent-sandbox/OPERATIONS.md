# Agent Sandbox Operations

Operator guide for inspecting and cleaning labelled sandbox containers and volumes,
and for understanding orphan reconciliation.

## Labels

Every container and workspace volume created by the server carries:

| Label              | Where           | Meaning                                                  |
| ------------------ | --------------- | -------------------------------------------------------- |
| `memory.workspace` | containers, volumes | `true` on all sandbox resources (reconciliation filter) |
| `workspace.type`   | containers, volumes | `agent_sandbox`, `mcp_server`, `snapshot`             |
| `workspace.volume` | containers      | name of the container's workspace volume                  |
| `memory.owner`     | containers, volumes | owning process identity (`host:pid:start-token`)      |
| `memory.warm-pool` | containers      | `true` on warm-pool (pre-booted) containers               |
| `memory.owner.heartbeat` | lease volumes | value is a container ID; volume creation time is the lease timestamp |

`memory.owner` is written at create time and survives process death, so after a crash
you can tell which resources the current process owns. It only proves *who created* a
container, not whether that process is alive — that is the job of the heartbeat lease.

### Liveness lease

Docker labels are immutable, so a warm-pool process cannot mutate a heartbeat label on
its own containers. Instead, every `WORKSPACE_OWNER_HEARTBEAT_MIN` (default 2) each
process creates a small lease volume per tracked container, labelled
`memory.owner.heartbeat=<container ID>`. The volume's creation time is the heartbeat.
Old leases for the same container are removed as newer ones are written. A predecessor
that dies stops writing leases, so its leases age out and its containers become
reapable; a live peer keeps writing, so its pool is spared. A container the pool drops
is no longer beaten, so it cannot be kept alive by a running owner.

## Inspecting labelled resources

```bash
# All sandbox containers (including stopped), with labels
docker ps -a --filter label=memory.workspace=true \
  --format 'table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Labels}}'

# Warm-pool containers only
docker ps -a --filter label=memory.warm-pool=true \
  --format 'table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.CreatedAt}}'

# Count of running labelled containers
docker ps --filter label=memory.workspace=true -q | wc -l

# All labelled sandbox volumes
docker volume ls --filter label=memory.workspace=true

# Count of labelled volumes
docker volume ls --filter label=memory.workspace=true -q | wc -l
```

## Automatic reconciliation

Reconciliation runs once at startup (gated on `ENABLE_AGENT_SANDBOXES`) and then on every
`WORKSPACE_CLEANUP_INTERVAL_MIN` tick. It:

1. Enumerates containers and volumes labelled `memory.workspace=true`. **If container
   enumeration fails the pass aborts with nothing destroyed** (the container list is
   what protects live volumes).
2. Reads per-container liveness leases (`memory.owner.heartbeat`). If this fails, all
   warm-pool containers are spared for the pass.
3. Skips resources owned by the current process (`memory.owner` matches).
4. Skips containers referenced by a workspace record that is not `stopped`/`error`.
5. Skips persistent MCP containers (DB row `lifecycle=persistent` / `container_type=mcp_server`,
   or the container's `workspace.type=mcp_server` label).
6. Skips a warm-pool container whose liveness lease is fresher than
   `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`. A missing/stale lease means the owner is presumed
   dead and the container is reapable.
7. Skips resources whose creation time is unknown (unparseable), and resources younger
   than `WORKSPACE_RECONCILE_GRACE_MIN` (default 15 minutes).
8. Destroys the remainder, container and its `workspace.volume` together, and logs each
   destruction with identifier, labels, age and reason.
9. Removes liveness-lease volumes whose container ID is absent from the container list,
   logging each with reason `container_gone`. This is label-scoped to
   `memory.owner.heartbeat` only (never workspace or `emergent_*` volumes) and is
   idempotent; leases of live containers are never removed.

### Verifying after a deploy

```bash
# Labelled container count should converge to WORKSPACE_WARM_POOL_SIZE (+ active workspaces)
docker ps --filter label=memory.workspace=true -q | wc -l

# Labelled volume count should match labelled container count; no zero-byte orphans
docker volume ls --filter label=memory.workspace=true
```

Server logs (component `sandbox-reconcile`) report a summary per pass:
`reconciled=… skipped=… failed=… grace_period=…`. Skip reasons include `owned`, `active`,
`persistent`, `peer_live`, `heartbeat_unknown`, `grace_period`, `in_use` and `not_primary`.

### Knobs

| Variable                        | Default | Effect                                                        |
| ------------------------------- | ------- | ------------------------------------------------------------- |
| `WORKSPACE_RECONCILE_ENABLED`   | `true`  | Disable the pass entirely (not recommended)                    |
| `WORKSPACE_RECONCILE_GRACE_MIN` | `15`    | Minutes before an ownerless resource may be destroyed          |
| `WORKSPACE_OWNER_HEARTBEAT_MIN` | `2`     | Lease refresh interval; staleness threshold is `3 ×` this value |

## One-off cleanup of pre-fix orphans

Containers created before `memory.owner` was introduced have no owner label. They are
eligible for reconciliation after the grace period, but can be removed immediately:

```bash
# 1. Preview: labelled containers WITHOUT an owner label (pre-fix orphans)
for id in $(docker ps -aq --filter label=memory.workspace=true); do
  owner=$(docker inspect -f '{{ index .Config.Labels "memory.owner" }}' "$id")
  if [ -z "$owner" ]; then docker inspect -f '{{.ID}} {{.Name}}' "$id"; fi
done

# 2. Remove each previewed container (also removes its labelled volume)
#    Re-check a container's workspace.volume before removing.
id=<container-id>
vol=$(docker inspect -f '{{ index .Config.Labels "workspace.volume" }}' "$id")
docker rm -f "$id"
[ -n "$vol" ] && docker volume rm "$vol"

# 3. Any labelled volume with no running container
docker volume ls -q --filter label=memory.workspace=true | while read -r v; do
  inuse=$(docker ps -q --filter volume="$v")
  [ -z "$inuse" ] && echo "orphan candidate: $v"
done
```

Do **not** remove `emergent_pgdata` or any `emergent-*` infra volume; only volumes
labelled `memory.workspace=true` are sandbox workspace volumes.

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

## Troubleshooting

- **Container count keeps growing across deploys** — reconciliation is disabled
  (`WORKSPACE_RECONCILE_ENABLED=false`) or the provider is unavailable. Check startup logs
  for `reconciliation skipped: sandbox resource manager unavailable`.
- **An active workspace was destroyed** — this should not happen; captures are skipped by
  owner, DB-active, persistent and grace checks. Report with the `sandbox-reconcile` log
  lines (labels + reason) for the affected container.
- **Orphans not reclaimed immediately** — they may be inside the grace window, or a peer
  that just crashed may have a still-fresh lease (startup spares it). They are reconsidered
  on a later pass once older than `WORKSPACE_RECONCILE_GRACE_MIN` and their lease exceeds
  `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`.
- **Lease volumes left after a crash** — a dead owner can no longer prune its own leases,
  so the reconciler removes leases whose container is gone (step 9 above). They are visible
  only via `docker volume ls --filter label=memory.owner.heartbeat`.
