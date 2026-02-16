# Multi-Agent Coordination — Status & Todo

_Updated: February 15, 2026_

## Research Phase — Complete

- [x] Always-running agent patterns and background execution
- [x] Event-driven and schedule-driven trigger mechanisms
- [x] Agent communication patterns and coordination
- [x] State synchronization methods across agents
- [x] Specialized agent architecture patterns
- [x] Distributed decision making approaches
- [x] Agent orchestration systems
- [x] Fault tolerance and recovery mechanisms
- [x] Resource management for multi-agent systems
- [x] Real-world open source implementation examples (20+ projects analyzed)
- [x] OpenCode sub-agent constraints analysis (`research/opencode-subagent-constraints-analysis.md`)

## Design Phase — Complete

- [x] Architecture design (`multi-agent-architecture-design.md`)
- [x] Orchestration walkthrough — static DAG (`multi-agent-orchestration-walkthrough.md`)
- [x] TaskDispatcher design with 3-approach analysis (`task-coordinator-design.md`)
- [x] Align all docs with emergent codebase (remove external system references)
- [x] Align with product layer design (`docs/features/product-layer-design.md`)
- [x] Dynamic sub-agent spawning scenario (`research-agent-scenario-walkthrough.md`)
- [x] Dynamic agent selection — parent agent picks from project's agent catalog at runtime
- [x] Per-project MCP server configuration + per-agent tool filtering design
- [x] External MCP server connection model in product manifest
- [x] Sub-agent safety mechanisms design (step limits, timeouts, doom loop, recursive spawning prevention)
- [x] Full state persistence design (AgentRunMessage, AgentRunToolCall entities)
- [x] Sub-agent resumption design (resume_run_id, cumulative step counter)
- [x] Agent visibility levels (external/project/internal) and ACP configuration
- [x] Agent Run History API design (6 endpoints, progressive disclosure)

## Implementation Phase — Not Started

### Phase 1: Agent Execution (Week 1-2)

- [ ] Build `AgentExecutor` in `domain/agents/` using ADK-Go pipeline pattern from `domain/extraction/agents/pipeline.go`
- [ ] Wire MCP graph tools as ADK tool functions
- [ ] Track execution via AgentRun (status, duration, summary)
- [ ] Replace stubbed execution in `handler.go:421-422`
- [ ] Implement step limit enforcement — soft stop via prompt injection (`max-steps` message), hard stop fallback
- [ ] Implement `DoomLoopDetector` — threshold=3 identical consecutive tool calls → inject error message, then hard-stop

### Phase 2: Product-Defined Agents + Tool Filtering (Week 3-4)

- [ ] Create `kb.agent_definitions` table with columns: `max_steps`, `default_timeout`, `visibility`, `acp_config`
- [ ] Build agent registry that loads definitions from product manifests
- [ ] Wire agent definitions into chat module for interactive agents
- [ ] Build `ToolPool` — manages combined tool set per project (built-in + external MCP)
- [ ] Build `ResolveTools(toolNames)` — per-agent tool filtering from project's tool pool
- [ ] Enforce `SubAgentDeniedTools` — remove `spawn_agents` and `list_available_agents` from sub-agents at depth > 0
- [ ] Support glob patterns in tool lists (e.g., `"entities_*"`)
- [ ] Build `list_available_agents` tool — queries `kb.agent_definitions`, returns summaries (shows ALL agents regardless of visibility)
- [ ] Build `spawn_agents` tool with `timeout` and `resume_run_id` parameters
- [ ] Each sub-agent gets its OWN tools (from its definition, not parent's)
- [ ] `sync.WaitGroup` wait + aggregated results (SpawnResult with run_id, status, summary, steps)
- [ ] Context propagation for cancellation (Go `context.WithDeadline`)
- [ ] Support trigger-based agents (on_document_ingested, schedule)
- [ ] Implement agent visibility filtering on admin UI endpoints (external+project visible, internal hidden by default)

### Phase 3: State Persistence + Agent Run History (Week 5-6)

- [ ] Create `kb.agent_run_messages` table — full LLM conversation history per run (id, run_id, role, content JSONB, step_number, created_at)
- [ ] Create `kb.agent_run_tool_calls` table — tool invocation records (id, run_id, message_id, tool_name, input JSONB, output JSONB, status, duration, step_number, created_at)
- [ ] Extend `kb.agent_runs` with columns: `parent_run_id`, `step_count`, `max_steps`, `resumed_from`, `error_message`, `completed_at`
- [ ] Persist messages and tool calls during agent execution (not after — during, for crash recovery)
- [ ] Implement sub-agent resumption flow:
  - [ ] `resume_run_id` parameter in `spawn_agents` loads prior AgentRun + all AgentRunMessages
  - [ ] Reconstruct LLM conversation from persisted messages
  - [ ] Append new user message ("Continue your work...")
  - [ ] Cumulative step counter (StepCount carries forward across resumes)
  - [ ] Fresh step budget per resume (max_steps applies per resume, not cumulative)
- [ ] Implement `MaxTotalStepsPerRun = 500` global safety cap across all resumes
- [ ] Build Agent Run History API — 6 endpoints with progressive disclosure:
  - [ ] `GET /api/projects/:id/agent-runs` — list runs with cursor-based pagination + filters (status, agent_id, parent_run_id)
  - [ ] `GET /api/projects/:id/agent-runs/:runId` — single run details (summary, step_count, duration, status)
  - [ ] `GET /api/projects/:id/agent-runs/:runId/messages` — messages for a run with cursor pagination
  - [ ] `GET /api/projects/:id/agent-runs/:runId/messages/:msgId` — single message with full content
  - [ ] `GET /api/projects/:id/agent-runs/:runId/tool-calls` — tool calls with cursor pagination + filters (tool_name, status)
  - [ ] `GET /api/projects/:id/agent-runs/:runId/tool-calls/:callId` — single tool call with full input/output
- [ ] Upgrade existing `GetAgentRuns` handler to cursor-based pagination (currently offset-based)
- [ ] Add new repository methods for message/tool-call queries with cursor pagination

### Phase 4: External MCP Server Connections + Task DAG (Week 7-8)

- [ ] Create `kb.project_mcp_servers` table
- [ ] Build MCP client for `stdio` transport (spawn + manage child process)
- [ ] Build MCP client for `sse` transport (HTTP SSE connection)
- [ ] Tool discovery via MCP `tools/list` method
- [ ] Wrap external MCP tools as ADK tool functions
- [ ] Connection pooling per project (lazy initialization)
- [ ] Environment variable interpolation for secrets (`${SEARCH_API_KEY}`)
- [ ] Store product-defined MCP servers during product install
- [ ] Create SpecTask template pack (object type + `blocks` relationship type)
- [ ] Build task generation tool (change description → task DAG)
- [ ] Build available-task query (pending + unblocked + unassigned)
- [ ] Build task assignment and completion tools
- [ ] Support dynamic DAG creation by agents (agents can create SpecTask objects mid-execution)

### Phase 5: TaskDispatcher (Week 9-10)

- [ ] New `domain/coordination/` module with fx wiring
- [ ] Go polling loop: query available tasks → select agents → dispatch → monitor → complete
- [ ] Integrate timeout enforcement from agent definitions (`default_timeout`) and dispatcher config
- [ ] Handle paused runs (step limit / timeout) — store run_id for potential resumption
- [ ] Session tracking in graph (Session + SessionMessage entities)
- [ ] Retry logic with failure context injection

### Phase 6: Collaborative Intelligence (Week 11-14)

- [ ] LLM-based agent selection for ambiguous task-agent matching (AgentSelector)
- [ ] Output evaluation before marking task complete
- [ ] Failure triage (retry, reassign, or escalate)
- [ ] Cross-task context threading
- [ ] Discussion entity types and consensus building
- [ ] Multi-agent collaboration for complex tasks
- [ ] Admin UI dashboard for coordination visibility

### Database Migrations (by phase)

Phase 1:

- [ ] No new tables (uses existing `kb.agent_runs`)

Phase 2:

- [ ] `kb.agent_definitions` — add `max_steps INT`, `default_timeout INTERVAL`, `visibility VARCHAR DEFAULT 'project'`, `acp_config JSONB`

Phase 3:

- [ ] `kb.agent_run_messages` — new table (id UUID PK, run_id FK, role VARCHAR, content JSONB, step_number INT, created_at TIMESTAMPTZ)
- [ ] `kb.agent_run_tool_calls` — new table (id UUID PK, run_id FK, message_id FK, tool_name VARCHAR, input JSONB, output JSONB, status VARCHAR, duration INTERVAL, step_number INT, created_at TIMESTAMPTZ)
- [ ] `kb.agent_runs` — add columns: `parent_run_id UUID FK`, `step_count INT DEFAULT 0`, `max_steps INT`, `resumed_from UUID FK`, `error_message TEXT`, `completed_at TIMESTAMPTZ`
- [ ] Indexes: `agent_run_messages(run_id, step_number)`, `agent_run_tool_calls(run_id, step_number)`, `agent_runs(parent_run_id)`

Phase 4:

- [ ] `kb.project_mcp_servers` — new table (from product layer design)
