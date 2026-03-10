## Context

The current agent execution model is fully synchronous: `trigger_agent` blocks the calling goroutine until the sub-agent completes. This is simple and works well for short, tightly-coupled tasks where the caller needs the result inline. However, it provides no crash recovery, no resource ceiling, and no retry semantics. If the server restarts mid-run, all in-flight delegations are lost.

The existing `AgentDefinition` entity already has an `execution_mode` field, but it stores the agent's **interaction mode** (`suggest` / `execute` / `hybrid` — controls how the UI presents agent actions). The new field we are adding is a **dispatch mode** (`sync` | `queued`) — an orthogonal concept about _how the runtime schedules the run_, not how the agent interacts with users.

The codebase already has a battle-tested PostgreSQL job queue in `apps/server/internal/jobs/queue.go` (used by extraction, chunking, email domains). Agent run queuing reuses this infrastructure rather than building a new scheduling layer.

## Goals / Non-Goals

**Goals:**
- Add `dispatch_mode` field (`sync` | `queued`) to `AgentDefinition`, defaulting to `sync`.
- When `trigger_agent` targets a `queued` agent, enqueue a job immediately and return `{ run_id, status: "queued" }` to the caller rather than blocking.
- Run a configurable worker pool (default 5 workers) that polls `kb.agent_run_jobs` using `FOR UPDATE SKIP LOCKED` and executes queued agent runs.
- Support configurable retry with exponential backoff (`max_retries` per agent definition, default 0).
- On server startup, re-enqueue any jobs that were stuck `queued` or `running` (extend existing orphan recovery).
- Add `get_run_status(run_id)` MCP tool so orchestrators can poll run completion without blocking a goroutine.
- Zero impact on existing sync agents — omitting `dispatch_mode` defaults to `sync`.

**Non-Goals:**
- A general-purpose scheduler / cron replacement.
- `schedule_wakeup` (orchestrator self-reschedule) — this is out of scope for this change and will be a separate capability.
- Priority queues, FIFO-within-agent-type, or SLA tracking.
- Real-time push notification to callers when a queued run completes (polling via `get_run_status` is sufficient for now).
- Distributed worker coordination across multiple server instances (single-node; `FOR UPDATE SKIP LOCKED` handles concurrent workers within one process).

## Decisions

### Decision 1: New field is `dispatch_mode`, not repurposing `execution_mode`

The existing `execution_mode` column on `kb.agent_definitions` stores the interaction mode (`suggest` / `execute` / `hybrid`). Reusing it for dispatch semantics would be a category error and would require a multi-state enum migration.

The new field is named `dispatch_mode` (`sync` | `queued`, default `sync`). This is clear, orthogonal to the interaction mode, and backward-compatible — all existing definitions silently default to `sync`.

**Alternative considered**: Repurpose `execution_mode` with new enum values. Rejected because the two concepts are independent, and conflating them would confuse the UI (which already reads `execution_mode`).

### Decision 2: Worker pool built on `internal/jobs/queue.go`, not a new queue

The existing `jobs.Queue` already provides `FOR UPDATE SKIP LOCKED`, exponential backoff, stale job recovery, and stats. Agent run jobs need the same guarantees. The agent worker pool creates an `agent_run_jobs` table (same structure as `kb.email_jobs`, `kb.chunk_embedding_jobs`) and uses `jobs.Queue` for all dequeue/ack/fail logic.

**Alternative considered**: Embed queue logic directly in a new `worker_pool.go`. Rejected — duplicates proven infrastructure.

### Decision 3: `kb.agent_run_jobs` is a thin dispatcher table; `kb.agent_runs` remains the run record

The new `kb.agent_run_jobs` table holds only what the worker needs to pick up a job: `run_id` (FK to `kb.agent_runs`), `status`, `attempt_count`, `next_run_at`. The actual run configuration (agent name, message, project ID) lives in `kb.agent_runs` as today.

The worker dequeues a job, looks up the corresponding `agent_runs` row, builds an `ExecuteRequest`, and calls `executor.Execute()`. The existing `AgentRun` entity accrues steps and result as normal.

**Alternative considered**: Store the full execute payload in the job row. Rejected — `agent_runs` already carries all needed fields; duplicating them creates consistency risk.

### Decision 4: `kb.agent_runs` gains `queued` as a new status value

The run lifecycle becomes: `queued → running → success | error | cancelled`. A `queued` run is visible in the UI and API from the moment `trigger_agent` returns. The worker transitions it to `running` before calling `executor.Execute()`.

Orphan recovery at startup marks any run that is still `running` (crash mid-execution) as `error`. It must also re-enqueue runs still in `queued` state (server crashed before a worker picked them up) by inserting new rows into `agent_run_jobs` where none exist.

**Alternative considered**: Use only the `agent_run_jobs` table status and omit `queued` from `agent_runs`. Rejected — the UI and API already expose `agent_runs.status`; adding `queued` there gives visibility without a new query join.

### Decision 5: Worker pool size is a server config env var, not per-agent

A fixed global pool (default 5 workers, configurable via `AGENT_WORKER_POOL_SIZE`) is simple and predictable. Per-agent concurrency limits are a future enhancement if needed.

### Decision 6: `get_run_status` is a new MCP tool, not a polling loop inside `trigger_agent`

When a caller gets `{ run_id, status: "queued" }` back from `trigger_agent`, it can poll `get_run_status(run_id)` in a loop with a sleep. This is LLM-visible — the LLM decides whether to poll or exit and schedule a wakeup. Baking a polling loop into `trigger_agent` would hide latency from the LLM and burn context window.

**Alternative considered**: `trigger_agent` for queued agents blocks with a timeout parameter. Rejected — this ties up a goroutine for the full wait duration and defeats the purpose of queued dispatch.

### Decision 7: Queued → queued deadlock prevention is a documentation rule, not a runtime guard

A queued agent that calls `trigger_agent` on another queued agent and then polls `get_run_status` in a tight loop will drain context window. The rule is: queued agents that need to spawn queued children should fire-and-forget (enqueue child, do not poll inline). This is enforced by agent prompt authoring, not by runtime code (adding a runtime guard would require knowing the full call stack depth, which is complex and adds latency to every call).

## Risks / Trade-offs

**[Risk] Worker pool goroutines starved by long-running agents**
→ Mitigation: Worker pool size is configurable. LLM calls are already bounded by the doom-loop detector (5 consecutive identical tool calls triggers termination). Document recommended pool size for workloads with many concurrent agents.

**[Risk] `agent_run_jobs` table grows unbounded**
→ Mitigation: Completed/failed job rows can be pruned by the same vacuum pattern used for other job tables. Add a `completed_at` timestamp column and prune rows older than N days in a background cleanup (consistent with email/chunk job cleanup).

**[Risk] Orphan recovery re-enqueues a run that another worker is already processing (split-brain after fast restart)**
→ Mitigation: Re-enqueue only inserts a new `agent_run_jobs` row if no `pending`/`processing` row already exists for the run. The `FOR UPDATE SKIP LOCKED` claim on `agent_run_jobs` prevents two workers from claiming the same job simultaneously. The window is small (server must restart and recover before the original worker completes), and the existing `agent_runs.status` uniqueness check catches the duplicate before `executor.Execute()` is called.

**[Risk] Callers get `run_id` but lose the reference (LLM context window rolls over)**
→ Mitigation: Run IDs are stored in the graph via `WorkPackage`/`Task` objects. The orchestrator pattern writes the run ID to graph state before exiting; on wakeup it re-reads the graph to continue. This is a prompt-authoring concern, not a runtime concern.

**[Risk] `dispatch_mode` field name conflicts with existing UI or API serialisation of `execution_mode`**
→ None. The two fields are independent; `execution_mode` is unchanged, `dispatch_mode` is new. JSON serialisation is `dispatchMode`.

## Migration Plan

1. Add Goose migration: `ALTER TABLE kb.agent_definitions ADD COLUMN dispatch_mode text NOT NULL DEFAULT 'sync'`.
2. Add Goose migration: Create `kb.agent_run_jobs` table (run_id FK, status, attempt_count, max_attempts, next_run_at, created_at, completed_at).
3. Add Goose migration: `ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS status value `queued` to the check constraint (or convert to unconstrained text if the current column is already text).
4. Deploy new binary — hot reload handles handler/service changes; new fx lifecycle component (worker pool) starts automatically.
5. Rollback: Stop worker pool (env var `AGENT_WORKER_POOL_SIZE=0` disables workers without reverting DB). Existing queued runs will remain `queued` until a rollback migration moves them to `error`.

## Open Questions

- **Should `max_retries` default to 0 (no retry) or 3?** Retries consume LLM credits on transient errors. Default 0 is safe and explicit; operators opt in per agent definition.
- **Job row retention policy**: How long should completed/failed `agent_run_jobs` rows be kept? 7 days aligns with existing email job retention, but there is no current audit requirement for agent run job history beyond what `kb.agent_runs` already records.
- **Worker pool across multiple server instances**: `FOR UPDATE SKIP LOCKED` is safe for concurrent workers on a single DB, but if we ever run N server replicas, the pool size effectively multiplies. Is that acceptable, or should we add a max-in-flight advisory lock?
