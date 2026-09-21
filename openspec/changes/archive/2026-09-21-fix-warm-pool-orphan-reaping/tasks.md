## 1. Ownership labels and label-scoped enumeration (R1, R2)

- [x] 1.1 Add an ownership label constant (e.g. `memory.owner`) applied by `GVisorProvider.Create` (`gvisor_provider.go:122`) and `CreateFromSnapshot` (`:441`), value = stable per-process instance identity (host + PID + process start token)
- [x] 1.2 Apply `memory-workspace-*` volumes the matching ownership label in addition to the existing `memory.workspace` / `workspace.type` / `workspace.volume` labels (`gvisor_provider.go:152`)
- [x] 1.3 Add label-scoped enumeration helpers on the provider: list sandbox containers by `memory.workspace=true` and list sandbox volumes by `memory.workspace=true`, returning name, id, labels, created-at, and state
- [x] 1.4 Add a label-scoped destroy helper that removes a container and the volume named by its `workspace.volume` label, reusing the existing `Destroy` volume path (`gvisor_provider.go:310-333`) and tolerating already-absent resources
- [x] 1.5 Unit tests: ownership label present on every created container and volume; enumeration returns only labelled resources; destroy removes container and its labelled volume; destroy is idempotent for missing resources

## 2. Orphan reconciliation pass (R2, R3)

- [x] 2.1 Add `reconcile.go` with a `Reconciler` that takes the orchestrator/provider enumeration, the workspace store, a grace period, and a logger
- [x] 2.2 Implement ownership decision: skip containers owned by the current process instance; skip containers referenced by a DB row that is not `stopped`/`error`; skip persistent MCP containers (`lifecycle = persistent` / `container_type = mcp_server`) — D6
- [x] 2.3 Implement the grace-period guard: never destroy a container created more recently than the configured grace window — D3
- [x] 2.4 Destroy ownerless containers past the grace period together with their labelled volume; continue past individual failures and aggregate the outcome
- [x] 2.5 Reconcile orphan volumes that have no corresponding running container (including volumes left by previously removed containers)
- [x] 2.6 Bound the pass with a context timeout and make a second concurrent invocation a no-op
- [x] 2.7 Unit tests: ownerless container past grace is destroyed with its volume; current-process container is kept; DB-active container is kept; persistent MCP container is kept; container inside grace window is kept; one failing destroy does not abort the pass; orphan volume with no container is removed; second concurrent pass is a no-op

## 3. Scheduling and configuration (R4)

- [x] 3.1 Run reconciliation once at startup after provider registration, gated on `Sandbox.IsEnabled()` (`module.go`)
- [x] 3.2 Invoke reconciliation from `CleanupJob.runCycle` (`cleanup.go:107`) alongside `cleanupExpired`, on the existing `WORKSPACE_CLEANUP_INTERVAL_MIN` ticker
- [x] 3.3 Add `WORKSPACE_RECONCILE_GRACE_MIN` (default 15) and a reconciliation enable/disable knob to `SandboxConfig` (`internal/config/config.go:390-429`)
- [x] 3.4 Unit tests: startup pass runs when sandboxes are enabled and is skipped when disabled; cycle runs reconcile plus expiry in one tick; grace period and toggle are honoured from config

## 4. Warm-pool convergence (R5)

- [x] 4.1 On `WarmPool.Start` (`warm_pool.go:128`), destroy surplus containers for managed images beyond the configured target instead of leaving them
- [x] 4.2 On the `Acquire` staleness path (`warm_pool.go:275`), destroy the discarded container before/as part of creating its replacement so no transient orphan remains
- [x] 4.3 Confirm `WarmPool.Stop` (`warm_pool.go:187`) + fx shutdown hook remain the graceful path, and that a missed hook is now covered by §2 reconciliation
- [x] 4.4 Unit tests: start with surplus containers converges to target and destroys the surplus; staleness discard leaves no extra container; steady-state pool size equals target after repeated acquire/replenish cycles

## 5. One-off cleanup of existing prod orphans (operational)

- [ ] 5.1 Document the exact label-filtered inspection commands (`docker ps --filter label=memory.warm-pool=true`, labelled volume listing) and the expected counts
- [ ] 5.2 Document and run the selective cleanup of the 184 pre-fix orphan containers and their 191 zero-byte volumes on prod, excluding the live pool of the running server
- [ ] 5.3 Verify post-cleanup: labelled container count equals the running pool target, orphan volumes = 0, `emergent-server-1` and `web-ui` remain healthy, `/health` returns OK
- [ ] 5.4 Verify the fix end-to-end in dev: restart the server ungracefully (SIGKILL), confirm the next startup reclaims the predecessor's warm containers automatically

## 6. Spec and docs

- [ ] 6.1 Delta spec `specs/agent-sandbox-lifecycle/spec.md` (this change)
- [x] 6.2 Update `docs/agent-sandbox/DEPLOYMENT.md` with warm-pool lifecycle and reconciliation behaviour
- [x] 6.3 Add/extend operator docs for inspecting and cleaning labelled sandbox containers and volumes
- [x] 6.4 Run `openspec validate fix-warm-pool-orphan-reaping` and `go build ./...` + `go test ./domain/sandbox/...` from `apps/server`

## 7. Review follow-ups (peer safety and fail-safe)

- [x] 7.1 Abort reconciliation when container enumeration fails — never sweep volumes from an empty container list
- [x] 7.2 Persist `provider_workspace_id` atomically with the workspace INSERT (close the acquire → DB-write window)
- [x] 7.3 Per-container warm-pool liveness heartbeat lease; reconciler spares fresh leases, reaps stale/missing
- [x] 7.4 Add `WORKSPACE_OWNER_HEARTBEAT_MIN` (default 2) and wire refresh interval + `3 ×` staleness threshold
- [x] 7.5 Remove dead `workspace.lifecycle` label check (label never written at create time)
- [x] 7.6 Fail-safe timestamps: unknown/zero creation time spares the resource
- [x] 7.7 Startup reconciliation runs once (cleanup initial pass no longer reconciles)
- [x] 7.8 Update `design.md`, `spec.md`, `OPERATIONS.md`/`DEPLOYMENT.md` to match implementation
- [x] 7.9 Unit tests for all of the above

## 8. Second review follow-ups (lease lifecycle and claim accuracy)

- [x] 8.1 Reconciler removes heartbeat leases whose container is gone (logged, label-scoped, idempotent)
- [x] 8.2 `DestroySandboxContainer` removes the container's leases on normal teardown
- [x] 8.3 Zero/unparseable lease timestamp fails safe (spare), matching the grace-period rule
- [x] 8.4 Exclude lease volumes from the workspace volume sweep (provider filter + defensive reconciler guard)
- [x] 8.5 Document the handler acquire → DB-write window as lease-bridged (or make it atomic)
- [x] 8.6 Correct D4/spec/docs: startup spares a just-crashed peer until its lease expires
- [x] 8.7 Unit tests for all of the above

## 9. Third review follow-ups (Copilot review, concurrency and fail-safe)

- [x] 9.1 Serialize heartbeat beat+prune per provider so concurrent refresh cannot remove the only lease (with concurrency regression test)
- [x] 9.2 Normalize non-positive Docker container creation timestamps to the zero time so unknown ages fail safe (with unit test)
- [x] 9.3 Re-validate a warm-pool container's lease immediately before destruction (stale-snapshot TOCTOU; with fake-driven test)
- [x] 9.4 Abort reconciliation when the workspace reference store is unavailable instead of proceeding with an empty reference set
- [x] 9.5 Make `provider_workspace_id` internal-only (`json:"-"`) so a client cannot shield an unrelated container (with decode test)
- [x] 9.6 Restrict warm-pool pre-booting to the reconciled, lease-capable provider (with test)
- [x] 9.7 Remove liveness leases on the normal `Destroy` path, not only `DestroySandboxContainer` (with Docker-backed test)
- [x] 9.8 Fix `OPERATIONS.md` docker `ps --format` field (`.CreatedAt`, not `.Generated`) and the stale "reclaimed by the next start" wording in `proposal.md`
- [x] 9.9 Update `design.md` (D2a, D7, D8, D9) and the delta spec to match, and re-run `openspec validate --strict`
