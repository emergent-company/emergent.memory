## Why

The current `trigger_agent` tool executes sub-agents synchronously inside the calling agent's goroutine — if the server crashes mid-run, the entire delegation chain is lost with no retry. There is also no resource control: any number of sub-agents can be spawned concurrently, competing for DB connections and memory without any backpressure. We want to keep the simplicity of synchronous calling (which is useful for short, tightly-coupled tasks where the parent needs the result inline) while adding a worker queue as an alternative execution mode that provides crash recovery, resource ceilings, and retry semantics.

## What Changes

- Each `AgentDefinition` gains a `dispatch_mode` field: `sync` (default, current behaviour) or `queued`.
- `trigger_agent` input changes from a plain string to a JSON object `{ "instructions": "...", "task_id": "..." }`. Both fields are optional. `task_id` is a purely advisory hint passed through to the agent's initial message — the system does not validate or look it up; the agent's prompt decides what to do with it. Plain-string usage is replaced by always-JSON going forward.
- `trigger_agent` inspects the target agent's `dispatch_mode` at call time:
  - `sync` — behaves exactly as today (blocking inline execution, result returned directly to caller).
  - `queued` — enqueues an `agent_run` row with `status=queued`, returns a run ID to the caller immediately; the run is picked up by a worker pool.
- A worker pool (configurable size, default 5) polls for `queued` agent runs using `FOR UPDATE SKIP LOCKED` and executes them.
- Queued runs that are `in_progress` at server startup are re-enqueued (extending the existing orphan recovery).
- Queued runs support a configurable retry count (`max_retries`, default 0) with exponential backoff.
- Parent agents calling `trigger_agent` on a `queued` agent receive the run ID and can either: poll `get_run_status(run_id)` inline, or exit and let the graph state reflect completion.
- `dispatch_mode` is declared in agent YAML definition files and stored in `kb.agent_definitions`.

## Capabilities

### New Capabilities

- `agent-run-queue`: Worker pool that executes queued agent runs — polling, FOR UPDATE SKIP LOCKED, configurable pool size, retry with backoff, startup re-enqueue of orphaned queued runs.
- `agent-execution-mode`: `dispatch_mode` field on AgentDefinition (`sync` | `queued`), YAML parsing, DB storage, enforcement in `trigger_agent` routing.

### Modified Capabilities

- `agent-coordination-tools`: `trigger_agent` input changes to structured JSON `{ instructions, task_id }`. When target agent has `dispatch_mode: queued`, it returns a run ID immediately instead of blocking. Adds `get_run_status(run_id)` tool for polling.
- `agent-execution`: Agent run lifecycle gains `queued` status before `running`. Orphan recovery extended to re-enqueue runs stuck in `queued` or `running` at startup.

## Impact

- **DB**: New `status` value `queued` on `kb.agent_runs`. New column `dispatch_mode` on `kb.agent_definitions`. New table `kb.agent_run_jobs`. Migration required.
- **agent-definitions YAML**: New optional field `dispatch_mode` (default `sync`). Backward compatible — existing definitions behave as before.
- **`mcp_tools.go`**: `ExecuteTriggerAgent` input schema changes to `{ instructions, task_id }`. Branches on target agent's `dispatch_mode`.
- **New file**: `apps/server/domain/agents/worker_pool.go` — worker pool implementation.
- **`module.go`**: Worker pool registered as fx lifecycle component, started/stopped with server.
- **`executor.go`**: `Execute()` can be called directly (sync path) or via worker pool (queued path). No change to sync path.
- **Blueprint YAML files**: `dispatch_mode: queued` added to all leaf workers (`web-researcher`, `code-researcher`, `coder`, `designer`, `enricher`, `reviewer`) and all manager agents (`research-manager`, `coding-manager`, `review-manager`). Orchestrators (`orchestrator`, `janitor`) remain `sync` as human-triggered entry points.
- **No breaking changes** to existing sync agents — omitting `dispatch_mode` defaults to `sync`.
