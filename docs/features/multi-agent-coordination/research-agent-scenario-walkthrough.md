# Research Agent Scenario: Dynamic Sub-Agent Spawning Walkthrough

_Design Validation Date: February 15, 2026_

## The Scenario

**A user asks the research agent to investigate a topic.** The research agent:

1. Analyzes the topic to determine what information is needed
2. **Queries the agent catalog** to see all available agent definitions for the project
3. **Dynamically decides** which agents to spawn and how many instances of each (not known upfront)
4. Dispatches N sub-agents in parallel, choosing the right agent type for each sub-task
5. Waits for all sub-agents to complete
6. Aggregates and synthesizes findings
7. Saves structured objects to the knowledge graph

**Why this scenario is a stress test:** The existing orchestration walkthrough uses a **static task DAG** — all tasks are known at Phase 0 and created as SpecTask objects with `blocks` relationships. This scenario requires a parent agent to **dynamically decide at runtime** both which agent types to use AND how many instances to spawn. The agent selection and task count aren't known until the parent agent's first LLM call returns.

### Key Design Principle: Dynamic Agent Selection

The parent agent does NOT hardcode which sub-agent to spawn. Instead:

1. The `spawn_agents` tool provides the parent with a **catalog of all available agent definitions** for the project (loaded from `kb.agent_definitions`)
2. The parent's LLM decides which agent(s) are appropriate for each sub-task based on the agent descriptions, tool sets, and capabilities
3. The parent can spawn **different agent types** for different sub-tasks — e.g., a `web-browser` agent for web research AND a `paper-summarizer` agent for document analysis in the same parallel batch

This means adding a new agent definition to the project immediately makes it available as a sub-agent — no code changes needed.

---

## Concrete Example

**User prompt:** "Research the current state of WebAssembly for server-side applications. Find papers, blog posts, and benchmarks."

**Expected behavior:**

1. Research agent receives the prompt
2. LLM call → sees available agents: `web-browser`, `paper-summarizer`, `data-analyst` (from project's agent catalog)
3. Decides: "I need 3 web-browser agents for live web research, plus 1 paper-summarizer to process any papers already in the knowledge graph"
4. Spawns 4 sub-agents in parallel (3x web-browser, 1x paper-summarizer)
5. Each sub-agent executes with its own tools, model, and system prompt
6. Research agent receives all 4 results
7. Synthesizes into a coherent research report
8. Saves to the knowledge graph: ResearchReport object, multiple Source objects, relationships between them

---

## How This Maps to the Current Architecture

### Pattern 1: Pure ADK-Go Pipeline (No TaskDispatcher)

The most natural fit is to handle this **entirely within a single ADK-Go pipeline execution** — the parent research agent is itself an ADK agent that has a `spawn_agents` tool. This bypasses the TaskDispatcher entirely because the sub-agent spawning is an internal implementation detail of one agent's execution, not a multi-task workflow.

```
AgentExecutor.Execute(researchAgentDef, userPrompt)
  │
  └── ADK-Go Pipeline (single goroutine initially)
       │
       ├── Tool Call: list_available_agents()
       │   → Returns catalog of all agent definitions for this project:
       │     [web-browser, paper-summarizer, data-analyst, ...]
       │
       ├── LLM Call 1: "Analyze topic + agent catalog, decide strategy"
       │   → Output: 4 sub-tasks, each assigned to a specific agent type
       │
       ├── Tool Call: spawn_agents(tasks)
       │   → Go code spawns 4 goroutines with different agent types
       │   │
       │   ├── goroutine 1: web-browser agent → searches papers online
       │   ├── goroutine 2: web-browser agent → searches case studies online
       │   ├── goroutine 3: web-browser agent → searches benchmarks online
       │   └── goroutine 4: paper-summarizer agent → analyzes papers in KG
       │   │
       │   └── sync.WaitGroup.Wait() → all 4 complete
       │       → aggregated results returned to parent
       │
       ├── LLM Call 2: "Synthesize all findings"
       │   → Output: structured research report
       │
       └── Tool Calls: create_entity, create_relationship
           → Saves ResearchReport, Source objects to graph
```

#### How This Works in Code

```go
// The research agent has two coordination tools:
//   1. list_available_agents — returns agent catalog
//   2. spawn_agents — spawns sub-agents in parallel

// list_available_agents returns all agent definitions for the project
func (e *AgentExecutor) listAvailableAgents(ctx context.Context) ([]AgentSummary, error) {
    defs, err := e.registry.ListAgentDefs(ctx, e.projectID)
    if err != nil {
        return nil, err
    }

    summaries := make([]AgentSummary, len(defs))
    for i, def := range defs {
        summaries[i] = AgentSummary{
            Name:        def.Name,
            Description: def.Description,
            Tools:       def.Tools,
            FlowType:    def.FlowType,
        }
    }
    return summaries, nil
}

// spawn_agents spawns sub-agents in parallel — each task specifies which agent to use
func (e *AgentExecutor) spawnAgents(ctx context.Context, tasks []SubAgentTask) ([]SubAgentResult, error) {
    var wg sync.WaitGroup
    results := make([]SubAgentResult, len(tasks))
    errs := make([]error, len(tasks))

    for i, task := range tasks {
        wg.Add(1)
        go func(idx int, t SubAgentTask) {
            defer wg.Done()

            // Look up the agent definition by name — the LLM chose this
            agentDef, err := e.registry.GetAgentDef(ctx, e.projectID, t.AgentName)
            if err != nil {
                errs[idx] = fmt.Errorf("agent %q not found: %w", t.AgentName, err)
                return
            }

            // Execute as a nested ADK pipeline
            run, err := e.Execute(ctx, agentDef, t.Prompt)
            if err != nil {
                errs[idx] = err
                return
            }

            results[idx] = SubAgentResult{
                AgentName:       t.AgentName,
                TaskDescription: t.Description,
                Findings:        run.Summary,
            }
        }(i, task)
    }

    wg.Wait()

    // Collect errors
    var failedTasks []string
    for i, err := range errs {
        if err != nil {
            failedTasks = append(failedTasks, fmt.Sprintf("task %d (%s): %v", i, tasks[i].AgentName, err))
        }
    }

    if len(failedTasks) > 0 && len(failedTasks) == len(tasks) {
        return nil, fmt.Errorf("all sub-agents failed: %s", strings.Join(failedTasks, "; "))
    }

    // Return successful results (partial success is OK)
    return filterSuccessful(results), nil
}

// SubAgentTask includes which agent to use — chosen by the parent's LLM
type SubAgentTask struct {
    AgentName   string `json:"agent_name"`   // e.g., "web-browser" or "paper-summarizer"
    Description string `json:"description"`
    Prompt      string `json:"prompt"`
}
```

#### Agent Definition for This Pattern

The research agent's product manifest defines it with coordination tools (`list_available_agents` + `spawn_agents`):

```json
{
  "name": "research-assistant",
  "system_prompt": "You are a research assistant. When given a topic, you:\n1. Call list_available_agents to see what agent types are available\n2. Analyze the topic and identify 2-6 specific sub-tasks\n3. For each sub-task, choose the most appropriate agent from the catalog\n4. Use spawn_agents to execute all sub-tasks simultaneously\n5. Synthesize the results into a coherent report\n6. Save the report and sources to the knowledge graph using create_entity and create_relationship",
  "model": { "provider": "google", "name": "gemini-2.5-pro" },
  "tools": [
    "list_available_agents",
    "spawn_agents",
    "create_entity",
    "create_relationship",
    "search_hybrid"
  ],
  "trigger": null,
  "flow_type": "single"
}
```

Available sub-agents (defined by the same or other products installed on the project):

```json
[
  {
    "name": "web-browser",
    "description": "Browses the web to find and extract information from URLs. Good for finding recent blog posts, documentation, and live web content.",
    "system_prompt": "You are a web browsing agent. Search the web for the given topic and extract structured information. Return: title, URL, key findings, relevance score.",
    "model": { "provider": "google", "name": "gemini-2.5-flash" },
    "tools": ["web_search", "web_fetch"],
    "trigger": null,
    "flow_type": "single",
    "is_default": false
  },
  {
    "name": "paper-summarizer",
    "description": "Analyzes academic papers and documents already in the knowledge graph. Extracts methodology, key findings, and conclusions.",
    "system_prompt": "You analyze documents in the knowledge graph. Search for relevant documents, read them, and extract structured information.",
    "model": { "provider": "google", "name": "gemini-2.5-flash" },
    "tools": ["search_hybrid", "get_entity", "search_fts"],
    "trigger": "on_document_ingested",
    "flow_type": "single",
    "is_default": false
  },
  {
    "name": "data-analyst",
    "description": "Analyzes structured data, produces summaries and comparisons. Good for benchmarks and quantitative analysis.",
    "system_prompt": "You analyze data and produce structured comparisons...",
    "model": { "provider": "google", "name": "gemini-2.5-pro" },
    "tools": ["search_hybrid", "graph_traverse"],
    "trigger": null,
    "flow_type": "single",
    "is_default": false
  }
]
```

**The parent agent's LLM sees all these descriptions and decides which agent to use for each sub-task.** Adding a new agent definition to the project immediately makes it available — no changes to the research agent needed.

---

### Pattern 2: Hybrid — Parent Agent Creates Dynamic Task DAG

Alternatively, the research agent can create SpecTask objects in the graph (dynamic DAG creation), and then the TaskDispatcher handles the rest. This is closer to the existing walkthrough pattern but with the DAG created mid-execution rather than at Phase 0.

```
Phase 0: Task planner creates initial DAG
  SpecTask("research-wasm")  status: pending

Phase 1: TaskDispatcher dispatches research agent
  Research agent runs LLM → decides 4 search areas
  Research agent creates 4 NEW SpecTask objects in graph:
    SpecTask("search-papers")      status: pending
    SpecTask("search-case-studies") status: pending
    SpecTask("search-benchmarks")  status: pending
    SpecTask("search-components")  status: pending
  Research agent creates a synthesis task blocked by all 4:
    SpecTask("synthesize-report")  status: pending
      blocked by: search-papers, search-case-studies, search-benchmarks, search-components
  Research agent marks itself as completed

Phase 2: TaskDispatcher finds 4 unblocked search tasks
  Dispatches all 4 in parallel (up to maxConcurrent)
  Each runs the web-browser agent definition

Phase 3: All 4 complete → synthesize-report unblocked
  TaskDispatcher dispatches research-assistant for synthesis
  Agent reads all search results from graph
  Creates ResearchReport + Source objects + relationships

Phase 4: Complete
```

#### How This Works in Code

```go
// The research agent has a tool: create_subtasks
// This creates SpecTask objects in the graph dynamically

func (e *AgentExecutor) createSubtasks(ctx context.Context, parentTaskID string, subtasks []SubtaskSpec) error {
    subtaskIDs := make([]string, len(subtasks))

    // Create each subtask as a SpecTask in the graph
    for i, st := range subtasks {
        obj, err := e.graphService.CreateObject(ctx, "SpecTask", map[string]any{
            "title":       st.Title,
            "description": st.Description,
            "status":      "pending",
            "priority":    "medium",
            "assigned_agent": st.AgentName,  // optional: suggest which agent
            "retry_count": 0,
            "max_retries": 2,
        })
        if err != nil {
            return err
        }
        subtaskIDs[i] = obj.ID
    }

    // Create a synthesis task blocked by all subtasks
    synthTask, err := e.graphService.CreateObject(ctx, "SpecTask", map[string]any{
        "title":       "Synthesize findings",
        "description": "Aggregate results from all search subtasks into a research report",
        "status":      "pending",
        "priority":    "high",
        "assigned_agent": "research-assistant",
        "retry_count": 0,
        "max_retries": 2,
    })
    if err != nil {
        return err
    }

    // Create blocks relationships: each subtask blocks the synthesis task
    for _, subtaskID := range subtaskIDs {
        _, err := e.graphService.CreateRelationship(ctx, subtaskID, synthTask.ID, "blocks")
        if err != nil {
            return err
        }
    }

    return nil
}
```

---

## Comparison: Pattern 1 vs Pattern 2

| Dimension             | Pattern 1: ADK-Go Tool                                  | Pattern 2: Dynamic DAG                                        |
| --------------------- | ------------------------------------------------------- | ------------------------------------------------------------- |
| **Simplicity**        | Simpler — self-contained within one execution           | More complex — involves TaskDispatcher, graph entities        |
| **Observability**     | Limited — sub-agents are goroutines inside one AgentRun | Full — each sub-agent has its own SpecTask, Session, AgentRun |
| **Agent selection**   | Parent LLM picks from catalog via list_available_agents | Task's `assigned_agent` field drives selection                |
| **Error handling**    | Parent handles failures inline                          | TaskDispatcher retry logic applies per subtask                |
| **State persistence** | Sub-agent results live in parent's memory only          | Sub-agent results stored in graph (durable)                   |
| **Cancellation**      | Cancel parent → all children cancelled (via context)    | Can cancel individual subtasks independently                  |
| **Cost tracking**     | Aggregated under one AgentRun                           | Per-subtask cost tracking                                     |
| **Timeout**           | Single timeout for entire execution                     | Per-subtask timeouts                                          |
| **Reuse**             | Pattern is agent-specific Go code                       | Pattern is generic — any agent can create subtasks            |
| **Architecture fit**  | Extends AgentExecutor only                              | Uses existing TaskDispatcher + graph entities                 |

---

## Recommendation: Pattern 1 for V1, Pattern 2 for V2

**Pattern 1 (ADK-Go tool)** is the right starting point because:

1. It requires **no new infrastructure** — just a Go function registered as an ADK tool
2. It follows the existing extraction pipeline pattern exactly (the pipeline already runs sub-agents internally)
3. The research scenario doesn't need the full weight of TaskDispatcher (no cross-workflow dependencies, no complex retry policies)
4. It's straightforward to implement and test

**Pattern 2 (Dynamic DAG)** becomes valuable when:

1. Sub-agents need **individual observability** (per-subtask dashboards, cost tracking)
2. Sub-agents might **take minutes** and you need durable state (surviving server restarts)
3. Other agents or workflows need to **reference individual subtask outputs**
4. You need **per-subtask retry policies** independent of the parent

The good news: both patterns can coexist. A research agent could use Pattern 1 for quick parallel web searches (30s total), while a multi-day research workflow could use Pattern 2 for durability.

---

## Step-by-Step Walkthrough: Pattern 1 (Recommended)

### Step 1: User Triggers Research (~0s)

```
User: "Research the current state of WebAssembly for server-side applications"
  │
  ▼
POST /api/agents/:id/trigger
  body: { input: "Research the current state of..." }
  │
  ▼
AgentExecutor.Execute(researchAgentDef, input)
  → Creates AgentRun (status: running)
  → Builds ADK-Go pipeline from AgentDefinition
  → Starts execution in goroutine
```

### Step 2: Research Agent Analyzes Topic (~3s)

```
ADK Pipeline: research-assistant
Model: gemini-2.5-pro via Vertex AI
Tools: list_available_agents, spawn_agents, create_entity, create_relationship, search_hybrid

Tool Call 1: list_available_agents()
  → Returns:
    [
      { "name": "web-browser", "description": "Browses the web...", "tools": ["web_search", "web_fetch"] },
      { "name": "paper-summarizer", "description": "Analyzes papers in KG...", "tools": ["search_hybrid", "get_entity"] },
      { "name": "data-analyst", "description": "Analyzes structured data...", "tools": ["search_hybrid", "graph_traverse"] }
    ]

LLM Call 1:
  System: "You are a research assistant..."
  User: "Research the current state of WebAssembly for server-side applications.
         Find papers, blog posts, and benchmarks."
  Tool result: [agent catalog above]

  Assistant response:
    "I have 3 agent types available. For this research task, I'll use:
     - 3x web-browser agents for live web research (papers, case studies, benchmarks)
     - 1x paper-summarizer to check if we already have relevant papers in the knowledge graph
     The data-analyst isn't needed here — the web-browser agents can find benchmark data directly."

  Tool call: spawn_agents({
    tasks: [
      {
        agent_name: "web-browser",
        description: "Academic papers on WASM runtimes",
        prompt: "Search for recent academic papers (2024-2026) about WebAssembly runtime performance. Focus on Wasmtime, Wasmer, WasmEdge. Find: paper titles, authors, key findings about cold start times, memory usage, and throughput."
      },
      {
        agent_name: "web-browser",
        description: "Production WASM case studies",
        prompt: "Find production case studies of WebAssembly used for server-side applications. Focus on: Cloudflare Workers, Fermyon Spin, Fastly Compute. Find: company name, use case, performance claims, limitations reported."
      },
      {
        agent_name: "web-browser",
        description: "WASM vs container benchmarks",
        prompt: "Search for benchmarks comparing WebAssembly to Docker containers and native executables for server-side workloads. Find: test methodology, cold start comparison, throughput comparison, memory overhead comparison."
      },
      {
        agent_name: "paper-summarizer",
        description: "Existing papers in knowledge graph",
        prompt: "Search the knowledge graph for any existing papers or documents related to WebAssembly, WASM runtimes, or serverless computing. Summarize any relevant findings."
      }
    ]
  })
```

### Step 3: Parallel Execution with Mixed Agent Types (~10-20s)

```
spawn_agents tool executes:
  │
  ├── goroutine 1: web-browser agent (looked up from registry)
  │   Model: gemini-2.5-flash (from web-browser's definition)
  │   Tools: web_search, web_fetch (from web-browser's tool list)
  │   Prompt: "Search for recent academic papers..."
  │   │
  │   ├── web_search("WebAssembly runtime performance 2024 2025 academic paper")
  │   ├── web_fetch(url1) → extracts paper details
  │   ├── web_fetch(url2) → extracts paper details
  │   └── Returns: 3 papers with titles, abstracts, key findings
  │
  ├── goroutine 2: web-browser agent
  │   Model: gemini-2.5-flash
  │   Tools: web_search, web_fetch
  │   Prompt: "Find production case studies..."
  │   │
  │   ├── web_search("WebAssembly server production case study")
  │   ├── web_fetch(url3) → Cloudflare blog post
  │   ├── web_fetch(url4) → Fermyon case study
  │   └── Returns: 4 case studies with details
  │
  ├── goroutine 3: web-browser agent
  │   Model: gemini-2.5-flash
  │   Tools: web_search, web_fetch
  │   Prompt: "Search for benchmarks..."
  │   │
  │   ├── web_search("WebAssembly vs Docker benchmark server")
  │   ├── web_fetch(url5) → benchmark results
  │   └── Returns: 2 benchmark comparisons
  │
  └── goroutine 4: paper-summarizer agent (DIFFERENT agent type!)
      Model: gemini-2.5-flash (from paper-summarizer's definition)
      Tools: search_hybrid, get_entity, search_fts (from paper-summarizer's tool list)
      Prompt: "Search knowledge graph for existing papers..."
      │
      ├── search_hybrid("WebAssembly WASM runtime serverless")
      ├── get_entity(doc_id_1) → existing paper in KG
      └── Returns: 1 existing paper with extracted findings

  sync.WaitGroup.Wait() → all 4 complete

  Tool result returned to parent agent:
  {
    results: [
      { agent: "web-browser", task: "Academic papers", findings: [...3 papers...] },
      { agent: "web-browser", task: "Case studies", findings: [...4 case studies...] },
      { agent: "web-browser", task: "Benchmarks", findings: [...2 benchmarks...] },
      { agent: "paper-summarizer", task: "Existing KG papers", findings: [...1 paper...] }
    ],
    failed: []
  }
```

### Step 4: Synthesis (~5s)

```
LLM Call 2 (research-assistant):
  System: "You are a research assistant..."
  User: [original prompt]
  Assistant: [previous analysis + tool call]
  Tool result: [aggregated findings from 4 sub-agents]

  Assistant response:
    "Based on my research across 4 areas, here's the synthesis:

     ## Key Findings
     1. WASM runtimes show 10-100x faster cold starts vs containers
     2. Production adoption is real but still early (Cloudflare, Fermyon)
     3. Component Model is the key enabler for ecosystem growth
     4. Main limitation: limited language support and debugging tools

     I'll now save this to the knowledge graph."

  Tool calls (sequential):
    1. create_entity("ResearchReport", {
         title: "WebAssembly for Server-Side Applications: 2026 State of the Art",
         summary: "...",
         methodology: "Parallel web search across 4 areas",
         key_findings: [...],
         limitations: [...],
         created_at: "2026-02-15T..."
       })

    2. create_entity("Source", {
         title: "Performance Analysis of WASM Runtimes",
         url: "https://...",
         type: "academic_paper",
         key_findings: "..."
       })
       ... (repeat for each source)

    3. create_relationship(researchReport.id, source1.id, "CITES")
       create_relationship(researchReport.id, source2.id, "CITES")
       ... (repeat for each source)
```

### Step 5: Completion (~0s)

```
ADK Pipeline completes
AgentRun updated:
  status: "completed"
  duration: 28s
  summary: "Research report created with 9 sources across 4 areas"
  tokens_used: 15,000 (parent) + 4 × 8,000 (sub-agents) = 47,000

Graph state after completion:

  ResearchReport("WASM for Server-Side: 2026")
    ├── CITES ──► Source("Performance Analysis of WASM Runtimes")
    ├── CITES ──► Source("Wasmtime vs Wasmer: A Comparative Study")
    ├── CITES ──► Source("Inside Cloudflare Workers")
    ├── CITES ──► Source("Building with Fermyon Spin")
    ├── CITES ──► Source("Fastly Compute at Scale")
    ├── CITES ──► Source("WASM vs Containers: Benchmark Suite")
    ├── CITES ──► Source("Startup Latency Comparison 2025")
    ├── CITES ──► Source("Component Model Specification Status")
    └── CITES ──► Source("WASI Preview 2 Runtime Support")
```

---

## Gaps Identified

### Gap 1: Coordination Tools Do Not Exist

**Status:** Needs implementation

Two coordination tools are needed:

1. **`list_available_agents`** — Returns the catalog of all agent definitions for the project from `kb.agent_definitions`. This is a read-only query. The parent's LLM uses the agent descriptions to decide which ones to spawn.

2. **`spawn_agents`** — Takes a list of sub-agent tasks (each with an `agent_name` field), looks up each agent definition by name, and executes them in parallel as goroutines.

**Solution:** Add both as built-in coordination tools in `domain/agents/executor.go`. They are registered as ADK tool functions when an agent's tool list includes them. `spawn_agents` is a **privileged tool** — it requires access to the `AgentExecutor` and registry, unlike simple graph tools.

**Complexity:** Medium. The main challenges are:

- Bridging the ADK tool interface with Go concurrency
- Ensuring each sub-agent gets the correct tools based on its own definition (not the parent's tools)
- The extraction pipeline already does something similar internally

### Gap 2: Per-Agent Tool Filtering Not Enforced

**Status:** Needs implementation

Agent definitions list tools (e.g., `["web_search", "web_fetch"]`), but the `AgentExecutor` doesn't filter tools when building the ADK pipeline. Currently `mcpService.GetToolDefinitions()` returns ALL tools. The executor needs to call `mcpService.ResolveTools(def.Tools)` which returns ONLY the tools listed in the agent definition.

**Solution:** Add `ResolveTools(toolNames []string) []ToolDefinition` to the MCP service. When building an ADK pipeline for an agent, only wire the resolved tools. This is critical for security — a read-only agent like `paper-summarizer` should NOT have access to `create_entity` or `delete_entity` even if the MCP server exposes them.

See the MCP configuration section in `multi-agent-architecture-design.md` for the full per-agent tool filtering design.

### Gap 2: Web Browsing Tools Do Not Exist

**Status:** Needs implementation (separate from multi-agent design)

The scenario assumes `web_search` and `web_fetch` tools are available. These are external capabilities that would need:

- A search API integration (Google Search API, Serper, etc.)
- A web fetching capability (HTTP GET + HTML parsing)

**Solution:** These are MCP tools that can be implemented independently. They're not specific to multi-agent coordination — any single agent could use them. They would be registered in `domain/mcp/service.go` or provided by an external MCP server.

### Gap 3: Sub-Agent Results Not Persisted in Graph

In Pattern 1, sub-agent results live only in the parent agent's ADK session memory. If the parent crashes after sub-agents complete but before synthesis, all sub-agent work is lost.

**Solution options:**

- **Accept the risk for V1.** Research sub-agents typically complete in 10-20s each. The window for data loss is small.
- **Persist intermediate results.** The `spawn_agents` tool could write each sub-agent's output to the graph as a temporary object, then the synthesis step reads from graph instead of from tool result memory.
- **Use Pattern 2** for workflows where durability matters.

### Gap 4: No Token Budget Enforcement Across Sub-Agents

The parent agent's AgentRun tracks tokens for its own LLM calls, but sub-agent tokens are tracked separately (each has its own AgentRun). There's no mechanism to enforce a total token budget across parent + children.

**Solution:** The `spawn_agents` tool should accept a `max_tokens_per_subtask` parameter and pass it through to each sub-agent's execution context. The parent's AgentRun should aggregate sub-agent token usage in its metrics.

### Gap 5: No Cancellation Propagation

If the parent agent is cancelled (timeout or user cancellation), in-flight sub-agent goroutines should also be cancelled. This requires proper `context.Context` propagation.

**Solution:** Already handled by Go's context pattern — the parent passes `ctx` to sub-goroutines, and cancelling the parent's context cancels all children. Just need to ensure the implementation uses `ctx` properly.

### Gap 6: Agent Catalog Must Be Queryable at Runtime

The `kb.agent_definitions` table stores all agent definitions for a project, but there's no query interface exposed as an ADK tool. The `list_available_agents` tool needs to:

1. Query `kb.agent_definitions` for the current project
2. Return a summary (name, description, tools, flow_type) — NOT the full system prompt
3. Filter out the calling agent itself (no self-spawning loops)

**Solution:** `list_available_agents` is a lightweight read from the agent registry. The summary format gives the parent's LLM enough information to make selection decisions without wasting tokens on full system prompts. The registry already needs to exist for `spawn_agents` to look up definitions — this is just exposing it as a tool.

---

## Impact on Design Docs

The existing design docs handle this scenario well with minor extensions:

| Document                                   | Status                   | Changes Needed                                                                                                                         |
| ------------------------------------------ | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `multi-agent-architecture-design.md`       | ✅ Mostly covers this    | Add `spawn_agents` + `list_available_agents` to coordination tools. Add MCP configuration section (per-project + per-agent filtering). |
| `multi-agent-orchestration-walkthrough.md` | ✅ Static DAG scenario   | No changes needed — this document covers static DAGs. The research scenario is a separate walkthrough (this document).                 |
| `task-coordinator-design.md`               | ✅ TaskDispatcher design | Add a note about dynamic DAG creation (Pattern 2) as a variant. The TaskDispatcher already handles newly-created tasks via polling.    |
| `todo.md`                                  | ⚠️ Needs update          | Add coordination tools, per-agent tool filtering, MCP configuration to implementation steps.                                           |
| `product-layer-design.md`                  | ⚠️ Needs update          | Add external MCP server connection model. Extend manifest schema with `mcp.servers` for project-level MCP config.                      |

---

## Implementation Order

For the research agent scenario specifically:

1. **AgentExecutor** (Step 1 in todo.md) — the foundation. Without this, nothing runs.
2. **Agent registry** — `kb.agent_definitions` table + CRUD service for querying agent definitions per project.
3. **`list_available_agents` tool** — read-only query against the agent registry. Returns agent summaries to the parent's LLM.
4. **`spawn_agents` tool** — Go function in `domain/agents/executor.go` that resolves agent definitions by name and spawns sub-agents as goroutines.
5. **Per-agent tool filtering** — `ResolveTools(toolNames)` in MCP service. Each sub-agent gets ONLY its own tools, not the parent's tools.
6. **Web browsing tools** — `web_search` and `web_fetch` as MCP tools. Can be implemented as internal tools or provided by an external MCP server connection.
7. **Product manifest** — Define the research-assistant, web-browser, and paper-summarizer agent definitions in a product manifest.
8. **Test with real execution** — Trigger the research agent with a topic, verify it queries the agent catalog, selects appropriate agents, executes in parallel, and writes to the graph.

Steps 1-5 are multi-agent coordination infrastructure. Steps 6-7 are product-specific. Step 8 validates the whole chain.

---

## Summary

The current architecture **can handle this scenario** with two key additions: the `list_available_agents` and `spawn_agents` coordination tools. Together, they enable a parent agent to dynamically discover all available agent types for the project, choose the right one for each sub-task, and execute them in parallel.

**Dynamic agent selection** is the critical design choice here: the parent agent doesn't hardcode which sub-agent to use. Instead, it queries the agent catalog (`kb.agent_definitions`) and lets its LLM decide based on agent descriptions and tool sets. This means:

- Adding a new agent definition to the project makes it immediately available for spawning
- A parent agent can use **different agent types** in the same parallel batch
- Agent selection logic lives in the LLM, not in Go code — it adapts to new agents without code changes

The pattern is consistent with what already exists in the codebase:

- The extraction pipeline (`domain/extraction/agents/pipeline.go`) already runs multiple ADK agents internally
- The ADK-Go framework supports `sequentialagent`, `loopagent`, and custom agents with `Run` functions
- Go's `sync.WaitGroup` provides the parallel execution primitive
- Context propagation handles cancellation

The main architectural insight: **dynamic sub-agent spawning doesn't need the TaskDispatcher.** It's a tool that an agent calls, not a coordination workflow. The TaskDispatcher is for static (or semi-static) multi-task workflows where tasks have cross-agent dependencies. Dynamic sub-agent spawning is an intra-agent pattern — the parent agent owns the entire lifecycle.

For cases where durability, individual observability, or per-subtask retry policies are needed, Pattern 2 (dynamic DAG creation by the parent agent, handled by TaskDispatcher) provides a heavier-weight alternative. Both patterns can coexist.
