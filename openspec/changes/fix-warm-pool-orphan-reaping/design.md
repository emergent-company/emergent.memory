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

**Choice**: Every created container carries an ownership label identifying the creating process instance (host + PID + boot/instance token). A container is *owned* if that identity matches the current process, or a DB row references it as active.

**Rationale**: Prevents self-destruction of the current pool (which is never in a DB row) and avoids reaping a concurrent process's containers. Unlabelled-with-ownership containers from older versions are treated as ownerless and reclaimed only after the grace period.

**Alternatives considered**: Reap everything except in-memory IDs — fails after restart, because the new process's own pool would be in memory only from `Start` onward and pre-`Start` timing could destroy a just-created pool. DB-only ownership — blind to warm containers by construction (the actual defect).

### D3: Grace period before destruction, configurable

**Choice**: Never destroy a container younger than a grace period (default 15 minutes, override `WORKSPACE_RECONCILE_GRACE_MIN`).

**Rationale**: A container created moments ago by a *starting* process (or a peer instance mid-`Start`) is indistinguishable from an orphan by labels alone. The grace window makes reconciliation safe against startup races, at the cost of orphan lifetime ≈ grace period, which is irrelevant at a 60-minute interval.

**Alternatives considered**: Reap immediately — risks destroying a live pool during overlapping restarts (rolling deploy: old process exiting, new process starting). Grace period is the cheap, standard fix.

### D4: Startup reconcile plus the existing cleanup interval

**Choice**: Run reconciliation once after provider registration at startup, then on the existing `CleanupJob` cycle (`WORKSPACE_CLEANUP_INTERVAL_MIN`, default 60m).

**Rationale**: Startup is when predecessors' orphans are known — cleaning them immediately means a crashed restart self-heals within seconds rather than up to an hour. Reusing the existing ticker avoids a second scheduler. Gated on `Sandbox.IsEnabled()` (`ENABLE_AGENT_SANDBOXES`), consistent with the current `startCleanupJob` guard (`module.go:192`).

**Alternatives considered**: Interval only — leaves orphans running for a full hour after every deploy, which is exactly the accumulation pattern observed. Separate ticker — duplicate lifecycle to maintain.

### D5: The warm pool converges instead of leaking at the edges

**Choice**: On `Start`, destroy surplus containers for managed images beyond the target and treat ownership labels as authoritative; in the `Acquire` staleness path, destroy the discarded container before creating its replacement.

**Rationale**: Fixes the secondary growth paths inside a *single* process, not just cross-restart orphaning. Container creation stays bounded by the configured target.

**Alternatives considered**: Only reconcile periodically — leaves transient surplus; harmless given D4 but trivially avoidable.

### D6: MCP persistent containers are excluded explicitly, not implicitly

**Choice**: Reconciliation skips containers that a DB row marks `lifecycle = persistent` or whose `container_type = mcp_server`.

**Rationale**: They legitimately have `expires_at NULL` and are started on boot by `ListPersistentMCPServers`. Skipping them explicitly — and documenting why — prevents a future reader from "fixing" the exclusion, and prevents a reaper from deleting hosted MCP servers.

## Risks / Trade-offs

- **Destroying live work if ownership detection is wrong** → mitigated by D2 (current-process identity), D3 (grace period), DB active-row cross-check, and the "never reap workspaces with status active/running" guard. Unit tests must cover each skip path.
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
