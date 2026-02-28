## 1. Database Migrations — Phase 1

- [ ] 1.1 Add columns to `kb.agent_runs`: `parent_run_id` (UUID nullable FK), `step_count` (INT DEFAULT 0), `max_steps` (INT nullable), `resumed_from` (UUID nullable FK), `error_message` (TEXT nullable), `completed_at` (TIMESTAMPTZ nullable)
- [ ] 1.2 Add index on `kb.agent_runs(parent_run_id)`

## 2. Agent Execution Engine

- [ ] 2.1 Create `AgentExecutor` in `domain/agents/` that builds an ADK-Go pipeline from an AgentDefinition (single, sequential, loop flow types), creates Gemini model via Vertex AI, and wires resolved tools
- [ ] 2.2 Implement `AgentRun` lifecycle tracking: create run with `status: running` before execution, update to `completed`/`failed`/`cancelled` with `summary`, `duration`, `completed_at`, `error_message`
- [ ] 2.3 Implement step limit enforcement: soft stop (inject "summarize and stop" message, disable tools for final iteration), hard stop (refuse tool calls, set `status: paused`)
- [ ] 2.4 Implement timeout enforcement using `context.WithDeadline`: inject "time's up" message, 30-second grace period, hard cancel after grace period, set `status: paused`
- [ ] 2.5 Implement `DoomLoopDetector`: track consecutive identical tool calls by name + argument hash, inject error at threshold=3, hard stop at threshold=5 with `status: failed`
- [ ] 2.6 Replace stubbed execution in `handler.go:421-422` with AgentExecutor dispatch
- [ ] 2.7 Update trigger endpoint (`POST /api/projects/:id/agents/:agentId/trigger`) to execute via AgentExecutor and return real results

## 3. Database Migrations — Phase 2

- [ ] 3.1 Create `kb.agent_definitions` table: id, product_id, project_id, name, description, system_prompt, model (JSONB), tools (TEXT[]), trigger, flow_type, is_default, max_steps, default_timeout, visibility (VARCHAR DEFAULT 'project'), acp_config (JSONB), config (JSONB), created_at, updated_at
- [ ] 3.2 Add unique index on `kb.agent_definitions(project_id, name)`

## 4. Agent Definitions

- [ ] 4.1 Create AgentDefinition entity and Bun model in `domain/agents/`
- [ ] 4.2 Build agent definition store with CRUD operations and project-scoped queries
- [ ] 4.3 Implement product manifest import: store agent definitions from product `agents` array on product install, re-sync on product update
- [ ] 4.4 Implement visibility filtering on admin-facing list endpoint: include external+project by default, support `include_internal=true` query parameter
- [ ] 4.5 Store ACP config for external-visibility agents, ignore for project/internal
- [ ] 4.6 Implement default agent selection for chat sessions (`is_default: true` lookup, fallback to direct LLM)
- [ ] 4.7 Implement trigger registration: event triggers (on_document_ingested listener) and schedule triggers (cron job registration in existing scheduler)

## 5. Tool Pool and Filtering

- [ ] 5.1 Build `ToolPool` struct per project: combine built-in graph tools from `domain/mcp/service.go` with external MCP server tools
- [ ] 5.2 Implement ToolPool caching per project with invalidation on MCP server config change
- [ ] 5.3 Build `ResolveTools(agentDef)` function: filter ToolPool by agent's `tools` whitelist with exact name matching and glob pattern support (`"entities_*"`)
- [ ] 5.4 Handle wildcard access (`tools: ["*"]`) returning entire ToolPool
- [ ] 5.5 Handle tool-not-found: log warning, skip unresolved tool, do not fail resolution
- [ ] 5.6 Implement `SubAgentDeniedTools`: remove `spawn_agents` and `list_available_agents` from sub-agents at depth > 0 by default
- [ ] 5.7 Implement opt-in delegation: allow sub-agents with explicit `spawn_agents` in tools list when depth < `max_depth` (default 2), hard block at depth >= `max_depth`
- [ ] 5.8 Ensure tool filtering is enforced at Go/ADK level — pipeline only receives resolved tools, LLM tool calls to unresolved tools return "tool not found"

## 6. Coordination Tools

- [ ] 6.1 Build `list_available_agents` tool: query `kb.agent_definitions` for project, return name, description, tools list, flow_type, visibility (exclude full system prompts)
- [ ] 6.2 Build `spawn_agents` tool: accept array of spawn requests (agent_name, task, optional timeout, optional resume_run_id), look up each agent in `kb.agent_definitions`, spawn goroutines, wait with `sync.WaitGroup`, return SpawnResult array (run_id, status, summary, steps)
- [ ] 6.3 Handle invalid agent name in spawn: fail that specific spawn request, do not affect others
- [ ] 6.4 Implement spawn timeout: override agent definition's `default_timeout` when explicit timeout is provided
- [ ] 6.5 Implement context propagation: parent cancellation cascades to all sub-agents, individual sub-agent timeout only stops that sub-agent

## 7. Database Migrations — Phase 3

- [ ] 7.1 Create `kb.agent_run_messages` table: id (UUID PK), run_id (UUID FK), role (VARCHAR), content (JSONB), step_number (INT), created_at (TIMESTAMPTZ)
- [ ] 7.2 Create `kb.agent_run_tool_calls` table: id (UUID PK), run_id (UUID FK), message_id (UUID FK), tool_name (VARCHAR), input (JSONB), output (JSONB), status (VARCHAR), duration (INTERVAL), step_number (INT), created_at (TIMESTAMPTZ)
- [ ] 7.3 Add indexes: `agent_run_messages(run_id, step_number)`, `agent_run_tool_calls(run_id, step_number)`

## 8. State Persistence

- [ ] 8.1 Create AgentRunMessage and AgentRunToolCall entities and Bun models
- [ ] 8.2 Persist LLM messages (system, user, assistant, tool_result) during execution — write each message as it occurs, not after completion
- [ ] 8.3 Persist tool calls during execution: record tool_name, input JSONB, output JSONB, status (completed/error), duration, step_number
- [ ] 8.4 Preserve complete assistant message structure in content JSONB (tool call IDs, function names, arguments) for conversation reconstruction

## 9. Sub-Agent Resumption

- [ ] 9.1 Implement `resume_run_id` flow in `spawn_agents`: load prior AgentRun, load all AgentRunMessages ordered by step_number, reconstruct LLM conversation, append continuation message
- [ ] 9.2 Implement cumulative step counter: new execution starts step count where prior run left off
- [ ] 9.3 Implement fresh step budget per resume: `max_steps` applies per resume session, cumulative counter continues incrementing
- [ ] 9.4 Implement `MaxTotalStepsPerRun = 500` enforcement: refuse to resume runs that have reached the global cap
- [ ] 9.5 Reject resume of non-paused runs: return error for completed/failed runs

## 10. Agent Run History API

- [ ] 10.1 `GET /api/projects/:id/agent-runs` — list runs with cursor-based pagination, filters (status, agent_id, parent_run_id)
- [ ] 10.2 `GET /api/projects/:id/agent-runs/:runId` — run detail with summary, error_message, max_steps, parent_run_id, resumed_from
- [ ] 10.3 `GET /api/projects/:id/agent-runs/:runId/messages` — messages with cursor-based pagination, ordered by step_number
- [ ] 10.4 `GET /api/projects/:id/agent-runs/:runId/messages/:msgId` — single message with full content JSONB
- [ ] 10.5 `GET /api/projects/:id/agent-runs/:runId/tool-calls` — tool calls with cursor-based pagination, filters (tool_name, status)
- [ ] 10.6 `GET /api/projects/:id/agent-runs/:runId/tool-calls/:callId` — single tool call with full input/output JSONB
- [ ] 10.7 Add repository methods for message and tool-call queries with cursor-based pagination

## 11. Database Migrations — Phase 4

- [ ] 11.1 Create `kb.project_mcp_servers` table: id, project_id, product_id, name, description, transport (enum: stdio/sse), config (JSONB), enabled, created_at, updated_at

## 12. External MCP Connections

- [ ] 12.1 Create ProjectMCPServer entity and Bun model
- [ ] 12.2 Store MCP server configs from product manifests during product install
- [ ] 12.3 Build MCP client for stdio transport: spawn child process, communicate via stdin/stdout MCP protocol, pass environment variables, graceful termination (SIGTERM then SIGKILL)
- [ ] 12.4 Build MCP client for SSE transport: HTTP SSE connection with configured headers, reconnection with exponential backoff, error on tool calls during disconnection
- [ ] 12.5 Implement environment variable interpolation for MCP server configs (`${SEARCH_API_KEY}` from project secret store)
- [ ] 12.6 Implement tool discovery via MCP `tools/list`: wrap discovered tools as ADK tool functions, forward tool calls to MCP server, record calls in `kb.agent_run_tool_calls`
- [ ] 12.7 Implement lazy connection initialization: connect on first use at pipeline build time, not at server startup
- [ ] 12.8 Implement connection pooling per project: share MCP client connections across agents in the same project
- [ ] 12.9 Implement circuit breaker: mark server unavailable after 3 consecutive connection failures, immediate errors during 60-second cooldown, retry after cooldown

## 13. Task DAG Infrastructure

- [ ] 13.1 Create SpecTask template pack: object type with status/priority/assigned_agent/retry_count/failure_context/metrics fields, `blocks` relationship type
- [ ] 13.2 Implement DAG validation: cycle detection via topological sort on `blocks` relationship creation, reject cycles with descriptive error
- [ ] 13.3 Build available-task query: pending status + no incomplete blockers + no assigned agent, ordered by priority

## 14. TaskDispatcher

- [ ] 14.1 Create `domain/coordination/` module with fx wiring
- [ ] 14.2 Build Go polling loop: query available tasks, select agent, dispatch execution in goroutine, monitor completion
- [ ] 14.3 Implement concurrent dispatch: up to `maxConcurrent` parallel tasks, each in own goroutine
- [ ] 14.4 Implement task lifecycle: update SpecTask status (pending → in_progress → completed/failed), record metrics (completion_time, duration_seconds)
- [ ] 14.5 Implement predecessor context injection: gather summaries from completed blocking tasks, include in agent input
- [ ] 14.6 Implement task completion unlocking: detect newly available tasks (dependents unblocked) on next poll
- [ ] 14.7 Implement DAG completion detection: stop polling when all tasks are completed or skipped
- [ ] 14.8 Maintain active runs map: track task-to-run assignments, remove on completion/failure/cancellation

## 15. Agent Selection

- [ ] 15.1 Build `CodeSelector`: match tasks to agents via configured rules (task type → agent name mapping), no LLM call
- [ ] 15.2 Build `LLMSelector`: send task description + available agent summaries to fast model (Gemini Flash), return selected agent name
- [ ] 15.3 Build `HybridSelector`: try CodeSelector first, fall back to LLMSelector for ambiguous matches

## 16. Retry and Failure Handling

- [ ] 16.1 Implement retry logic: on task failure with `retry_count < max_retries`, reset status to pending, increment retry_count, store failure_context, clear assigned_agent
- [ ] 16.2 Implement max retries exceeded: set `status: failed`, preserve all retry attempt details in failure_context
- [ ] 16.3 Implement failure context injection: include previous error and agent output in retry input, instruct agent to learn from previous failure

## 17. Health Monitoring

- [ ] 17.1 Implement stale run detection: identify active runs exceeding their timeout, cancel and treat as failure
- [ ] 17.2 Implement goroutine leak prevention: `activeRuns` map tracking, health monitor goroutine that kills stale runs
