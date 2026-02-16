# Multi-Agent Coordination for Emergent

_Feature Design — Started February 14, 2026_
_Updated: February 15, 2026 — Aligned with emergent reality, product layer design, and OpenCode sub-agent analysis_

## Vision

Emergent dispatches multiple specialized agents in parallel to work on tasks. The system supports **two coordination patterns**: (1) a **TaskDispatcher** that walks a dependency DAG stored in the knowledge graph, and (2) **agent-initiated coordination** where a parent agent dynamically discovers and spawns sub-agents at runtime using `list_available_agents` and `spawn_agents` tools.

Agents are **defined by products** (configuration bundles installed on projects) and execute via **ADK-Go pipelines** inside the emergent server. Some agents are built-in (shipped with `emergent.memory`), others come from installable products like `emergent.research` or `emergent.code`. Each agent gets a **filtered set of tools** from the project's ToolPool — enabling read-only agents, write-only agents, etc.

## Architecture Overview

```
Product Manifest (agent definitions + MCP server connections)
  ├── Agent Definitions (system prompt, model, tools whitelist, flow type)
  └── MCP Server Configs (external MCP servers per project)
        │
        ▼
  ┌─────────────────────────────────────────────────────────┐
  │  ToolPool (per project)                                  │
  │  ├── Built-in graph tools (domain/mcp/service.go)        │
  │  ├── External MCP server tools (kb.project_mcp_servers)  │
  │  └── ResolveTools(agentDef) → filtered tool set          │
  └─────────────────────────────────────────────────────────┘
        │
        ▼
  Two Coordination Patterns:
  ┌─────────────────────────────────────────────────────────┐
  │ Pattern 1: TaskDispatcher (Go)                           │
  │  ├── Walks task DAG (SpecTask + blocks relationships)    │
  │  ├── Selects agents via AgentSelector strategy           │
  │  ├── Dispatches via ADK-Go pipelines (goroutines)        │
  │  ├── Monitors completion via AgentRun tracking            │
  │  └── Stores sessions in graph                            │
  │                                                         │
  │ Pattern 2: Agent-Initiated Coordination                  │
  │  ├── Parent agent calls list_available_agents tool       │
  │  ├── LLM dynamically selects agent types per sub-task    │
  │  ├── Parent calls spawn_agents with mixed agent types    │
  │  └── Sub-agents run as goroutines, results returned      │
  └─────────────────────────────────────────────────────────┘
        │
        ▼
  ADK-Go Agent Pipelines
  ├── Each agent runs as a goroutine with ADK runner
  ├── Tools from ToolPool (filtered per agent definition)
  ├── LLM calls via Vertex AI (Gemini models)
  ├── Results tracked via kb.agent_runs
  └── Safety: step limits, timeouts, doom loop detection
        │
        ▼
  State Persistence
  ├── kb.agent_run_messages (full LLM conversation history)
  ├── kb.agent_run_tool_calls (every tool invocation with I/O)
  ├── Sub-agent resumption via resume_run_id
  └── Agent Run History API (progressive disclosure)
```

## What Already Exists in Emergent

| Component                | Status   | Location                                                                       |
| ------------------------ | -------- | ------------------------------------------------------------------------------ |
| **Agent entity**         | Exists   | `domain/agents/entity.go` — name, strategy, prompt, capabilities, config       |
| **AgentRun tracking**    | Exists   | `domain/agents/entity.go` — status, duration, summary, errors                  |
| **AgentProcessingLog**   | Exists   | `domain/agents/entity.go` — tracks which objects agents processed              |
| **Agent CRUD + routes**  | Exists   | `domain/agents/handler.go` — full REST API                                     |
| **Knowledge Graph**      | Exists   | `domain/graph/` — typed objects, relationships, BFS traversal, search          |
| **ADK-Go pipeline**      | Exists   | `domain/extraction/agents/pipeline.go` — multi-agent orchestration pattern     |
| **Model factory**        | Exists   | `pkg/adk/model.go` — creates Gemini models for LLM calls                       |
| **MCP tools**            | Exists   | `domain/mcp/service.go` — 30+ graph operation tools                            |
| **PostgreSQL job queue** | Exists   | `internal/jobs/queue.go` — generic queue with FOR UPDATE SKIP LOCKED           |
| **Template packs**       | Exists   | `domain/templatepacks/` — defines object/relationship schemas as data          |
| **Scheduler**            | Exists   | `domain/scheduler/` — cron-based scheduled tasks                               |
| **Product layer design** | Designed | `docs/features/product-layer-design.md` — products define agents + MCP servers |

### What's Missing (The Gap)

1. **Agent execution is stubbed** — `handler.go:421-422` says "execution not yet implemented in Go server" and marks runs as `skipped`
2. **No agent executor** — nothing takes an Agent definition, builds an ADK-Go pipeline, and runs it
3. **No ToolPool** — no component that combines built-in graph tools + external MCP server tools and filters per agent definition via `ResolveTools()`
4. **No external MCP connections** — emergent IS the MCP server (built-in tools only). No `kb.project_mcp_servers` table or client connections to external servers
5. **No coordination tools** — no `list_available_agents` or `spawn_agents` tools for agent-initiated coordination
6. **No task DAG** — no SpecTask entity type or `blocks` relationships in the graph
7. **No TaskDispatcher** — nothing walks the DAG, dispatches agents, and tracks completion
8. **No product-defined agents** — the `kb.agent_definitions` table from product-layer design doesn't exist yet
9. **No per-agent tool filtering** — `AgentCapabilities` in `domain/agents/entity.go` has `CanCreateObjects`, `CanUpdateObjects`, etc. but these are metadata only, not enforced
10. **No sub-agent safety mechanisms** — no step limits, timeouts, doom loop detection, or recursive spawning prevention
11. **No state persistence** — no `kb.agent_run_messages` or `kb.agent_run_tool_calls` tables for full conversation history
12. **No sub-agent resumption** — no `resume_run_id` parameter on `spawn_agents`, no cumulative step tracking
13. **No Agent Run History API** — no endpoints for inspecting agent runs with progressive disclosure (overview → messages → tool calls)
14. **No agent visibility levels** — no `external`/`project`/`internal` visibility on agent definitions

### How Products Define Agents

From the [product layer design](../product-layer-design.md), products include agent definitions in their manifest:

```json
{
  "agents": [
    {
      "name": "research-assistant",
      "system_prompt": "You are a research assistant...",
      "model": { "provider": "google", "name": "gemini-2.0-flash" },
      "tools": ["search_hybrid", "graph_traverse", "entities_get"],
      "trigger": null,
      "is_default": true
    },
    {
      "name": "paper-summarizer",
      "system_prompt": "Extract key findings from documents...",
      "trigger": "on_document_ingested",
      "tools": ["entities_*", "relationships_*"]
    }
  ],
  "mcp": {
    "servers": [
      {
        "name": "github",
        "transport": "sse",
        "url": "https://api.github.com/mcp",
        "env": { "GITHUB_TOKEN": "{{secrets.GITHUB_TOKEN}}" }
      }
    ]
  }
}
```

The `tools` field is a **whitelist** — each agent only sees the tools listed here from the project's ToolPool. Glob patterns are supported (e.g., `"entities_*"` matches `entities_create`, `entities_update`, etc.). This enables security boundaries: a read-only research agent can only see `search_*` and `graph_traverse`, while an implementation agent gets `entities_create`, `relationships_create`, etc.

The `mcp.servers` block defines **external MCP server connections** stored in `kb.project_mcp_servers`. These tools are combined with the built-in graph tools into a unified ToolPool per project.

These are stored in `kb.agent_definitions` and wire into the chat and coordination systems. Some agents are interactive (chat), others are triggered (on events or schedule), and the multi-agent coordinator dispatches them for complex tasks.

### Coordination Tools

Agents that need to coordinate other agents get two special tools:

| Tool                    | Purpose                                                                                                                                                                                                                                                                                                                         |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `list_available_agents` | Queries `kb.agent_definitions` for the current project. Returns agent summaries (name, description, tools, flow_type) — NOT full system prompts. The parent agent's LLM uses this to decide which agents to spawn.                                                                                                              |
| `spawn_agents`          | Spawns N sub-agents as goroutines. Each task specifies an `agent_name` (from the catalog), optional `timeout`, and optional `resume_run_id` for resuming paused runs. Sub-agents get their OWN tools from their definition, not the parent's. Sub-agents cannot call `spawn_agents` by default (recursive spawning prevention). |

**Dynamic agent selection**: Rather than hardcoding which sub-agent type to use, a parent agent calls `list_available_agents` to discover what's available, then the LLM decides which agent type fits each sub-task. This means adding a new agent to a product automatically makes it available to all coordinator agents.

## Design Decisions

### Resolved

| #   | Question                      | Decision                                     | Rationale                                                                                                                                                                                              |
| --- | ----------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | **Agent execution model**     | ADK-Go pipelines in goroutines               | Already proven by extraction pipeline. Vertex AI + Gemini models. No external process spawning needed.                                                                                                 |
| 2   | **Coordination approach**     | Hybrid (code loop + LLM judgment)            | Code handles mechanics (DAG walking, slot management, dispatch) at $0 cost. LLM called only for judgment: agent selection, output evaluation, failure triage.                                          |
| 3   | **Agent definitions**         | Product manifests + kb.agent_definitions     | Products provide domain-specific agent configurations. Built-in `emergent.memory` product provides defaults.                                                                                           |
| 4   | **State management**          | Knowledge graph entities                     | Tasks, sessions, and agent state stored as graph objects with relationships. Queryable, versionable, searchable.                                                                                       |
| 5   | **Tool management**           | ToolPool + per-agent filtering               | ToolPool combines built-in + external MCP tools per project. `ResolveTools()` filters by agent's `tools` whitelist. Security boundary enforced in Go.                                                  |
| 6   | **Two coordination patterns** | TaskDispatcher + spawn_agents                | DAG-driven workflows use TaskDispatcher. Ad-hoc sub-agent delegation uses `spawn_agents`. Both share the same ToolPool and AgentExecutor.                                                              |
| 7   | **Dynamic agent selection**   | list_available_agents + LLM choice           | Parent agents discover available agents at runtime via catalog query. LLM decides which agent type to spawn per sub-task. Adding new agents = automatically available.                                 |
| 8   | **External MCP servers**      | kb.project_mcp_servers + lazy connect        | Per-project external MCP server connections. Stored in DB, connected lazily on first tool call. Supports stdio + SSE transports.                                                                       |
| 9   | **Agent visibility**          | Three levels: external/project/internal      | `project` is default. `external` enables ACP/external API access. `internal` hides from admin UI (sub-agent only). `list_available_agents` and `spawn_agents` see ALL agents regardless of visibility. |
| 10  | **State persistence**         | Full message history + tool call records     | Every agent run persists complete conversation in `kb.agent_run_messages` and tool calls in `kb.agent_run_tool_calls`. Enables resumption, debugging, audit trail. ~50-500KB per run.                  |
| 11  | **Sub-agent safety**          | Step limits + timeouts + doom loop detection | `max_steps` (default 50 for sub-agents, nil for top-level), `default_timeout` with soft+hard stop, `DoomLoopDetector` (3 identical calls), recursive spawning prevention, `MaxTotalStepsPerRun=500`.   |

### Open

| #   | Question                | Options                                    | Recommendation                                                                                |
| --- | ----------------------- | ------------------------------------------ | --------------------------------------------------------------------------------------------- |
| 12  | **Agent pool**          | Ephemeral goroutines vs persistent workers | **Ephemeral goroutines** — clean state, simple lifecycle, follows extraction pipeline pattern |
| 13  | **User visibility**     | Admin UI dashboard / notifications         | TBD — leverage existing admin UI patterns                                                     |
| 14  | **Multi-project scope** | One project at a time vs cross-project     | Single project — matches existing project scoping                                             |

## Document Map

### Design Documents (this directory)

| File                                       | Description                                                                           |
| ------------------------------------------ | ------------------------------------------------------------------------------------- |
| `multi-agent-architecture-design.md`       | Architecture design with ToolPool, ResolveTools, coordination tools, MCP config       |
| `multi-agent-orchestration-walkthrough.md` | Walkthrough: TaskDispatcher-driven DAG coordination with retry loops                  |
| `research-agent-scenario-walkthrough.md`   | Walkthrough: Agent-initiated coordination with dynamic agent selection + spawn_agents |
| `task-coordinator-design.md`               | TaskDispatcher design, three approaches (Code/LLM/Hybrid), sessions-in-graph model    |
| `todo.md`                                  | Implementation checklist (7 steps) and status                                         |

### Research Documents (`research/`)

| File                                                  | Description                                                                                    |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `opencode-subagent-constraints-analysis.md`           | OpenCode sub-agent analysis — step limits, timeouts, doom loops, state persistence, visibility |
| `Multi-Agent_Systems_Architecture_Research_Report.md` | Comprehensive multi-agent research — patterns, frameworks, state management                    |
| `investigation-similar-projects.md`                   | Analysis of 20+ similar open source projects                                                   |
| `agent-communication-patterns-research.md`            | Communication patterns with Go implementation examples                                         |
| `opencode-vs-diane-orchestration-comparison.md`       | Orchestration comparison and sessions analysis                                                 |

### Related Documents

| File                         | Description                                            |
| ---------------------------- | ------------------------------------------------------ |
| `../product-layer-design.md` | Product layer — how agents are defined and distributed |

## Implementation Plan

See `todo.md` for the detailed implementation checklist with per-task tracking.

### Phase 1: Agent Execution (Week 1-2)

- Build `AgentExecutor` in `domain/agents/` using ADK-Go pipeline pattern
- Wire MCP graph tools as ADK tool functions
- Track via AgentRun with proper status/duration/summary
- Replace stubbed execution in `handler.go` TriggerAgent
- Implement step limit enforcement (soft stop via prompt injection, hard stop fallback)
- Implement doom loop detection (`DoomLoopDetector`, threshold=3)

### Phase 2: Product-Defined Agents + Tool Filtering (Week 3-4)

- Create `kb.agent_definitions` table with `max_steps`, `default_timeout`, `visibility`, `acp_config` columns
- Build agent registry that stores definitions from product manifests
- Build ToolPool component: combines built-in graph tools + external MCP server tools per project
- Implement `ResolveTools(agentDef)` — filters ToolPool by agent's `tools` whitelist (supports glob patterns)
- Enforce `SubAgentDeniedTools` — recursive spawning prevention for sub-agents
- Build `list_available_agents` and `spawn_agents` coordination tools (with `timeout` and `resume_run_id`)
- Wire agent definitions into chat module for interactive agents
- Support trigger-based agents (on_document_ingested, schedule)
- Implement agent visibility filtering on admin UI endpoints

### Phase 3: State Persistence + Agent Run History (Week 5-6)

- Create `kb.agent_run_messages` and `kb.agent_run_tool_calls` tables
- Extend `kb.agent_runs` with `parent_run_id`, `step_count`, `max_steps`, `resumed_from` columns
- Implement sub-agent resumption flow (cumulative step counter, fresh budget per resume)
- Implement `MaxTotalStepsPerRun = 500` global safety cap
- Build Agent Run History API (6 endpoints with progressive disclosure)
- Upgrade existing `GetAgentRuns` to cursor-based pagination

### Phase 4: External MCP Connections + Task DAG (Week 7-8)

- Create `kb.project_mcp_servers` table
- Build MCP client for stdio + SSE transports with lazy connection lifecycle
- Integrate external tools into ToolPool
- Create SpecTask template pack (object type + `blocks` relationship type)
- Build task generation tool and available-task queries

### Phase 5: TaskDispatcher (Week 9-10)

- New `domain/coordination/` module with Go polling loop
- Agent selection (CodeSelector + LLMSelector + HybridSelector)
- Timeout enforcement and paused run handling
- Session tracking in graph (Session + SessionMessage entities)
- Retry logic with failure context injection

### Phase 6: Collaborative Intelligence (Week 11-14)

- LLM-based agent selection for ambiguous task-agent matching
- Output evaluation before marking task complete
- Failure triage (retry, reassign, or escalate)
- Cross-task context threading
- Discussion entity types and consensus building
- Admin UI dashboard for coordination visibility

## Source Code Locations (in emergent)

- `apps/server-go/domain/agents/` — Agent entity, CRUD, runs, processing log
- `apps/server-go/domain/graph/` — Knowledge graph service
- `apps/server-go/domain/mcp/service.go` — MCP tools (graph operations)
- `apps/server-go/domain/extraction/agents/pipeline.go` — ADK-Go pipeline pattern
- `apps/server-go/pkg/adk/model.go` — Vertex AI model factory
- `apps/server-go/internal/jobs/queue.go` — PostgreSQL job queue
- `apps/server-go/domain/templatepacks/` — Template pack definitions
- `apps/server-go/domain/scheduler/` — Cron scheduler
