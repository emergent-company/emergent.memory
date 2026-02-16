## Why

Agent execution is stubbed — `handler.go:421-422` marks runs as `skipped`. Emergent has all the building blocks (ADK-Go pipelines, 30+ MCP graph tools, knowledge graph, job queues, scheduler) but nothing that takes an agent definition, builds a pipeline, and runs it. This blocks the entire product layer vision where products define domain-specific agents, and prevents the `graph-query-agent` approach that would replace the planned 3-service MCP Chat Integration Design with a single agent definition.

## What Changes

- **Agent execution engine**: `AgentExecutor` that builds ADK-Go pipelines from agent definitions and runs them as goroutines, replacing the stubbed execution
- **Tool management**: `ToolPool` per project combining built-in graph tools + external MCP server tools, with `ResolveTools()` for per-agent tool filtering (whitelists, glob patterns, read-only/write-only patterns)
- **Coordination tools**: `list_available_agents` (catalog query) and `spawn_agents` (parallel sub-agent execution with mixed types, timeout, resumption)
- **State persistence**: Full LLM conversation history (`kb.agent_run_messages`) and tool call records (`kb.agent_run_tool_calls`) persisted during execution for crash recovery, debugging, audit, and sub-agent resumption
- **Safety mechanisms**: Step limits (soft+hard stop), execution timeouts (context deadline with grace period), doom loop detection (3 identical consecutive tool calls), recursive spawning prevention, `MaxTotalStepsPerRun=500` global cap
- **Product-defined agents**: `kb.agent_definitions` table storing agent configs from product manifests with `max_steps`, `default_timeout`, `visibility`, `acp_config`
- **Agent visibility**: Three levels — `external` (ACP-exposed), `project` (admin-visible, default), `internal` (sub-agent only)
- **Agent Run History API**: 6 endpoints with progressive disclosure (overview → messages → tool calls) and cursor-based pagination
- **External MCP connections**: Client connections to external MCP servers (stdio + SSE transports) with lazy initialization, tool discovery, and credential management
- **Task DAG coordination**: `TaskDispatcher` that walks SpecTask dependency graphs, selects agents (code rules + LLM fallback), dispatches execution, handles retries with failure context injection
- **Trigger system**: Event-driven (`on_document_ingested`) and schedule-driven (cron) agent triggers using existing scheduler infrastructure

## Capabilities

### New Capabilities

- `agent-execution`: Core AgentExecutor that builds ADK-Go pipelines from agent definitions, executes them as goroutines, and tracks runs — includes step limit enforcement, timeout enforcement, and doom loop detection
- `agent-tool-pool`: ToolPool component that combines built-in graph tools with external MCP server tools per project, and ResolveTools for per-agent whitelist filtering with glob pattern support
- `agent-coordination-tools`: The `list_available_agents` and `spawn_agents` tools that enable parent agents to discover and delegate to sub-agents at runtime, with timeout and resumption support
- `agent-state-persistence`: Full conversation history and tool call persistence in `kb.agent_run_messages` and `kb.agent_run_tool_calls`, enabling sub-agent resumption via `resume_run_id` and the Agent Run History API
- `agent-definitions`: Product-defined agent configurations stored in `kb.agent_definitions` with visibility levels, ACP config, step limits, and timeout defaults — loaded from product manifests
- `external-mcp-connections`: MCP client connections to external servers (stdio + SSE transports) with `kb.project_mcp_servers` table, lazy initialization, tool discovery, and environment variable interpolation for secrets
- `task-dispatcher`: DAG-walking coordinator that queries available tasks, selects agents via HybridSelector (code rules + LLM fallback), dispatches execution, and handles retries with failure context injection

### Modified Capabilities

- `agent-infrastructure`: Extends existing agent persistence and scheduling with new execution engine, additional run columns (`parent_run_id`, `step_count`, `max_steps`, `resumed_from`, `error_message`, `completed_at`), and integration with product-defined agent definitions

## Impact

- **Database**: 4 new tables (`kb.agent_definitions`, `kb.agent_run_messages`, `kb.agent_run_tool_calls`, `kb.project_mcp_servers`), extended columns on `kb.agent_runs`, new indexes
- **Go server**: New `domain/coordination/` module, significant extensions to `domain/agents/` (executor, tool pool, registry), new MCP client in `domain/mcp/`
- **Existing code**: Replaces stubbed execution in `domain/agents/handler.go:421-422`, extends existing `AgentRun` entity, integrates with existing ADK pipeline pattern from `domain/extraction/agents/pipeline.go`
- **Chat module**: Wires interactive agents (agents with `is_default: true`) into the chat flow
- **Product layer**: Agents defined in product manifests are stored in `kb.agent_definitions` on product install
- **Admin UI**: Agent list filtered by visibility, agent run detail pages with progressive disclosure (runs → messages → tool calls)
- **API surface**: 6 new Agent Run History endpoints, extended agent trigger endpoint
- **Dependencies**: No new external dependencies — uses existing PostgreSQL, Vertex AI, ADK-Go
