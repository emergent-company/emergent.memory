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

`memory.owner` is written at create time and survives process death, so after a crash
you can tell which resources the current process owns.

## Inspecting labelled resources

```bash
# All sandbox containers (including stopped), with labels
docker ps -a --filter label=memory.workspace=true \
  --format 'table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Labels}}'

# Warm-pool containers only
docker ps -a --filter label=memory.warm-pool=true \
  --format 'table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Generated}}'

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

1. Enumerates containers and volumes labelled `memory.workspace=true`.
2. Skips resources owned by the current process (`memory.owner` matches).
3. Skips containers referenced by a workspace record that is not `stopped`/`error`.
4. Skips persistent MCP containers (`lifecycle=persistent` / `container_type=mcp_server`).
5. Skips resources younger than `WORKSPACE_RECONCILE_GRACE_MIN` (default 15 minutes).
6. Destroys the remainder, container and its `workspace.volume` together, and logs each
   destruction with identifier, labels, age and reason.

### Verifying after a deploy

```bash
# Labelled container count should converge to WORKSPACE_WARM_POOL_SIZE (+ active workspaces)
docker ps --filter label=memory.workspace=true -q | wc -l

# Labelled volume count should match labelled container count; no zero-byte orphans
docker volume ls --filter label=memory.workspace=true
```

Server logs (component `sandbox-reconcile`) report a summary per pass:
`reconciled=… skipped=… failed=… grace_period=…`.

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

## Troubleshooting

- **Container count keeps growing across deploys** — reconciliation is disabled
  (`WORKSPACE_RECONCILE_ENABLED=false`) or the provider is unavailable. Check startup logs
  for `reconciliation skipped: sandbox resource manager unavailable`.
- **An active workspace was destroyed** — this should not happen; captures are skipped by
  owner, DB-active, persistent and grace checks. Report with the `sandbox-reconcile` log
  lines (labels + reason) for the affected container.
- **Orphans not reclaimed immediately** — they may be inside the grace window; they will be
  reconsidered on the next pass once older than `WORKSPACE_RECONCILE_GRACE_MIN`.
