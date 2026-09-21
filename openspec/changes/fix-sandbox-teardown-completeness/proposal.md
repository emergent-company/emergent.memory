## Why

Three sandbox reclamation gaps were found while fixing warm-pool orphan accumulation (`fix-warm-pool-orphan-reaping`). Each is a distinct, still-open leak that the label-based orphan reconciler does **not** cover, because all three leave sandbox rows in a non-stopped state — and reconciliation deliberately spares any row that is not stopped or errored.

**Gap 1 — Agent-session teardown is not guaranteed.**
`ExecuteResult.Cleanup` is idempotent, but nothing guarantees it is invoked. The contract comment at `executor.go:557-560` says *"Callers are responsible for invoking Cleanup on the returned ExecuteResult"*, while the struct doc at `executor.go:312-315` claims *"The executor always defers Cleanup as a safety net"* — **no such defer exists anywhere in `executor.go`**. The stale comment hides the real requirement. Known consequences:

- **Share links leak unconditionally.** `share_service.go:851` calls `s.runner.Execute(ctx, req)` inside `StreamMessage` (`:814`) and never calls `Cleanup`; its caller `share_handler.go:287` does not either. Whenever share-link sandboxing is enabled, every served share link leaves a running container and volume behind.
- **Panics skip teardown.** Callers register `defer result.Cleanup()` only *after* the call returns (`handler.go:1276-1277`, `worker_pool.go:160-161`, `agent_run_once.go:144-145`, `a2a_stream.go:546-547`, `triggers.go:426-427`, `coordination_tools.go:435-436`/`471-472`, `a2a_message.go:622-623`/`720-721`, `mcp_tools.go:612-613`), and direct-call sites (`handler.go:762-763`, `:2960-2961`, `a2a_message.go:610-611`/`704-705`, `mcp_tools.go:1434-1435`, `executor.go:1352-1353`) are simply jumped over. The recover wrappers (`handler.go:739-746`, `~2919-2944`) only log and mark the run errored.
- **Crash / restart / OOM mid-run leaves rows un-reclaimed.** `registerOrphanRecovery` (`agents/module.go:178-210`) marks orphaned **runs** as error and requeues queued runs, but never touches the sandbox row; `StaleRunReaper` (`stale_run_reaper.go:84-97`) only touches run rows. The sandbox stays non-stopped with `expires_at = now + 30 days`, so the only reaper that can ever match it is the 30-day TTL job.

**Gap 2 — Persistent MCP server workspaces are unreclaimable.**
`service.go:73-76` / `:104-108` give `container_type = mcp_server` the `persistent` lifecycle and leave `expires_at` NULL; `Store.ListExpired` (`store.go:138-151`) requires `expires_at IS NOT NULL`, so they are structurally excluded from every reaper. The only removal path is an explicit `DELETE /api/v1/mcp/hosted/:id` (`mcp_handler.go:233` → `mcp_hosting.go:247-278`). Abandoned hosted MCP servers therefore run forever, holding a container, a volume, and memory. `last_used_at` already exists and is maintained (`entity.go:85`, `TouchLastUsed` at `store.go:177-190`, called on every JSON-RPC call at `mcp_hosting.go:222`) and is already surfaced to operators via `GET /api/v1/mcp/hosted` (`dto.go:255-256`) — but nothing acts on it.

**Gap 3 — Two stale comments misdescribe teardown semantics.**
`executor.go:312-315` (claims a non-existent defer) and `auto_provisioner.go:231` (claims `TeardownWorkspace` spares persistent workspaces; `:233-267` has no lifecycle check and always destroys) both mislead future readers working in exactly this failure area.

## What Changes

- **Teardown becomes guaranteed, not caller-dependent.** A single registered cleanup is bound to the run's lifetime so that every exit path — normal return, error return, context cancellation, panic, streaming abort — results in exactly one teardown. The documented contract is corrected to match the code.
- **Share-link runs tear down.** The share-link execution path stops leaking a container and volume per served link.
- **Startup recovery for sandbox rows with no live owner.** Sandbox rows whose owning run is no longer active after a restart SHALL be transitioned to a terminal state so the orphan reconciler can reclaim their containers. Run-level orphan recovery (`registerOrphanRecovery`) is extended to the sandbox rows it currently orphans.
- **Idle reclamation for persistent MCP servers.** A configurable idle-based reclamation policy SHALL destroy persistent MCP server workspaces that have not been used for longer than the configured window, expressed in terms of the existing `last_used_at` column. The policy SHALL default to disabled so existing persistent semantics are preserved unless an operator opts in.
- **Operator visibility for reclamation decisions.** Reclamation SHALL log which resources were destroyed, skipped, and why, so an abandoned MCP server or an un-torn-down sandbox is detectable rather than silent.
- **Stale comments corrected** so the teardown and persistent-teardown contracts read as the code behaves.

## Capabilities

### New Capabilities

- `agent-sandbox-teardown`: every provisioned agent sandbox is torn down exactly once per run across all exit paths, and sandbox rows with no live owner are reclaimed after restart.
- `mcp-hosted-lifecycle`: hosted MCP server workspaces are reclaimable by a configurable idle policy over `last_used_at`, in addition to explicit deletion.

### Modified Capabilities

- `agent-sandbox-providers`: unchanged in behaviour; the provider `Destroy` path is reused by both new capabilities.

## Impact

### Code Changes
- `apps/server/domain/agents/executor.go` (guaranteed teardown binding; corrected `ExecuteResult` contract comment)
- `apps/server/domain/agents/share_service.go` (tear down share-link runs; correct the `StreamMessage` path)
- `apps/server/domain/sandbox/auto_provisioner.go` (correct the inaccurate `TeardownWorkspace` comment)
- `apps/server/domain/sandbox/cleanup.go` (run the idle reclamation pass alongside expiry and reconciliation)
- `apps/server/domain/sandbox/cleanup.go` (idle reclamation pass: candidate listing, `last_used_at`/lifecycle policy evaluation, fresh pre-destroy re-read, bounded per-server destroy)
- `apps/server/domain/sandbox/store.go` (persistent-MCP candidate query and conditional fresh re-read over `last_used_at`, reusing the existing `Remove`/`Destroy` path)
- `apps/server/domain/sandbox/module.go` (wire the idle policy into the cleanup cycle)
- `apps/server/domain/agents/module.go` (`registerOrphanRecovery` also transitions orphaned sandbox rows)
- `apps/server/internal/config/config.go` (idle reclamation window, default disabled)
- Tests: `agents/executor_cleanup_test.go`, `agents/share_test.go`, `agents/module_test.go`, `sandbox/cleanup_test.go`, `sandbox/store_query_test.go`, `sandbox/e2e_scenario_test.go`

### API Changes
- None required. `GET /api/v1/mcp/hosted` already exposes `LastUsedAt`; no response shape changes.

### Operational Impact
- Enabling the idle policy is the only behavioural change for persistent MCP servers, and it is opt-in via configuration (default disabled), so no existing deployment changes behaviour on upgrade.
- Startup sandbox-row recovery may stop rows that a previous process left `ready`; those rows' containers are then reclaimed. This is intended and must be logged per row.

### Docs
- `docs/agent-sandbox/DEPLOYMENT.md` (teardown guarantee + idle reclamation configuration)
- `docs/agent-sandbox/OPERATIONS.md` (identifying abandoned MCP servers and un-torn-down sandboxes)

### Explicitly Out of Scope
- Warm-pool orphan accumulation and label-based orphan reconciliation (owned by `fix-warm-pool-orphan-reaping`).
- Multi-replica coordination of reclamation beyond sparing another live process's resources.
- Any change to the default 30-day ephemeral TTL.
