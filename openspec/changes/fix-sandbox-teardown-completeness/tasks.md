## 1. Guaranteed teardown on every exit path (Gap 1)

- [x] 1.1 Bind the run's cleanup to the run lifetime in `executor.go` so exactly one teardown occurs for every path (normal return, error return, cancellation, panic, streaming abort), keeping the existing idempotent `sync.Once` semantics
- [x] 1.2 Ensure the binding survives a panic in `Execute` / `ExecuteWithRun` / `Resume` (teardown runs even when the call does not return normally)
- [x] 1.3 Correct the `ExecuteResult` doc comment (`executor.go:312-315`) to describe the actual guarantee, and reconcile it with the caller contract comment (`executor.go:557-560`)
- [x] 1.4 Confirm by test that a caller which never invokes `Cleanup` still gets teardown exactly once, and that a caller which does invoke it does not double-destroy
- [x] 1.5 Unit tests: teardown on normal return; on error return; on context cancellation; on panic mid-run; exactly-once under repeated `Cleanup()` calls; teardown failure is logged and does not mask the run result

## 2. Share-link runs tear down (Gap 1)

- [x] 2.1 Fix `share_service.go:851` (`StreamMessage`, `:814`) so the execute result's cleanup is always invoked on every exit path of the handler
- [x] 2.2 Verify `share_handler.go:287` needs no separate change once the service guarantees teardown; if it does, fix it there
- [x] 2.3 Unit test: a share-link run leaves no workspace row in a non-stopped state and invokes provider destroy exactly once (including when the stream aborts early and when the run errors)

## 3. Startup recovery for sandbox rows with no live owner (Gap 1)

- [x] 3.1 Extend `registerOrphanRecovery` (`agents/module.go:178-210`) so that when a run is marked orphaned, the sandbox rows linked to it are transitioned out of a non-stopped state
- [x] 3.2 Ensure the transitioned rows become eligible for label-based orphan reconciliation (reuse the existing reconciliation path rather than destroying containers directly in the recovery hook)
- [x] 3.3 Log each recovered row with id, provider workspace id, previous status, and reason
- [x] 3.4 Unit tests: a run left `running` with a `ready` sandbox row is recovered on startup; a `stopped`/`errored` row is left alone; a row belonging to a live run is never touched; recovery is idempotent across repeated startups

## 4. Idle reclamation for persistent MCP servers (Gap 2)

- [x] 4.1 Add a store query returning persistent MCP server rows (`container_type = mcp_server`, `lifecycle = persistent`) with `last_used_at` older than the configured window
- [x] 4.2 Add an idle reclamation pass that destroys those workspaces and deletes their rows by reusing the existing `mcp_hosting.Remove`/provider `Destroy` path (no new removal mechanism)
- [x] 4.3 Add `WORKSPACE_PERSISTENT_IDLE_TTL_DAYS` (default `0` = disabled) to `SandboxConfig` and document that `0` preserves current persistent semantics
- [x] 4.4 Schedule the pass from `CleanupJob.runCycle` alongside expiry and orphan reconciliation, gated on sandboxes being enabled and the policy being enabled
- [x] 4.5 Never reclaim a persistent MCP server that is `creating`/`stopping`, or whose row is newer than the window, or whose `last_used_at` was updated within the window
- [x] 4.6 Unit tests: disabled policy reclaims nothing; enabled policy reclaims an idle server; a recently used server is kept; a server touched inside the window is kept; an explicit-DELETE server is unaffected; a destroy failure is logged and does not abort the pass; row deletion happens only after successful container destroy
- [x] 4.7 Verify `GET /api/v1/mcp/hosted` exposes enough information (existing `LastUsedAt`) for an operator to see why a server was reclaimed; extend only if a field is genuinely missing

## 5. Stale comments and contracts (Gap 3)

- [x] 5.1 Correct `auto_provisioner.go:231` (`TeardownWorkspace` comment) to match the actual lifecycle behaviour at `:233-267`
- [x] 5.2 Grep the sandbox and agents domains for other comments that assert teardown/reaping behaviour the code does not implement; correct or delete them
- [x] 5.3 No test required; confirm no behaviour changed by this section

## 6. Spec, docs, verification

- [x] 6.1 Delta specs: `specs/agent-sandbox-teardown/spec.md` and `specs/mcp-hosted-lifecycle/spec.md` (this change)
- [x] 6.2 Update `docs/agent-sandbox/DEPLOYMENT.md` with the teardown guarantee and the idle reclamation knob
- [x] 6.3 Update `docs/agent-sandbox/OPERATIONS.md` with how to spot an abandoned MCP server or an un-torn-down sandbox
- [x] 6.4 Run `openspec validate fix-sandbox-teardown-completeness`, `go build ./...`, `go test ./domain/sandbox/... ./domain/agents/...`, `go vet ./domain/sandbox/... ./domain/agents/...` from `apps/server`
- [ ] 6.5 Note in the PR/commit body that orphan-container reconciliation itself is owned by `fix-warm-pool-orphan-reaping` and that this change depends on it only for row-to-container reclamation
