## 1. Database Migrations

- [x] 1.1 Add Goose migration: `ALTER TABLE kb.agent_definitions ADD COLUMN dispatch_mode text NOT NULL DEFAULT 'sync'`
- [x] 1.2 Add Goose migration: create `kb.agent_run_jobs` table (`id`, `run_id` FK → `kb.agent_runs`, `status`, `attempt_count`, `max_attempts`, `next_run_at`, `created_at`, `completed_at`)
- [x] 1.3 Add Goose migration: add `queued` as a valid value for `kb.agent_runs.status` (update check constraint or add to enum if applicable)

## 2. Entity & Model Changes

- [x] 2.1 Add `DispatchMode` field (`sync` | `queued`, default `sync`) to `AgentDefinition` struct in `entity.go` — distinct from the existing `ExecutionMode` (interaction mode) field
- [x] 2.2 Add `AgentRunJob` Bun model struct in `entity.go` mirroring `kb.agent_run_jobs` schema
- [x] 2.3 Add `RunStatusQueued AgentRunStatus = "queued"` constant to `entity.go`

## 3. YAML & Auto-Provisioner

- [x] 3.1 Add `dispatch_mode` field to the agent definition YAML schema parser (wherever YAML → `AgentDefinitionInput` is parsed)
- [x] 3.2 Propagate `dispatch_mode` through `CreateDefinition` / `UpdateDefinition` store methods so it is persisted correctly
- [x] 3.3 Add `dispatch_mode: queued` to all leaf worker and manager agent YAMLs in the workspace blueprint: `web-researcher`, `code-researcher`, `coder`, `designer`, `enricher`, `reviewer`, `research-manager`, `coding-manager`, `review-manager`. Leave `orchestrator` and `janitor` as `sync` (they are human-triggered entry points).

## 4. trigger_agent: Structured Input

- [x] 4.1 Change `trigger_agent` MCP tool input schema in `mcp_tools.go`: replace the `message` string property with a `message` object property containing `instructions` (string, optional) and `task_id` (string, optional)
- [x] 4.2 Update `ExecuteTriggerAgent` to extract `instructions` and `task_id` from the new structured input and compose the `userMessage` string (e.g. prepend task_id hint when present)
- [x] 4.3 Update the `trigger_agent` tool description to document the new JSON input format

## 5. trigger_agent: Dispatch Routing

- [x] 5.1 In `ExecuteTriggerAgent`, after resolving the agent and looking up `agentDef`, check `agentDef.DispatchMode`
- [x] 5.2 Implement the `queued` branch: create `kb.agent_runs` row with `status: queued` and insert a `kb.agent_run_jobs` row atomically (same DB transaction), then return `{ run_id, status: "queued" }` immediately
- [x] 5.3 Keep the existing `sync` branch unchanged (call `executor.Execute()` and return result inline)

## 6. Worker Pool

- [x] 6.1 Create `apps/server/domain/agents/worker_pool.go` with a `WorkerPool` struct and `Start(ctx)` / `Stop()` lifecycle methods
- [x] 6.2 Implement the poll loop: each worker calls a `ClaimNextJob()` helper that does `SELECT … FOR UPDATE SKIP LOCKED LIMIT 1` on `kb.agent_run_jobs`, transitions the job to `processing` and the run to `running` in one transaction
- [x] 6.3 Implement job execution: after claiming, look up the `agent_runs` row, build an `ExecuteRequest`, call `executor.Execute()`, then mark the job `completed` / `failed` and the run `success` / `error`
- [x] 6.4 Implement retry logic: on failure, if `attempt_count < max_attempts` set job back to `pending` with `next_run_at = now + backoff(attempt_count)`, else mark `failed`
- [x] 6.5 Implement idle sleep: when no job is found, sleep `AGENT_WORKER_POLL_INTERVAL` (default 5s) before next poll
- [x] 6.6 Wire `WorkerPool` into `module.go` as an fx lifecycle component (`OnStart` / `OnStop`); read pool size from config (`AGENT_WORKER_POOL_SIZE`, default 5; 0 = disabled)

## 7. Orphan Recovery

- [x] 7.1 Extend `MarkOrphanedRunsAsError` in `repository.go` to also re-enqueue `kb.agent_runs` rows with `status: queued` that have no active `kb.agent_run_jobs` row (insert a new job row for each)
- [x] 7.2 Ensure the existing logic marking `status: running` runs as `error` on startup is unchanged and still runs before the worker pool starts

## 8. get_run_status Tool

- [x] 8.1 Add `ExecuteGetRunStatus` handler in `mcp_tools.go`: accept `run_id`, query `kb.agent_runs`, enforce project isolation, return structured status/result/error fields
- [x] 8.2 Register `get_run_status` tool in the MCP tool definitions list with input schema `{ run_id: string (required) }`
- [x] 8.3 Add `queued` to the `status` enum in `list_agent_runs` tool definition (consistency)

## 9. Repository Methods

- [x] 9.1 Add `CreateRunJob(ctx, runID, maxAttempts) error` to `repository.go`
- [x] 9.2 Add `ClaimNextJob(ctx) (*AgentRunJob, error)` to `repository.go` — uses `FOR UPDATE SKIP LOCKED`
- [x] 9.3 Add `CompleteJob(ctx, jobID, runID) error` and `FailJob(ctx, jobID, runID, errMsg string, requeue bool, nextRunAt time.Time) error` to `repository.go`
- [x] 9.4 Add `FindRunByID(ctx, runID, projectID string) (*AgentRun, error)` (project-scoped, used by `get_run_status`)
- [x] 9.5 Add `RequeueOrphanedQueuedRuns(ctx) error` to `repository.go` — inserts missing job rows for orphaned queued runs

## 10. Tests & Verification

- [x] 10.1 Write unit test for the `queued` branch of `ExecuteTriggerAgent`: verify it returns `run_id` and `status: queued` without calling `executor.Execute()`
- [x] 10.2 Write unit test for `WorkerPool.ClaimNextJob`: verify `FOR UPDATE SKIP LOCKED` semantics (mock DB or integration test against test Postgres)
- [x] 10.3 Write unit test for orphan re-enqueue: seed a `queued` run with no job row, call `RequeueOrphanedQueuedRuns`, verify job row is created
- [x] 10.4 Write unit test for `get_run_status`: verify cross-project isolation (run from different project returns not-found)
- [x] 10.5 Verify `TestOrchestratorDelegatesResearch` still passes end-to-end on mcj-emergent after all changes
