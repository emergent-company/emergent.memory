## Why

Production (`emergent-memory`, VM 220) is accumulating **orphaned warm-pool workspace containers and volumes that nothing can ever reclaim**:

- `docker ps` on prod shows **184** running containers named `memory-ws-<nano-timestamp>`, oldest up 11 days, all carrying `memory.warm-pool=true` and `memory.workspace=true`, creation batched **48 / 48 / 48 / 38** across Sep 7–10 and **2** on Sep 16.
- Orphans span **11 different image versions** (`0.62.1` → `0.82.0`), i.e. they survive every deploy and are never cleaned up.
- A further **191 volumes** named `memory-workspace-<nano-timestamp>` exist, all **0 bytes** — the volumes of those same orphans. `emergent_pgdata` is 5.6 GB; `/var/lib/docker` is 56 GB.
- Prod sets **no** `WORKSPACE_*` env vars, so defaults apply (`WarmPoolSize: 2`, `CleanupIntervalMin: 60`, `DefaultTTLDays: 30`). An 11-day accumulation of 184 containers from a pool of 2 is therefore not a config problem — it is an unreclaimable-state problem.

**Root cause.** Warm-pool containers are tracked **only in process memory**.

- `WarmPool.Start` (`apps/server/domain/sandbox/warm_pool.go:128`) creates `Size` (+1 per extra image) containers and appends them to the in-memory `wp.containers` slice. No DB row is written — unlike agent-session workspaces, which go through `service.Create`.
- `WarmPool.Stop` (`warm_pool.go:187`) destroys only what is still present in that slice, and `startWarmPool` (`module.go:231`) wires it to the fx shutdown hook. Any process death that skips a graceful `Stop` — SIGKILL, OOM, deploy replacement, hot-reload restart, rollback — leaks **every** warm container it created, permanently.
- Nothing else ever looks at them. `CleanupJob.cleanupExpired` (`cleanup.go:115`) reaps strictly from the DB via `Store.ListExpired` (`store.go:138`), whose predicate requires `expires_at IS NOT NULL`. Warm-pool containers have no row at all, so the 60-minute reaper is structurally blind to them. `GVisorProvider.Health` (`gvisor_provider.go:837`) only *counts* containers; it never reconciles.
- Because `GVisorProvider.Create` always creates a labelled volume (`gvisor_provider.go:148`, `:152`) and only `Destroy` removes it (`:310-333`), each leaked container also leaks its zero-byte volume.

Net effect: every server restart with a non-graceful exit permanently costs 2 containers + 2 volumes, forever, with no alarm. The count only grows.

## What Changes

- **Durable identity for every sandbox container.** Containers created by the warm pool SHALL carry the same reconciliation labels as other workspaces, extended with an owning-process/instance identity label so a live process can recognise its own containers after restart-free operation.
- **Label-driven orphan reconciliation.** A new reconcile pass lists Docker containers and volumes labelled `memory.workspace=true` and destroys any that are neither accounted for by an active DB row nor owned by the current process — after a grace period. Volume removal uses the container's `workspace.volume` label.
- **Startup + periodic reconciliation.** Reconciliation runs once at startup (so a crash-restart cleans up its predecessor immediately) and then on the existing cleanup interval, alongside `cleanupExpired`.
- **Bounded, self-healing warm pool.** The pool SHALL converge to its configured target on every start and reconcile cycle, destroying surplus containers instead of orphaning them, including stale-image containers discarded by the `Acquire` staleness path.
- **Regression protection for the leak class.** Graceful shutdown keeps removing its own pool, and any residue from an ungraceful exit is reclaimed by the next start — so steady-state container count is bounded by the configured pool target regardless of restart pattern.
- **Observability.** Reconciliation SHALL log each destroyed orphan with its identifier, labels, and age, and SHALL expose counts (reconciled, skipped, failed) so future drift is detectable rather than silent.

## Capabilities

### New Capabilities

- `agent-sandbox-lifecycle`: durable, label-based ownership and reconciliation of all sandbox containers and volumes (warm pool included), so container/volume state converges to the configured pool target across restarts and crashes instead of leaking.

### Modified Capabilities

- `agent-sandbox-providers`: the gvisor provider additionally exposes label-scoped container/volume enumeration used by reconciliation (health reporting keeps its current counting behaviour, extended to report orphan counts).

## Impact

### Code Changes
- `apps/server/domain/sandbox/warm_pool.go` (owning-process identity label; bounded convergence on start; destroy surplus instead of leaking)
- `apps/server/domain/sandbox/gvisor_provider.go` (create: ownership label; new label-scoped list/destroy helpers reusing the existing `Destroy` volume path)
- `apps/server/domain/sandbox/reconcile.go` (new: orphan reconciliation pass, grace period, in-use exclusions)
- `apps/server/domain/sandbox/cleanup.go` (run reconciliation in `runCycle`; startup pass)
- `apps/server/domain/sandbox/module.go` (wire reconciliation; startup reconcile after provider registration)
- `apps/server/internal/config/config.go` (grace-period override; reconciliation toggle)
- Tests: `warm_pool_test.go`, `gvisor_provider_test.go`, `reconcile_test.go` (new), `cleanup_test.go`

### API Changes
- None. Reconciliation is internal; no endpoint or response shape changes.

### Operational Impact
- One-off cleanup of the **existing** 184 orphan containers and 191 orphan volumes on prod is required — they predate the fix and carry no DB rows. Must be done by label, selectively, and verified against the running server's live pool.
- Steady-state prod container count becomes bounded by `WORKSPACE_WARM_POOL_SIZE` (currently 2) instead of growing per restart.

### Docs
- `docs/agent-sandbox/DEPLOYMENT.md` (warm pool lifecycle + reconciliation)
- `docs/agent-sandbox/OPERATIONS.md` (or equivalent: inspecting and cleaning labelled sandbox containers)

### Explicitly Out of Scope (repo rule: separate issue, no inline fix)
- Persistent MCP server workspaces (`lifecycle = persistent`, `expires_at IS NULL`) are intentionally long-lived and excluded from reconciliation; their reaping semantics are unchanged.
- Agent-session workspaces that miss their best-effort teardown closure (`executor.go:561`) are covered by the existing 30-day TTL reaper, not by this change.
- The stale env name `ENABLE_AGENT_WORKSPACES` in `emergent.memory.infra`'s `compose/.env.example:137` (code reads `ENABLE_AGENT_SANDBOXES`) belongs to the infra repo's own OpenSpec root.
