# Multi-Agent Architecture Design for Emergent

_Design Date: February 14, 2026_
_Updated: February 15, 2026 — Aligned with emergent reality and product layer design_

## Executive Summary

This document presents the multi-agent architecture for emergent's coordination system. Agents are **defined by products** (configuration bundles installed on projects), stored in `kb.agent_definitions`, and executed via **ADK-Go pipelines** as goroutines inside the server. The knowledge graph provides persistent state management — tasks, sessions, agent interactions, and discussion outcomes are all stored as typed graph objects with relationships.

Tool access is managed at two levels: **per-project MCP server configuration** defines which tools are available (built-in graph tools + external MCP servers), and **per-agent tool filtering** controls which subset each agent can use (enabling read-only agents, write-only agents, etc.). Parent agents can dynamically discover and spawn any available agent via coordination tools.

The architecture is domain-agnostic: products define what agents exist (system prompt, model, tools, trigger type), and the coordination layer handles how they collaborate.

## Current Emergent Architecture

Emergent operates as:

- **Knowledge Graph Platform**: Typed objects, relationships, vector/FTS/hybrid search, BFS traversal
- **ADK-Go Agent Runtime**: Multi-agent pipelines using sequential/loop/parallel agents (Vertex AI + Gemini)
- **MCP Server**: 30+ built-in tools for graph operations, exposed to agents and external clients. No external MCP client connections yet.
- **Job Scheduler**: Cron-based scheduled tasks with PostgreSQL job queue
- **Product System**: Declarative configuration bundles (manifest.json) installed per-project
- **Technologies**: Go backend, React admin frontend, PostgreSQL with pgvector

### What Exists (Building Blocks)

| Component            | Location                               | Notes                                             |
| -------------------- | -------------------------------------- | ------------------------------------------------- |
| Agent entity         | `domain/agents/entity.go`              | Name, strategy, prompt, capabilities, config      |
| AgentRun tracking    | `domain/agents/entity.go`              | Status, duration, summary, errors                 |
| Knowledge Graph      | `domain/graph/`                        | 30+ service methods, typed objects, relationships |
| ADK-Go pipelines     | `domain/extraction/agents/pipeline.go` | Multi-agent orchestration pattern to follow       |
| Model factory        | `pkg/adk/model.go`                     | Creates Gemini models via Vertex AI               |
| MCP tools            | `domain/mcp/service.go`                | 30+ built-in graph tools, no external connections |
| PostgreSQL job queue | `internal/jobs/queue.go`               | Generic queue with FOR UPDATE SKIP LOCKED         |
| Template packs       | `domain/templatepacks/`                | Object/relationship schemas as data               |
| Scheduler            | `domain/scheduler/`                    | Cron-based scheduled tasks                        |

### What's Missing (The Gap)

1. **Agent execution is stubbed** — `handler.go:421-422` marks runs as `skipped`
2. **No agent executor** — nothing builds an ADK-Go pipeline from an Agent definition and runs it
3. **No task DAG** — no SpecTask entity type or `blocks` relationships in the graph
4. **No TaskDispatcher** — nothing walks the DAG, dispatches agents, and tracks completion
5. **No `kb.agent_definitions`** — product-defined agents table doesn't exist yet
6. **No per-agent tool filtering** — `GetToolDefinitions()` returns ALL tools, no filtering per agent
7. **No external MCP server connections** — emergent serves MCP but doesn't consume from other MCP servers
8. **No coordination tools** — no `list_available_agents` or `spawn_agents` for dynamic sub-agent spawning

## Proposed Multi-Agent Architecture

### 1. Agent Definition Model

Agents are defined by products via `manifest.json` and stored in `kb.agent_definitions`. This is **not** a fixed set of agent types — each product contributes its own agents to the project.

#### Product-Defined Agent Examples

A **research product** (`emergent.research`) might define:

```json
{
  "agents": [
    {
      "name": "research-assistant",
      "description": "Helps find, synthesize, and organize research information",
      "visibility": "external",
      "system_prompt": "You are a research assistant that helps find, synthesize, and organize information...",
      "model": { "provider": "google", "name": "gemini-2.5-flash" },
      "tools": [
        "search_hybrid",
        "graph_traverse",
        "create_entity",
        "web_search",
        "list_available_agents",
        "spawn_agents"
      ],
      "trigger": null,
      "flow_type": "single",
      "is_default": true,
      "acp": {
        "display_name": "Research Assistant",
        "description": "AI research assistant that searches your knowledge graph, synthesizes findings, and creates structured summaries.",
        "capabilities": ["chat", "research", "summarization"],
        "input_modes": ["text"],
        "output_modes": ["text"]
      }
    },
    {
      "name": "paper-summarizer",
      "description": "Extracts key findings from ingested documents",
      "visibility": "project",
      "system_prompt": "Extract key findings, methodology, and conclusions from documents...",
      "model": { "provider": "google", "name": "gemini-2.5-flash" },
      "tools": ["create_entity", "create_relationship"],
      "trigger": "on_document_ingested",
      "flow_type": "single"
    },
    {
      "name": "extraction-worker",
      "description": "Extracts entities from text chunks (internal sub-agent)",
      "visibility": "internal",
      "max_steps": 30,
      "system_prompt": "You extract structured entities from the provided text...",
      "model": { "provider": "google", "name": "gemini-2.5-flash" },
      "tools": ["create_entity", "create_relationship"],
      "trigger": null,
      "flow_type": "single"
    }
  ]
}
```

A **code product** (`emergent.code`) might define:

```json
{
  "agents": [
    {
      "name": "spec-writer",
      "description": "Creates technical specifications from high-level requirements",
      "visibility": "project",
      "system_prompt": "You create detailed technical specifications from high-level requirements...",
      "tools": ["create_entity", "create_relationship", "search_hybrid"],
      "trigger": null,
      "flow_type": "single"
    },
    {
      "name": "task-planner",
      "description": "Breaks specifications into ordered task DAGs with dependencies",
      "visibility": "project",
      "system_prompt": "You break specifications into an ordered task DAG with dependency relationships...",
      "tools": [
        "create_entity",
        "create_relationship",
        "graph_traverse",
        "list_available_agents",
        "spawn_agents"
      ],
      "trigger": null,
      "flow_type": "single"
    },
    {
      "name": "code-reviewer",
      "description": "Reviews code changes for correctness and style (internal sub-agent)",
      "visibility": "internal",
      "max_steps": 25,
      "system_prompt": "You review code changes for correctness, style, and alignment with specs...",
      "tools": ["search_hybrid", "graph_traverse"],
      "trigger": null,
      "flow_type": "single"
    }
  ]
}
```

The built-in **memory product** (`emergent.memory`) ships default agents:

```json
{
  "agents": [
    {
      "name": "entity-extractor",
      "description": "Extracts structured entities from unstructured text",
      "visibility": "project",
      "max_steps": 50,
      "system_prompt": "Extract structured entities from unstructured text...",
      "tools": ["create_entity", "create_relationship", "search_hybrid"],
      "trigger": "on_document_ingested",
      "flow_type": "sequential"
    }
  ]
}
```

#### Agent Definition Entity

```go
type AgentDefinition struct {
    ID             string            `json:"id"`
    ProductID      string            `json:"product_id"`              // Which product defined this
    ProjectID      string            `json:"project_id"`              // Which project it's installed on
    Name           string            `json:"name"`                    // "research-assistant"
    Description    string            `json:"description"`             // Short description for catalog
    SystemPrompt   string            `json:"system_prompt"`
    Model          ModelConfig       `json:"model"`                   // Provider, name, temperature
    Tools          []string          `json:"tools"`                   // MCP tool names (whitelist)
    Trigger        *string           `json:"trigger"`                 // nil=manual, "on_document_ingested", cron
    FlowType       string            `json:"flow_type"`               // "single", "sequential", "loop"
    IsDefault      bool              `json:"is_default"`              // Default agent for chat
    MaxSteps       *int              `json:"max_steps,omitempty"`     // Max LLM iterations; nil=unlimited (top-level), 50 (sub-agents)
    DefaultTimeout *time.Duration    `json:"default_timeout,omitempty"` // Default execution timeout; nil=5min system default
    Visibility     AgentVisibility   `json:"visibility"`              // "external", "project" (default), "internal"
    ACP            *ACPConfig        `json:"acp,omitempty"`           // ACP metadata; only for visibility="external"
    Config         map[string]any    `json:"config"`                  // Product-specific config
}

type ModelConfig struct {
    Provider    string  `json:"provider"`    // "google"
    Name        string  `json:"name"`        // "gemini-2.5-flash"
    Temperature float64 `json:"temperature"` // 0.0 - 1.0
}

// AgentVisibility controls where an agent is discoverable and invocable
type AgentVisibility string

const (
    VisibilityExternal AgentVisibility = "external" // ACP agent card + admin UI + other agents
    VisibilityProject  AgentVisibility = "project"  // Admin UI + other agents (DEFAULT)
    VisibilityInternal AgentVisibility = "internal"  // Other agents only (via list_available_agents/spawn_agents)
)

// ACPConfig holds metadata for agents exposed via Agent Card Protocol (ACP)
type ACPConfig struct {
    DisplayName  string   `json:"display_name"`  // Human-readable name for external consumers
    Description  string   `json:"description"`   // Longer description for agent card
    Capabilities []string `json:"capabilities"`  // e.g., ["chat", "research", "summarization"]
    InputModes   []string `json:"input_modes"`   // e.g., ["text", "file"]
    OutputModes  []string `json:"output_modes"`  // e.g., ["text", "structured"]
}
```

### 2. Agent Execution Architecture

#### Executor: ADK-Go Pipeline Builder

The executor takes an AgentDefinition and builds an ADK-Go pipeline to run it. This follows the pattern already established in `domain/extraction/agents/pipeline.go`.

```go
type AgentExecutor struct {
    graphService   *graph.Service
    modelFactory   *adk.ModelFactory
    toolPool       *ToolPool           // project-level tool pool (built-in + external MCP)
    agentRepo      *AgentRepository
    agentRegistry  *AgentRegistry      // for list_available_agents + spawn_agents
}

func (e *AgentExecutor) Execute(ctx context.Context, def AgentDefinition, input string) (*AgentRun, error) {
    // 1. Create AgentRun record (status: running)
    run := e.agentRepo.CreateRun(ctx, def.ID, input)

    // 2. Build ADK agent from definition
    model := e.modelFactory.Create(def.Model.Provider, def.Model.Name)

    // 3. Resolve tools: filter project's tool pool to only this agent's allowed tools
    tools := e.ResolveTools(ctx, def.ProjectID, def.Tools)

    // 4. Wire coordination tools if agent has them
    if contains(def.Tools, "list_available_agents") {
        tools = append(tools, e.makeListAgentsTool(ctx, def.ProjectID))
    }
    if contains(def.Tools, "spawn_agents") {
        tools = append(tools, e.makeSpawnAgentsTool(ctx, def.ProjectID))
    }

    agent := adk.NewAgent(def.Name, def.SystemPrompt, model, tools)

    // 3. Build pipeline based on flow type
    var pipeline adk.Agent
    switch def.FlowType {
    case "single":
        pipeline = agent
    case "sequential":
        pipeline = sequentialagent.New(def.Name+"-seq", agent)
    case "loop":
        pipeline = loopagent.New(def.Name+"-loop", agent, maxIterations)
    }

    // 4. Execute with session state
    session := e.createSession(ctx, def, input)
    result, err := pipeline.Run(ctx, session)

    // 5. Update AgentRun (status: success/error, duration, summary)
    e.agentRepo.CompleteRun(ctx, run.ID, result, err)

    return run, err
}
```

#### Dispatch Model: Ephemeral Goroutines

Each agent execution is a goroutine — clean state, simple lifecycle, follows the extraction pipeline pattern. No persistent worker processes.

```
TaskDispatcher
├── polls for available tasks (pending + unblocked + unassigned)
├── for each task:
│   ├── selects agent via AgentSelector
│   ├── go execute(agentDef, taskContext) ← goroutine
│   │   ├── builds ADK-Go pipeline
│   │   ├── runs LLM calls via Vertex AI
│   │   ├── writes results to graph
│   │   └── marks AgentRun complete
│   └── monitors via AgentRun status
└── on completion: marks task done → checks for newly-unblocked tasks
```

### 3. Coordination via Knowledge Graph

All coordination state lives in the knowledge graph as typed objects and relationships. No external message bus needed — the graph IS the shared state.

#### Multi-Agent Template Pack

```json
{
  "name": "emergent-coordination",
  "version": "1.0.0",
  "description": "Multi-agent coordination, task DAGs, sessions, and discussions",
  "object_type_schemas": {
    "SpecTask": {
      "type": "object",
      "properties": {
        "title": { "type": "string" },
        "description": { "type": "string" },
        "status": {
          "enum": ["pending", "in_progress", "completed", "failed", "skipped"]
        },
        "priority": { "enum": ["low", "medium", "high", "critical"] },
        "complexity": { "type": "integer", "minimum": 1, "maximum": 10 },
        "assigned_agent": { "type": "string" },
        "requires_collaboration": { "type": "boolean" },
        "retry_count": { "type": "integer" },
        "max_retries": { "type": "integer" },
        "failure_context": { "type": "string" },
        "metrics": {
          "type": "object",
          "properties": {
            "start_time": { "type": "string", "format": "date-time" },
            "completion_time": { "type": "string", "format": "date-time" },
            "duration_seconds": { "type": "number" },
            "llm_calls": { "type": "integer" },
            "tokens_used": { "type": "integer" }
          }
        }
      }
    },
    "Session": {
      "type": "object",
      "properties": {
        "status": { "enum": ["active", "completed", "failed", "cancelled"] },
        "agent_name": { "type": "string" },
        "task_context": { "type": "string" },
        "started_at": { "type": "string", "format": "date-time" },
        "completed_at": { "type": "string", "format": "date-time" },
        "message_count": { "type": "integer" }
      }
    },
    "SessionMessage": {
      "type": "object",
      "properties": {
        "role": { "enum": ["system", "user", "assistant", "tool"] },
        "content": { "type": "string" },
        "tool_calls": { "type": "array" },
        "timestamp": { "type": "string", "format": "date-time" }
      }
    },
    "Discussion": {
      "type": "object",
      "properties": {
        "topic": { "type": "string" },
        "status": { "enum": ["active", "resolved", "escalated", "abandoned"] },
        "discussion_type": {
          "enum": ["consensus", "debate", "brainstorm", "problem_solving"]
        },
        "context": { "type": "string" },
        "participating_agents": {
          "type": "array",
          "items": { "type": "string" }
        },
        "decision_criteria": { "type": "array", "items": { "type": "string" } },
        "consensus_level": { "type": "number", "minimum": 0, "maximum": 1 },
        "final_decision": { "type": "object" }
      }
    },
    "DiscussionEntry": {
      "type": "object",
      "properties": {
        "agent_name": { "type": "string" },
        "content": { "type": "string" },
        "entry_type": {
          "enum": ["argument", "proposal", "vote", "question", "clarification"]
        },
        "timestamp": { "type": "string", "format": "date-time" }
      }
    }
  },
  "relationship_type_schemas": {
    "blocks": {
      "description": "Task dependency — source blocks target from starting",
      "from": "SpecTask",
      "to": "SpecTask"
    },
    "assigned_to_agent": {
      "description": "Task assigned to an agent for execution",
      "from": "SpecTask",
      "to": "SpecTask",
      "properties": {
        "agent_name": { "type": "string" },
        "assigned_at": { "type": "string", "format": "date-time" }
      }
    },
    "has_session": {
      "description": "Task has an execution session",
      "from": "SpecTask",
      "to": "Session"
    },
    "has_message": {
      "description": "Session contains a message",
      "from": "Session",
      "to": "SessionMessage",
      "properties": {
        "sequence": { "type": "integer" }
      }
    },
    "spawned_discussion": {
      "description": "Task spawned a discussion for collaboration",
      "from": "SpecTask",
      "to": "Discussion"
    },
    "has_entry": {
      "description": "Discussion contains an entry",
      "from": "Discussion",
      "to": "DiscussionEntry",
      "properties": {
        "sequence": { "type": "integer" }
      }
    },
    "resulted_in_task": {
      "description": "Discussion resulted in new task creation",
      "from": "Discussion",
      "to": "SpecTask"
    }
  }
}
```

### 4. Discussion and Collaboration Patterns

When a task requires input from multiple agents, the coordinator spawns a Discussion in the graph. Each agent contributes entries, and the coordinator evaluates consensus.

#### Consensus Building

```go
type ConsensusBuilder struct {
    graphService *graph.Service
    executor     *AgentExecutor
    threshold    float64 // e.g., 0.7 = 70% agreement
}

func (cb *ConsensusBuilder) RunDiscussion(ctx context.Context, topic string, agents []AgentDefinition) (*Decision, error) {
    // 1. Create Discussion object in graph
    discussion := cb.graphService.CreateObject(ctx, "Discussion", map[string]any{
        "topic":  topic,
        "status": "active",
        "discussion_type": "consensus",
        "participating_agents": agentNames(agents),
    })

    // 2. Each agent contributes arguments (sequential or parallel)
    for _, agent := range agents {
        entry := cb.executor.Execute(ctx, agent, formatDiscussionPrompt(topic, discussion))
        cb.graphService.CreateObject(ctx, "DiscussionEntry", map[string]any{
            "agent_name": agent.Name,
            "content":    entry.Summary,
            "entry_type": "argument",
        })
        cb.graphService.CreateRelationship(ctx, discussion.ID, "has_entry", entry.ID)
    }

    // 3. Evaluate consensus via LLM
    entries := cb.graphService.GetRelated(ctx, discussion.ID, "has_entry")
    consensus := cb.evaluateConsensus(ctx, entries)

    // 4. Decision or escalation
    if consensus.Level >= cb.threshold {
        cb.graphService.UpdateObject(ctx, discussion.ID, map[string]any{
            "status":          "resolved",
            "consensus_level": consensus.Level,
            "final_decision":  consensus.Decision,
        })
        return consensus.Decision, nil
    }

    cb.graphService.UpdateObject(ctx, discussion.ID, map[string]any{
        "status": "escalated",
    })
    return nil, ErrNeedsHumanDecision
}
```

#### Collaborative Problem Solving

For complex tasks that benefit from multiple perspectives:

```go
type CollaborationSession struct {
    Phases []CollaborationPhase
}

type CollaborationPhase struct {
    Name       string           // "analysis", "brainstorm", "evaluate", "decide"
    Agents     []AgentDefinition // Which agents participate
    MaxRounds  int              // Iteration limit
    OutputType string           // Expected DiscussionEntry type
}

// Example: multi-agent code review
phases := []CollaborationPhase{
    {Name: "review", Agents: []AgentDefinition{codeReviewer, securityReviewer}, OutputType: "argument"},
    {Name: "synthesis", Agents: []AgentDefinition{specWriter}, OutputType: "proposal"},
    {Name: "decision", Agents: []AgentDefinition{taskPlanner}, OutputType: "vote"},
}
```

### 5. Trigger Systems

Agents can be triggered by events, schedules, or manual invocation. The trigger system uses emergent's existing scheduler and extends it with event-based triggers.

#### Event-Driven Triggers

```go
type EventTrigger struct {
    AgentDefID  string   `json:"agent_def_id"`  // Which agent to trigger
    EventType   string   `json:"event_type"`    // "on_document_ingested", "on_entity_created"
    Conditions  []Filter `json:"conditions"`    // Optional filters
}

// Example: trigger paper-summarizer when a document is ingested
// Defined in product manifest:
//   "trigger": "on_document_ingested"
//
// The extraction pipeline already emits events when documents are processed.
// The trigger system listens and dispatches the appropriate agent.
```

#### Schedule-Driven Triggers

Using the existing `domain/scheduler/` cron system:

```go
// Example: daily research digest
// Defined in product manifest:
//   "trigger": "0 8 * * MON-FRI"  (cron expression)
//
// The scheduler creates a job that invokes the agent executor
// with the agent definition and configured input context.
```

#### Manual Triggers

Via the existing Agent API (`POST /agents/:id/trigger`) or through the admin UI. The stubbed execution in `handler.go` is replaced with the real executor.

### 6. TaskDispatcher: DAG-Walking Coordinator

The TaskDispatcher is the core coordination component. It walks the task DAG, dispatches agents, and manages the execution lifecycle.

```go
type TaskDispatcher struct {
    graphService  *graph.Service
    executor      *AgentExecutor
    selector      AgentSelector
    maxConcurrent int
    mu            sync.Mutex
    activeRuns    map[string]*AgentRun
}

func (td *TaskDispatcher) Run(ctx context.Context, projectID string) error {
    for {
        // 1. Query available tasks: pending + unblocked + unassigned
        tasks := td.queryAvailableTasks(ctx, projectID)
        if len(tasks) == 0 {
            if td.allTasksComplete(ctx, projectID) {
                return nil // All done
            }
            time.Sleep(pollInterval)
            continue
        }

        // 2. Respect concurrency limit
        slots := td.maxConcurrent - len(td.activeRuns)
        if slots <= 0 {
            time.Sleep(pollInterval)
            continue
        }

        // 3. Dispatch available tasks up to slot limit
        for _, task := range tasks[:min(len(tasks), slots)] {
            agent := td.selector.SelectAgent(ctx, task)
            go td.executeTask(ctx, task, agent)
        }
    }
}

func (td *TaskDispatcher) queryAvailableTasks(ctx context.Context, projectID string) []GraphObject {
    // Find SpecTask objects where:
    // - status = "pending"
    // - no incoming "blocks" relationship from incomplete tasks
    // - not currently assigned
    return td.graphService.QueryObjects(ctx, QueryParams{
        ObjectType: "SpecTask",
        Filters: map[string]any{
            "status": "pending",
        },
        // Additional: check no blocking predecessor is incomplete
    })
}

func (td *TaskDispatcher) executeTask(ctx context.Context, task GraphObject, agent AgentDefinition) {
    // 1. Mark task in_progress
    td.graphService.UpdateObject(ctx, task.ID, map[string]any{
        "status":         "in_progress",
        "assigned_agent": agent.Name,
    })

    // 2. Build context from predecessors and task description
    predecessors := td.graphService.GetRelated(ctx, task.ID, "blocks", graph.Incoming)
    taskContext := td.buildTaskContext(task, predecessors)

    // 3. Execute agent
    run, err := td.executor.Execute(ctx, agent, taskContext)

    // 4. Handle result
    if err != nil {
        td.handleFailure(ctx, task, run, err)
    } else {
        td.graphService.UpdateObject(ctx, task.ID, map[string]any{
            "status": "completed",
            "metrics": map[string]any{
                "completion_time":  time.Now(),
                "duration_seconds": run.Duration.Seconds(),
            },
        })
    }

    // 5. Remove from active runs
    td.mu.Lock()
    delete(td.activeRuns, task.ID)
    td.mu.Unlock()

    // Newly-unblocked tasks will be picked up on next poll iteration
}
```

### 7. Agent Selection Strategies

```go
type AgentSelector interface {
    SelectAgent(ctx context.Context, task GraphObject) AgentDefinition
}

// CodeSelector: deterministic mapping from task properties to agents
type CodeSelector struct {
    agentDefs []AgentDefinition
    rules     []SelectionRule
}

type SelectionRule struct {
    TaskPattern  string // Regex or field match on task properties
    AgentName    string // Agent to assign
}

// LLMSelector: uses an LLM to match tasks to agents (for ambiguous cases)
type LLMSelector struct {
    executor  *AgentExecutor
    agentDefs []AgentDefinition
}

func (s *LLMSelector) SelectAgent(ctx context.Context, task GraphObject) AgentDefinition {
    prompt := fmt.Sprintf(
        "Given this task: %s\nAnd these available agents: %s\nWhich agent is best suited? Return the agent name.",
        task.Description, formatAgentSummaries(s.agentDefs),
    )
    // Use a fast model for selection decisions
    result := s.executor.QuickQuery(ctx, prompt)
    return s.findByName(result)
}

// HybridSelector: code rules first, LLM fallback for ambiguous matches
type HybridSelector struct {
    code *CodeSelector
    llm  *LLMSelector
}

func (s *HybridSelector) SelectAgent(ctx context.Context, task GraphObject) AgentDefinition {
    if agent, ok := s.code.TrySelect(ctx, task); ok {
        return agent
    }
    return s.llm.SelectAgent(ctx, task)
}
```

### 8. Fault Tolerance and Recovery

#### Retry with Context Injection

When a task fails, the dispatcher captures the failure context and injects it into the retry attempt so the agent can learn from previous failures.

```go
func (td *TaskDispatcher) handleFailure(ctx context.Context, task GraphObject, run *AgentRun, err error) {
    retryCount := task.Properties["retry_count"].(int)
    maxRetries := task.Properties["max_retries"].(int)

    if retryCount < maxRetries {
        // Inject failure context for retry
        td.graphService.UpdateObject(ctx, task.ID, map[string]any{
            "status":          "pending",  // Back to pending for retry
            "retry_count":     retryCount + 1,
            "failure_context": fmt.Sprintf("Previous attempt failed: %s\nAgent output: %s", err, run.Summary),
            "assigned_agent":  nil,  // Allow re-selection
        })
    } else {
        // Max retries exceeded — mark failed
        td.graphService.UpdateObject(ctx, task.ID, map[string]any{
            "status":          "failed",
            "failure_context": fmt.Sprintf("Failed after %d attempts. Last error: %s", maxRetries, err),
        })
        // Optionally: skip dependent tasks or escalate
    }
}
```

#### Health Monitoring

Agent health is tracked via AgentRun records — no separate supervision tree needed.

```go
func (td *TaskDispatcher) monitorHealth(ctx context.Context) {
    for {
        td.mu.Lock()
        for taskID, run := range td.activeRuns {
            if time.Since(run.StartedAt) > taskTimeout {
                // Task timed out — cancel and retry
                run.Cancel()
                task := td.graphService.GetObject(ctx, taskID)
                td.handleFailure(ctx, task, run, ErrTimeout)
                delete(td.activeRuns, taskID)
            }
        }
        td.mu.Unlock()
        time.Sleep(healthCheckInterval)
    }
}
```

### 9. System Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     Emergent Multi-Agent System                         │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │                    Product Layer                                    ││
│  │                                                                     ││
│  │  emergent.memory      emergent.research     emergent.code           ││
│  │  ├─ entity-extractor  ├─ research-assistant  ├─ spec-writer         ││
│  │  └─ (built-in)        ├─ paper-summarizer    ├─ task-planner        ││
│  │                       ├─ web-browser          ├─ code-reviewer       ││
│  │                       └─ (installable)       └─ (installable)       ││
│  │                                                                     ││
│  │  Each product's manifest.json defines:                              ││
│  │    → agents → kb.agent_definitions                                  ││
│  │    → MCP servers → project-level tool pool                          ││
│  │    → per-agent tool lists → tool filtering                          ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                │                                        │
│                                ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │                    Coordination Layer                               ││
│  │                                                                     ││
│  │  ┌─────────────────┐  ┌──────────────────┐  ┌────────────────────┐ ││
│  │  │  TaskDispatcher  │  │  AgentSelector   │  │ ConsensusBuilder   │ ││
│  │  │  - DAG walking   │  │  - Code rules    │  │ - Multi-agent      │ ││
│  │  │  - Slot mgmt     │  │  - LLM fallback  │  │   discussions      │ ││
│  │  │  - Retry logic   │  │  - Hybrid        │  │ - Voting           │ ││
│  │  └─────────────────┘  └──────────────────┘  └────────────────────┘ ││
│  │                                                                     ││
│  │  Coordination Tools:                                                ││
│  │    list_available_agents → query agent catalog for dynamic spawning ││
│  │    spawn_agents → parallel sub-agent execution with mixed types     ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                │                                        │
│                                ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │                    Execution Layer                                  ││
│  │                                                                     ││
│  │  ┌──────────────────────────────────────────────┐                   ││
│  │  │             AgentExecutor                     │                   ││
│  │  │  - Builds ADK-Go pipeline from definition    │                   ││
│  │  │  - Creates Gemini model via Vertex AI        │                   ││
│  │  │  - ResolveTools: filters per-agent tool set  │                   ││
│  │  │  - Tracks via AgentRun                       │                   ││
│  │  └──────────────────────────────────────────────┘                   ││
│  │                                                                     ││
│  │  goroutine 1: agent-A   goroutine 2: agent-B   goroutine 3: ...    ││
│  │  ┌──────────────────┐   ┌──────────────────┐   ┌────────────────┐  ││
│  │  │ ADK Pipeline     │   │ ADK Pipeline     │   │ ADK Pipeline   │  ││
│  │  │ └─ LLM calls     │   │ └─ LLM calls     │   │ └─ LLM calls   │  ││
│  │  │ └─ Tool calls    │   │ └─ Tool calls    │   │ └─ Tool calls  │  ││
│  │  │    (filtered!)   │   │    (filtered!)   │   │    (filtered!) │  ││
│  │  │ └─ Graph writes  │   │ └─ Graph writes  │   │ └─ Graph writes│  ││
│  │  └──────────────────┘   └──────────────────┘   └────────────────┘  ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                │                                        │
│                                ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │                    Tool Pool (MCP Layer)                            ││
│  │                                                                     ││
│  │  ┌────────────────────┐  ┌───────────────────┐  ┌───────────────┐  ││
│  │  │  Built-in Graph    │  │  External MCP:    │  │  External MCP:│  ││
│  │  │  Tools (30+)       │  │  web-tools        │  │  github       │  ││
│  │  │  create_entity     │  │  web_search       │  │  gh_search    │  ││
│  │  │  search_hybrid     │  │  web_fetch        │  │  gh_read_file │  ││
│  │  │  graph_traverse    │  │                   │  │               │  ││
│  │  │  ...               │  │                   │  │               │  ││
│  │  └────────────────────┘  └───────────────────┘  └───────────────┘  ││
│  │                                                                     ││
│  │  Per-project tool pool: union of all connected MCP servers          ││
│  │  Per-agent filtering: agent.tools whitelist from tool pool          ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                │                                        │
│                                ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │                    Knowledge Graph (State Layer)                    ││
│  │                                                                     ││
│  │  SpecTask ──blocks──► SpecTask ──has_session──► Session             ││
│  │     │                    │                         │                 ││
│  │     │                    └──spawned_discussion──►  │                 ││
│  │     │                         Discussion          has_message        ││
│  │     │                         ├─ has_entry ──►     │                 ││
│  │     │                         │  DiscussionEntry   │                 ││
│  │     │                         └─ resulted_in_task  SessionMessage   ││
│  │     │                              └──► SpecTask                    ││
│  │     │                                                               ││
│  │  All state queryable via graph search (vector, FTS, hybrid)        ││
│  │  All history versioned via graph object versioning                  ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                │                                        │
│                                ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │                    Infrastructure                                   ││
│  │                                                                     ││
│  │  PostgreSQL + pgvector    Vertex AI (Gemini)    Job Queue           ││
│  │  (graph storage)          (LLM calls)           (FOR UPDATE SKIP)  ││
│  └─────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────┘
```

### 10. MCP Configuration: Per-Project Servers + Per-Agent Tool Filtering

Tool access in the multi-agent system operates at **two levels**: project-level MCP server connections define which tools are available, and agent-level tool lists define which subset each agent can use.

#### Two-Level Tool Configuration

```
┌─────────────────────────────────────────────────────────────────────┐
│ PROJECT LEVEL: Which MCP servers are connected?                     │
│                                                                     │
│ Every project has access to:                                        │
│   1. Built-in graph tools (30+ tools from domain/mcp/service.go)   │
│   2. External MCP servers configured per-project (optional)         │
│                                                                     │
│ Project "acme-research" connections:                                │
│   ├── [built-in]  emergent graph tools: 30+ tools                  │
│   ├── [external]  web-tools-server:     web_search, web_fetch      │
│   └── [external]  github-server:        gh_search, gh_read_file    │
│                                                                     │
│ Combined tool pool: 34 tools                                        │
├─────────────────────────────────────────────────────────────────────┤
│ AGENT LEVEL: Which tools can this specific agent use?               │
│                                                                     │
│ research-assistant:                                                  │
│   tools: ["list_available_agents", "spawn_agents",                  │
│           "create_entity", "create_relationship", "search_hybrid"]  │
│   → 5 tools from the 34 available                                   │
│                                                                     │
│ web-browser:                                                        │
│   tools: ["web_search", "web_fetch"]                                │
│   → 2 tools (from external web-tools-server)                        │
│                                                                     │
│ paper-summarizer (read-only):                                       │
│   tools: ["search_hybrid", "get_entity", "search_fts"]             │
│   → 3 tools, NO write access (cannot create/update/delete)          │
│                                                                     │
│ memory-assistant:                                                    │
│   tools: ["*"]                                                      │
│   → All 34 tools (wildcard)                                         │
└─────────────────────────────────────────────────────────────────────┘
```

#### Project-Level MCP Server Configuration

Projects configure which external MCP servers are connected. This is defined in the product manifest and stored per-project.

```json
{
  "mcp": {
    "servers": [
      {
        "name": "web-tools",
        "description": "Web search and fetch capabilities",
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-web-tools"],
        "env": {
          "SEARCH_API_KEY": "${SEARCH_API_KEY}"
        }
      },
      {
        "name": "github",
        "description": "GitHub repository access",
        "transport": "sse",
        "url": "https://mcp.github.com/sse",
        "headers": {
          "Authorization": "Bearer ${GITHUB_TOKEN}"
        }
      }
    ],
    "tool_filter": ["*"]
  }
}
```

At project installation time, the product manager:

1. Parses `mcp.servers` from the manifest
2. Stores server configurations in the project's settings
3. On server startup (or lazy on first agent execution), connects to each external MCP server
4. Discovers available tools via MCP's `tools/list` method
5. Merges external tools with built-in graph tools into the project's **combined tool pool**

#### Agent-Level Tool Filtering

Each agent definition's `tools` field is a **whitelist** of tool names from the project's combined tool pool. The `AgentExecutor` enforces this:

```go
// ResolveTools filters the project's tool pool to only tools in the agent's whitelist
func (e *AgentExecutor) ResolveTools(ctx context.Context, projectID string, toolNames []string) ([]adk.Tool, error) {
    // 1. Get the project's combined tool pool (built-in + external MCP servers)
    allTools := e.toolPool.GetProjectTools(ctx, projectID)

    // 2. If wildcard, return all
    if len(toolNames) == 1 && toolNames[0] == "*" {
        return allTools, nil
    }

    // 3. Filter to only allowed tools
    allowed := make(map[string]bool, len(toolNames))
    for _, name := range toolNames {
        // Support glob patterns: "entities_*" matches entities_create, entities_update, etc.
        allowed[name] = true
    }

    var resolved []adk.Tool
    for _, tool := range allTools {
        if allowed[tool.Name()] || matchesGlob(tool.Name(), toolNames) {
            resolved = append(resolved, tool)
        }
    }

    return resolved, nil
}
```

#### Read-Only Agent Pattern

A common pattern is creating agents with read-only access to the knowledge graph:

```json
{
  "name": "auditor",
  "description": "Reviews knowledge graph for quality and completeness. Cannot modify data.",
  "tools": [
    "search_hybrid",
    "search_semantic",
    "search_fts",
    "get_entity",
    "graph_traverse",
    "list_objects"
  ]
}
```

This agent can search and traverse the graph but has no access to `create_entity`, `update_entity`, `delete_entity`, `create_relationship`, etc. The tool filtering is enforced at the `AgentExecutor` level — the ADK pipeline for this agent simply doesn't have those tools wired.

#### Write-Only / Specialized Agent Pattern

Conversely, an extraction agent might have write access but no search access:

```json
{
  "name": "entity-writer",
  "description": "Creates entities from structured data. Does not search or read existing data.",
  "tools": ["create_entity", "create_relationship"]
}
```

#### Tool Pool Architecture

```go
// ToolPool manages the combined set of tools available per project
type ToolPool struct {
    builtinTools []adk.Tool                    // graph tools from domain/mcp/service.go
    externalMCP  map[string]*MCPClientConn     // external MCP server connections
    projectTools map[string][]adk.Tool         // cached per-project combined pools
    mu           sync.RWMutex
}

// GetProjectTools returns all tools available for a project
func (tp *ToolPool) GetProjectTools(ctx context.Context, projectID string) []adk.Tool {
    tp.mu.RLock()
    if cached, ok := tp.projectTools[projectID]; ok {
        tp.mu.RUnlock()
        return cached
    }
    tp.mu.RUnlock()

    // Build tool pool: built-in + external MCP servers for this project
    tools := make([]adk.Tool, len(tp.builtinTools))
    copy(tools, tp.builtinTools)

    // Get project's MCP server configurations
    servers := tp.getProjectMCPServers(ctx, projectID)
    for _, server := range servers {
        conn := tp.getOrCreateConnection(server)
        externalTools, _ := conn.ListTools(ctx)
        tools = append(tools, wrapMCPTools(externalTools, server.Name)...)
    }

    tp.mu.Lock()
    tp.projectTools[projectID] = tools
    tp.mu.Unlock()

    return tools
}
```

#### Security Considerations

1. **Tool filtering is enforced in Go, not by the LLM.** An agent cannot access tools not in its `tools` list, even if it knows the tool name. The ADK pipeline is built with only the resolved tools.

2. **External MCP server credentials** are stored in project settings (encrypted), not in agent definitions. An agent that has `web_search` in its tool list gets access to call it, but the API key for the search service is managed at the project level.

3. **Agent spawning respects tool boundaries.** When a parent agent spawns a sub-agent via `spawn_agents`, the sub-agent gets its OWN tools (from its own definition), not the parent's tools. A parent with `create_entity` access cannot grant that to a sub-agent that doesn't have it.

4. **Glob patterns** (e.g., `"entities_*"`) are resolved at pipeline build time, not at runtime. If a new tool is added that matches the glob, the agent gets it on next execution — but cannot discover it mid-execution.

### 11. Sub-Agent Safety Mechanisms

Sub-agents running server-side without human supervision need guardrails. These mechanisms prevent runaway agents, infinite loops, and excessive resource consumption.

#### Step Limit Enforcement

Each agent definition has an optional `max_steps` field. When the step limit is reached, the agent is given a chance to summarize its work before stopping.

```go
func (e *AgentExecutor) executeWithStepLimit(ctx context.Context, run *AgentRun, agent adk.Agent) error {
    maxSteps := run.MaxSteps // from agent definition; nil = unlimited
    if maxSteps == nil {
        maxSteps = &defaultMaxSteps // 50 for sub-agents, unlimited for top-level
    }

    for step := 0; ; step++ {
        run.StepCount++ // cumulative across resumes

        if step >= *maxSteps {
            // Soft stop: inject "summarize and stop" system message
            injectMaxStepsMessage(ctx, agent)
            // LLM produces final summary, then we stop
            break
        }

        // Normal agent iteration: LLM call → tool execution
        result, err := agent.Step(ctx)
        if err != nil || result.Done {
            break
        }
    }
    return nil
}
```

**Soft enforcement** (inspired by OpenCode): When `step >= max_steps`, inject a system message:

```
CRITICAL - MAXIMUM STEPS REACHED

Tools are disabled. Respond with text only.

Your response MUST include:
- Summary of what has been accomplished so far
- List of any remaining tasks that were not completed
- Recommendations for what should be done next
```

If the LLM ignores the soft stop and makes a tool call, hard-stop by refusing to execute the tool and returning the summary.

**Recommended defaults**:

- Sub-agents spawned via `spawn_agents`: default `max_steps = 50`
- Top-level agents: default `max_steps = nil` (unlimited, user is watching or trigger-based)

#### Timeout Enforcement

Timeout operates at two levels:

**Level 1: Per-spawn timeout** (in `spawn_agents` parameters):

```go
type SpawnRequest struct {
    AgentType    string         `json:"agent_type"`
    Task         string         `json:"task"`
    Timeout      *time.Duration `json:"timeout,omitempty"`       // default: 5 minutes
    ResumeRunID  *string        `json:"resume_run_id,omitempty"` // resume a paused run
}
```

**Level 2: Per-agent-definition default timeout**:

The `default_timeout` field on `AgentDefinition` provides a baseline. The `spawn_agents` timeout parameter overrides it when specified.

**Enforcement**: Use Go `context.WithDeadline`. When the timeout fires:

1. Inject a "time's up, summarize" message (soft stop)
2. Wait 30 seconds for the LLM to respond
3. If still running, cancel the context (hard stop)
4. Return partial results to the parent agent with status `"paused"`

#### Recursive Spawning Prevention

By default, sub-agents **cannot** call `spawn_agents` or `list_available_agents`. These tools are removed from the sub-agent's tool set regardless of their agent definition:

```go
// System-level tool restrictions for sub-agents (depth > 0)
var SubAgentDeniedTools = []string{
    "spawn_agents",
    "list_available_agents",
}

func (e *AgentExecutor) ResolveTools(ctx context.Context, def AgentDefinition, depth int) []adk.Tool {
    tools := e.toolPool.FilterByWhitelist(ctx, def.ProjectID, def.Tools)

    // Enforce system-level restrictions for sub-agents
    if depth > 0 {
        tools = removeDenied(tools, SubAgentDeniedTools)
    }

    return tools
}
```

Agent definitions can **opt in** to delegation by explicitly including `spawn_agents` in their `tools` list AND being granted a depth exemption. Even when opted in, a hard `max_depth` limit (default 2) prevents infinite recursion:

```go
type SpawnContext struct {
    Depth    int // 0 = top-level, 1 = sub-agent, 2 = sub-sub-agent
    MaxDepth int // default: 2
}
```

#### Doom Loop Detection

Detects when an agent is stuck calling the same tool with identical arguments repeatedly:

```go
type DoomLoopDetector struct {
    lastCall      ToolCall
    consecutiveN  int
    threshold     int // default: 3
}

func (d *DoomLoopDetector) Check(call ToolCall) DoomLoopAction {
    if call.Name == d.lastCall.Name && call.ArgsHash == d.lastCall.ArgsHash {
        d.consecutiveN++
    } else {
        d.consecutiveN = 1
    }
    d.lastCall = call

    if d.consecutiveN >= d.threshold {
        return DoomLoopBreak
    }
    return DoomLoopContinue
}
```

When a doom loop is detected (3 identical consecutive calls), inject an error message instead of executing the tool:

```
LOOP DETECTED: You have called {tool_name} with identical arguments {N} times.
This tool call will not be executed. Please try a different approach or summarize
your progress so far.
```

This gives the LLM a chance to course-correct. If it loops again after the warning, hard-stop the agent.

### 12. State Persistence & Sub-Agent Resumption

#### Full State Persistence

Every agent run persists its complete message history and tool call records. This is not optional — it's a core architectural requirement for observability, debugging, auditability, and resumption.

```go
// AgentRun — extended from existing entity with new fields
type AgentRun struct {
    ID            string         `bun:"id,pk"`
    AgentID       string         `bun:"agent_id"`
    ParentRunID   *string        `bun:"parent_run_id"`      // nil for top-level runs
    ProjectID     string         `bun:"project_id"`
    Status        RunStatus      `bun:"status"`             // running, completed, failed, paused, cancelled
    StepCount     int            `bun:"step_count"`         // cumulative across resumes
    MaxSteps      *int           `bun:"max_steps"`          // from agent definition
    Summary       string         `bun:"summary"`            // final text output
    ErrorMessage  *string        `bun:"error_message"`
    CreatedAt     time.Time      `bun:"created_at"`
    CompletedAt   *time.Time     `bun:"completed_at"`
    Duration      time.Duration  `bun:"duration"`
    ResumedFrom   *string        `bun:"resumed_from"`       // previous run ID if this is a resume
}

// AgentRunMessage — full LLM conversation history per run
type AgentRunMessage struct {
    ID         string          `bun:"id,pk"`
    RunID      string          `bun:"run_id"`
    Role       string          `bun:"role"`               // system, user, assistant, tool_result
    Content    json.RawMessage `bun:"content,type:jsonb"` // full message content
    StepNumber int             `bun:"step_number"`        // which iteration this belongs to
    CreatedAt  time.Time       `bun:"created_at"`
}

// AgentRunToolCall — every tool invocation with input/output
type AgentRunToolCall struct {
    ID         string          `bun:"id,pk"`
    RunID      string          `bun:"run_id"`
    MessageID  string          `bun:"message_id"`         // which assistant message triggered this
    ToolName   string          `bun:"tool_name"`
    Input      json.RawMessage `bun:"input,type:jsonb"`
    Output     json.RawMessage `bun:"output,type:jsonb"`
    Status     string          `bun:"status"`             // running, completed, error
    Duration   time.Duration   `bun:"duration"`
    StepNumber int             `bun:"step_number"`
    CreatedAt  time.Time       `bun:"created_at"`
}
```

**Storage**: Messages and tool calls are stored in `kb.agent_run_messages` and `kb.agent_run_tool_calls` tables respectively. The typical run produces ~50-500KB of JSONB data — negligible storage cost for the debugging and resumption value provided.

#### Sub-Agent Resumption

When a sub-agent hits `max_steps` or times out, it returns a summary and its `run_id`. The parent can resume the sub-agent with full context preserved:

```
Step 1: Sub-agent reaches limit
  ├── Inject "summarize and stop" message
  ├── LLM produces summary of work done + remaining tasks
  ├── AgentRun.Status = "paused"
  ├── Return to parent: { summary, run_id, status: "paused" }
  └── All messages + tool calls already persisted

Step 2: Parent decides to resume
  ├── Parent calls spawn_agents with resume_run_id = <run_id>
  ├── AgentExecutor loads AgentRun + all AgentRunMessages
  ├── Reconstructs LLM conversation from persisted messages
  ├── Appends new user message: "Continue your work..."
  ├── AgentRun.StepCount carries forward (cumulative)
  ├── New step budget = max_steps (fresh budget per resume)
  └── Sub-agent continues with full context of prior work
```

The `spawn_agents` tool returns results with resumption support:

```go
type SpawnResult struct {
    RunID    string `json:"run_id"`      // for future resumption
    Status   string `json:"status"`      // completed, paused, failed, cancelled
    Summary  string `json:"summary"`     // final text output
    Steps    int    `json:"steps"`       // total steps executed (cumulative)
}
```

#### Cumulative Step Counter

Unlike OpenCode (which resets `step = 0` on each resume), our step counter is **cumulative across resumes**:

- A sub-agent that ran 45 steps, got paused, then resumed, starts at step 46
- The `max_steps` on resume is a fresh budget, but `StepCount` reflects total work
- Enables cost tracking and runaway detection across resumes

#### Global Safety: Max Total Steps

To prevent infinite resume loops:

```go
const MaxTotalStepsPerRun = 500 // across all resumes combined

func (e *AgentExecutor) canResume(run *AgentRun) error {
    if run.StepCount >= MaxTotalStepsPerRun {
        return fmt.Errorf("agent run %s has exceeded maximum total steps (%d)", run.ID, MaxTotalStepsPerRun)
    }
    return nil
}
```

### 13. Agent Visibility & Access Control

Agent visibility controls where an agent is discoverable and who can invoke it. Three levels:

| Level      | Who Can See It                                  | Who Can Invoke It                      | Use Case                                           |
| ---------- | ----------------------------------------------- | -------------------------------------- | -------------------------------------------------- |
| `external` | External clients (ACP), admin UI, other agents  | External API, admin UI, `spawn_agents` | Primary interactive agents exposed to integrations |
| `project`  | Admin UI, other agents within the project       | Admin UI trigger, `spawn_agents`       | Triggered/scheduled agents, utility agents         |
| `internal` | Only other agents (via `list_available_agents`) | Only `spawn_agents`                    | Pure sub-agents, implementation details            |

**Default is `project`** — agents must be explicitly opted-in to external exposure.

#### Visibility Matrix

| Surface                          | `external` | `project` | `internal` |
| -------------------------------- | ---------- | --------- | ---------- |
| `list_available_agents` (tool)   | Yes        | Yes       | Yes        |
| `spawn_agents` (tool)            | Yes        | Yes       | Yes        |
| Admin UI — Agent List            | Yes        | Yes       | No\*       |
| Admin UI — Manual Trigger        | Yes        | Yes       | No         |
| Admin UI — Run History           | Yes        | Yes       | Yes\*\*    |
| ACP Agent Card (future)          | Yes        | No        | No         |
| External Invocation API (future) | Yes        | No        | No         |
| Scheduler/Event Triggers         | Yes        | Yes       | No\*\*\*   |

\* Hidden by default, visible with `include_internal=true` query param
\*\* Internal agent runs appear as child runs in the run tree
\*\*\* Internal agents are spawned by other agents, not by system triggers

#### Key Design Decision

`list_available_agents` shows ALL agents regardless of visibility (the LLM needs the full catalog to make good delegation decisions). `spawn_agents` has NO visibility restriction (internal delegation is unrestricted). Visibility only restricts **external access surfaces**: ACP agent card, external invocation endpoint, and admin UI listing.

#### ACP Configuration

Only agents with `visibility: "external"` should include an `acp` configuration block in their manifest. This metadata populates the ACP agent card for external discovery:

```json
{
  "name": "research-assistant",
  "visibility": "external",
  "acp": {
    "display_name": "Research Assistant",
    "description": "AI research assistant that searches your knowledge graph...",
    "capabilities": ["chat", "research", "summarization"],
    "input_modes": ["text"],
    "output_modes": ["text"]
  }
}
```

If `visibility` is `"external"` but `acp` is nil, the agent is externally invocable but won't appear in the agent card (invoke-only, not discoverable).

## Implementation Phases

#### Phase 1: Agent Execution (Week 1-2)

- [ ] Build `AgentExecutor` in `domain/agents/` using ADK-Go pipeline pattern
- [ ] Wire MCP graph tools as ADK tool functions
- [ ] Replace stubbed execution in `handler.go` TriggerAgent
- [ ] Track execution via AgentRun with proper status/duration/summary
- [ ] Implement step limit enforcement (soft stop via prompt injection, hard stop fallback)
- [ ] Implement doom loop detection (`DoomLoopDetector`, threshold=3)

#### Phase 2: Product-Defined Agents + Tool Filtering (Week 3-4)

- [ ] Create `kb.agent_definitions` table with `max_steps`, `default_timeout`, `visibility`, `acp_config` columns
- [ ] Build agent registry that stores definitions from product manifests
- [ ] Wire agent definitions into chat module for interactive agents
- [ ] **Build `ResolveTools()` — per-agent tool filtering from project's tool pool**
- [ ] **Enforce sub-agent denied tools list (recursive spawning prevention)**
- [ ] **Build `list_available_agents` coordination tool**
- [ ] **Build `spawn_agents` coordination tool with `timeout` and `resume_run_id` support**
- [ ] Support trigger-based agents (on_document_ingested, schedule)

#### Phase 3: State Persistence + Agent Run History (Week 5-6)

- [ ] Create `kb.agent_run_messages` table (full LLM conversation history)
- [ ] Create `kb.agent_run_tool_calls` table (tool invocation records)
- [ ] Extend `kb.agent_runs` with `parent_run_id`, `step_count`, `max_steps`, `resumed_from` columns
- [ ] Implement sub-agent resumption flow (`resume_run_id` in `spawn_agents`)
- [ ] Implement cumulative step counter across resumes
- [ ] Implement `MaxTotalStepsPerRun` global safety cap (500)
- [ ] Build Agent Run History API (6 endpoints with progressive disclosure)
- [ ] Upgrade existing `GetAgentRuns` to cursor-based pagination

#### Phase 4: External MCP Connections + Task DAG (Week 7-8)

- [ ] Create `kb.project_mcp_servers` table
- [ ] Build MCP client for stdio + SSE transports with lazy connection lifecycle
- [ ] Integrate external tools into ToolPool (tool discovery via `tools/list`)
- [ ] Create SpecTask template pack with `blocks` relationship
- [ ] Build task generation tool (from description → task DAG)
- [ ] Build available-task query (pending + unblocked + unassigned)

#### Phase 5: TaskDispatcher (Week 9-10)

- [ ] Build `TaskDispatcher` in `domain/coordination/` (Go polling loop)
- [ ] Implement CodeSelector + LLMSelector + HybridSelector (AgentSelector strategies)
- [ ] Integrate timeout enforcement from agent definitions
- [ ] Handle paused runs (step limit / timeout) — store run_id for resumption
- [ ] Session tracking in graph (Session + SessionMessage)
- [ ] Retry logic with failure context injection

#### Phase 6: Collaborative Intelligence (Week 11-14)

- [ ] Discussion entity types and consensus building
- [ ] Multi-agent collaboration for complex tasks
- [ ] Output evaluation before marking tasks complete
- [ ] Cross-task context threading
- [ ] Admin UI dashboard for coordination visibility

### 14. Success Metrics

#### System Performance

- **Agent Uptime**: >99% availability for triggered agents
- **Execution Time**: <30s for simple single-agent tasks
- **DAG Throughput**: Process 20+ task DAG in <10 minutes with parallel execution
- **Resource Usage**: <500MB memory per concurrent agent goroutine

#### Coordination Quality

- **Task Completion Rate**: >95% tasks complete without human intervention
- **Retry Success Rate**: >50% of retried tasks succeed on second attempt
- **Agent Selection Accuracy**: >90% correct agent-task matches (measured by task success)
- **Discussion Resolution**: >70% of discussions reach consensus without escalation

#### Product Integration

- **Agent Definition Coverage**: Built-in agents for common operations
- **Product Adoption**: Products can define domain-specific agents via manifest
- **Template Pack Compatibility**: Coordination entities work with existing graph features

### 15. Risk Mitigation

| Risk                    | Mitigation                                                                                     |
| ----------------------- | ---------------------------------------------------------------------------------------------- |
| Agent goroutine leak    | Context cancellation + timeout monitoring + activeRuns tracking                                |
| Concurrent graph writes | PostgreSQL serializable transactions for task state transitions                                |
| LLM cost explosion      | Code-first selector (LLM only for ambiguous), fast models for decisions                        |
| Task DAG cycles         | Validate DAG on creation (topological sort check), reject cycles                               |
| Stale agent definitions | Product version tracking, re-sync on product update                                            |
| Runaway agents          | `max_steps` (soft+hard stop), `default_timeout`, `DoomLoopDetector`, `MaxTotalStepsPerRun=500` |
| State inconsistency     | Graph object versioning, idempotent task transitions                                           |

## Conclusion

This architecture transforms emergent from a knowledge graph platform with stubbed agent execution into a multi-agent coordination system where products define domain-specific agents, the knowledge graph provides persistent queryable state, and ADK-Go pipelines handle execution. The design builds entirely on existing infrastructure — no new external dependencies (no NATS, no ACP, no external process spawning).

The phased implementation starts with the simplest high-value step (making agent execution work) and incrementally adds coordination capabilities. Each phase delivers standalone value while building toward the full multi-agent vision.
