## Context

The sandbox domain (`apps/server/domain/sandbox/`) manages three kinds of Docker containers:

| Kind | Tracking | Reaping |
|---|---|---|
| Agent-session workspace | DB row (`AgentSandbox`, `lifecycle = ephemeral`) | `CleanupJob` after `expires_at` (30d default), or best-effort teardown closure |
| Persistent MCP server | DB row (`lifecycle = persistent`, `expires_at NULL`) | Explicit `Remove()` only |
| **Warm pool** | **process memory only** (`WarmPool.containers`) | `WarmPool.Stop()`, or `Resize`/`Acquire` paths — **nothing else** |

The warm pool is therefore the only container class with no durable record. Prod evidence: 184 orphans, 11 image versions, 48/day batches, 191 zero-byte volumes, no `WORKSPACE_*` configuration set (defaults in effect). Any restart that does not run the fx shutdown hook leaks the pool permanently and silently.

Docker labels already exist (`memory.workspace=true`, `workspace.type`, `workspace.volume`, and `memory.warm-pool=true` on warm containers), so container identity survives process death; it is the *reconciliation* that is missing.

## Goals / Non-Goals

**Goals**
- No sandbox container or volume can outlive its owning process's responsibility for it.
- Steady-state container count on any host is bounded by the configured pool target plus genuinely active workspaces.
- Reclamation is label-driven (survives restarts), safe (never destroys live work), and observable.

**Non-Goals**
- Changing DB-backed TTL semantics for agent-session workspaces.
- Reaping persistent MCP workspaces (`expires_at IS NULL` is deliberate).
- A general Docker garbage collector for unrelated containers, images, or build cache.
- Multi-replica warm-pool coordination beyond not destroying another live process's containers.

## Decisions

### D1: Reconciliation is label-driven, not memory-driven

**Choice**: Reconcile by enumerating Docker containers/volumes filtered on `memory.workspace=true`, then destroying those with no *live* owner.

**Rationale**: The bug is precisely that the authoritative record was process memory. Labels already exist on every container the pool creates, so Docker itself can supply the ground truth after any crash. Keeps working even if the process that created them is gone.

**Alternatives considered**: (a) Persist warm-pool containers as DB rows — heavier (schema semantics for containers that never serve a session; `CountActive` would need exclusions to avoid skewing `WORKSPACE_MAX_CONCURRENT`), and still needs a crash reconcile because rows are only as good as the writer. (b) Fixed lifetime cap with no reconcile — does not bound accumulation, just slows it. Chosen approach does not exclude (a) later, but does not require it.

### D2: Ownership is an explicit process-identity label, plus a DB cross-check

**Choice**: Every created container carries an ownership label identifying the creating process instance (host + PID + boot/instance token). The current process skips containers whose owner label matches its own identity; a DB row that is not stopped/errored protects the container it references.

**Rationale**: The owner label prevents self-destruction of the current pool (which is never in a DB row). It does **not** by itself distinguish a live peer from a dead predecessor — a second process always has a different identity. Peer safety therefore rests on the liveness lease in D7; the owner label is only the self-protection leg. Containers labelled by older versions (no owner) are treated as ownerless and reclaimed subject to the grace window.

**Alternatives considered**: Reap everything except in-memory IDs — fails after restart, because the new process's own pool would be in memory only from `Start` onward and pre-`Start` timing could destroy a just-created pool. DB-only ownership — blind to warm containers by construction (the actual defect).

### D2a: The container reference is persisted atomically with the workspace row

**Choice**: `CreateWorkspaceRequest` carries `ProviderWorkspaceID`, and `Service.Create` includes it in the single INSERT. The auto-provisioner passes the acquired/created container ID into that call instead of writing it in a later UPDATE.

**Rationale**: Between acquiring a warm container and writing its DB reference there is a window where a peer reconciler sees the container as ownerless-and-unreferenced; an hours-old warm container is already past grace and was previously destroyed. One atomic INSERT removes the window. (Defence in depth: the acquired container is also owned by the provisioning process's owner label, so that process's own reconciliation never touches it.)

**Handler path (documented, not atomic)**: `handler.go` creates the workspace row synchronously and only then provisions the container, so it cannot use the single-INSERT path. It instead bridges the acquire → DB-write window with the liveness lease: it refreshes the acquired container's lease immediately and then persists `provider_workspace_id` via a follow-up UPDATE. A warm container's lease is at most one heartbeat interval old at acquisition, so it remains fresh (`peer_live`) for up to `3 × WORKSPACE_OWNER_HEARTBEAT_MIN` — far longer than the UPDATE takes — so a peer reconciler spares it throughout. This claim is explicit in the code comments at the acquire and update sites.

**Alternatives considered**: Keep the follow-up UPDATE and rely on grace — insufficient: the container is older than the grace window by construction. Route the handler through `createWorkspaceWithContainer` — not possible without creating a second row; the handler's row pre-exists provisioning.

### D3: Grace period before destruction, configurable

**Choice**: Never destroy a container younger than a grace period (default 15 minutes, override `WORKSPACE_RECONCILE_GRACE_MIN`).

**Rationale**: A container created moments ago by a *starting* process (or a peer instance mid-`Start`) is indistinguishable from an orphan by labels alone. The grace window makes reconciliation safe against startup races, at the cost of orphan lifetime ≈ grace period, which is irrelevant at a 60-minute interval.

**Fail-safe direction**: The grace decision fails safe. A missing or unparseable creation timestamp is treated as *inside* the window (spare the resource), never as "old and eligible". Reaping only happens on a positively-parsed timestamp older than the grace window. The one exception is a warm-pool container whose liveness lease is positively stale/missing (D7).

**Alternatives considered**: Reap immediately — risks destroying a live pool during overlapping restarts (rolling deploy: old process exiting, new process starting). Grace period is the cheap, standard fix.

### D4: Startup reconcile plus the existing cleanup interval

**Choice**: Run reconciliation once after provider registration at startup, then on the existing `CleanupJob` cycle (`WORKSPACE_CLEANUP_INTERVAL_MIN`, default 60m).

**Rationale**: Startup is when a predecessor's orphans are known. The startup pass immediately reclaims orphans that are *not* protected by a fresh lease — including all pre-fix containers, which carry no lease at all. It does **not** reclaim a peer that crashed moments ago: that peer's lease is still fresh (D7), so the startup pass spares its warm containers and they are reclaimed on a later cleanup cycle, once the lease is older than `3 × WORKSPACE_OWNER_HEARTBEAT_MIN` (i.e. after roughly one cleanup interval, up to ~60 min by default). "Self-heals within seconds" applies only to owners whose lease has already aged out. Reusing the existing ticker avoids a second scheduler. Gated on `Sandbox.IsEnabled()` (`ENABLE_AGENT_SANDBOXES`), consistent with the current `startCleanupJob` guard.

**Alternatives considered**: Interval only — leaves orphans running for a full hour after every deploy, which is exactly the accumulation pattern observed. Separate ticker — duplicate lifecycle to maintain.

### D5: The warm pool converges its own view; Docker-level surplus belongs to reconciliation

**Choice**: On `Start`, the pool converges the containers it already tracks in memory to the per-image target (destroying surplus it owns) and creates only the shortfall; the `Acquire` staleness path destroys the discarded container before creating its replacement.

**Rationale**: `WarmPool` only knows the containers in its in-memory slice; it cannot enumerate Docker and therefore cannot reclaim containers it never tracked. Its job is to stop *creating* surplus and to destroy what it does track. Reclaiming Docker-level surplus (predecessors, previously-dropped containers) is the reconciler's job (D1/D4). Together they bound the host count. Making `Start` create only the shortfall also means a repeated `Start` no longer duplicates the pool.

**Alternatives considered**: Only reconcile periodically — leaves transient surplus; harmless given D4 but trivially avoidable. Have `Start` enumerate Docker and reconcile — duplicates D1's responsibility and couples the pool to the Docker client.

### D6: MCP persistent containers are excluded explicitly, not implicitly

**Choice**: Reconciliation skips a container when (a) a live DB row references it and that row is `lifecycle = persistent` or `container_type = mcp_server`, or (b) the container itself carries the `workspace.type = mcp_server` label (covers a persistent container even if its row is transiently absent).

**Rationale**: They legitimately have `expires_at NULL` and are started on boot by `ListPersistentMCPServers`. Skipping them explicitly — and documenting why — prevents a future reader from "fixing" the exclusion, and prevents a reaper from deleting hosted MCP servers.

**Note on a rejected label**: an earlier draft also checked a `workspace.lifecycle` label. That label is never written at create time (Docker labels are immutable and the provider is not given the lifecycle), so the check was dead code and has been removed rather than left as fake coverage. Persistent exclusion therefore rests on the DB row and the `workspace.type` label above.

### D7: Peer liveness is a refreshed per-container heartbeat lease

**Choice**: Each warm-pool process refreshes a liveness lease for every container it currently tracks, every `WORKSPACE_OWNER_HEARTBEAT_MIN` (default 2 minutes). Reconciliation spares a warm-pool container whose lease is fresher than `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`; a missing or stale lease means the owner is presumed dead and the container is reapable. Non-warm containers keep the owner/DB/grace logic.

Because Docker container/volume labels are immutable after creation, the lease cannot literally be a label mutated on the container. It is a dedicated Docker volume per tracked container, labelled `memory.owner.heartbeat = <container ID>`; its creation time is the heartbeat timestamp. Refresh creates a new lease volume and then removes older ones for the same container, so a lease is always present during refresh (no reader-visible gap).

**Rationale**: Ownership alone cannot tell a live peer from a dead predecessor (D2). Warm-pool containers are idle for hours, so a fixed grace window cannot protect them. A heartbeat is the minimum mechanism that makes "owner is still alive" observable after process death: predecessors stop beating, so their leases go stale and their containers are reclaimed; a live peer keeps beating, so its pool is spared. Tying refresh to the pool's *tracked* set (not merely to the process) means a container the pool drops is no longer kept alive by the heartbeat.

**Lease lifecycle (no leak)**: Lease volumes are not `memory.workspace=true`, so the workspace volume sweep never sees them; they are cleaned up explicitly both on normal teardown (`DestroySandboxContainer` removes the container's leases) and by the reconciler, which removes any lease whose container ID is absent from the current container list. That bounds lease count even when an owner dies mid-cycle and no longer prunes its own old leases. Lease removal is label-scoped to `memory.owner.heartbeat` only and is idempotent.

**Fail-safe lease timestamps**: An unparseable lease timestamp yields the zero time. The reconciler treats a zero lease timestamp as *unknown* and spares the container (`heartbeat_unknown`), never as stale — consistent with the grace-period fail-safe (D3).

**Alternatives considered**: DB rows for warm containers (heavier, skews `CountActive`) — still needs liveness. A fixed long TTL — delays the leak fix instead of bounding it. Process-wide (not per-container) heartbeat — would keep a dropped container alive as long as any container is tracked. Relying on `removeOlderHeartbeats` alone to prune leases — leaks a lease per crash, because a dead owner cannot run its own pruning.

### D8: Container enumeration is mandatory; failure aborts the pass

**Choice**: If `ListSandboxContainers` fails, reconciliation aborts before any destruction. If `ListContainerHeartbeats` fails, the pass still runs but every warm-pool container is spared (`heartbeat_unknown`).

**Rationale**: The container list is what builds the protected-volume set. Continuing the volume sweep with an empty list would destroy the workspace volume of a live container — data loss. Volume enumeration failure, by contrast, is safe to skip (nothing is destroyed), and heartbeat-lookup failure fails safe for warm-pool containers only.

**Alternatives considered**: Treat all enumeration as best-effort — the observed data-loss bug. Abort on any error — safe but needlessly skips the TTL reaper; not warranted.

## Risks / Trade-offs

- **Destroying live work if ownership detection is wrong** → mitigated by D2 (current-process identity + DB active-row cross-check), D7 (peer liveness lease), D3 (fail-safe grace period), D8 (abort if the container list is unavailable), and the persistent-MCP exclusion. Unit tests cover each skip path.
- **Heartbeat lease churn** → one small volume per tracked warm container, refreshed at `WORKSPACE_OWNER_HEARTBEAT_MIN`; old leases are removed on refresh. Trivial volume count (pool target) at a 60-minute reconcile interval.
- **Docker API surface growth** → enumeration reuses the existing Docker client and `Destroy` path; no new dependency.
- **Cost of enumeration with many containers** (190 now) → single `ContainerList`/`VolumeList` by label per cycle; negligible against a 60-minute interval. Guard with a bounded context timeout.
- **Existing 184 prod orphans predate the fix and carry no ownership label** → they are ownerless and will be reclaimed by the first post-deploy reconcile; until then they consume resources. A one-off operator cleanup (tasks §5) reclaims them immediately at deploy time.
- **Two concurrent server instances** (rolling deploy) → the grace period plus ownership label prevent cross-instance destruction; a container owned by a peer that then crashes becomes ownerless and is reclaimed on the next cycle.

## Migration Plan

1. Ship reconciliation behind the existing sandbox-enabled guard; no schema or config migration required (new grace-period knob is optional with a default).
2. Deploy; the startup pass reclaims the accumulated ownerless orphans as a side effect.
3. Verify: labelled container count converges to `WORKSPACE_WARM_POOL_SIZE` (+ active workspaces), and orphan volumes drop to zero.
4. Optional immediate relief before/at deploy: run the documented one-off label-filtered cleanup (tasks §5).
5. No rollback action needed — reverting the code simply restores previous behaviour; nothing is deleted that is not an orphan.

## Open Questions

- Should reconciliation additionally emit a metric/alarm when the orphan count exceeds a threshold (e.g. reuse `WORKSPACE_ALERT_THRESHOLD_PCT` semantics) rather than only logging? Deferred to review; logging is the minimum bar in this change.
- Should warm-pool containers gain DB rows for visibility in the existing dashboards even though reconciliation is label-driven? Deferred; not required to stop the leak.
