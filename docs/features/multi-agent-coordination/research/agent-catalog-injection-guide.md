# Agent Catalog Injection — Implementation Guide

_Written: March 2026_
_Based on: OpenCode source analysis (`packages/opencode/src/tool/task.ts`) + emergent multi-agent architecture_

## What This Guide Covers

How to expose the catalog of available agents to a parent agent so the LLM can make informed spawning decisions. This is the mechanism that answers: "How does a parent agent know what sub-agents exist and when to use them?"

The technique is called **catalog injection into tool description**. It is the pattern OpenCode uses for its `Task` tool, and it is what emergent should implement for `spawn_agents` and `list_available_agents`.

---

## The Core Idea

**Agents learn what other agents exist by reading the description of the tool they use to spawn them.**

There is no separate discovery step, no system prompt section, no "list agents first, then spawn" dance. The LLM reads tool descriptions before deciding which tools to call. Injecting the agent catalog into the `spawn_agents` tool description means the LLM has the full catalog every time it considers whether to delegate.

```
LLM sees tool descriptions
  └── spawn_agents description contains:
        "Available agent types:
         - extraction-worker: Extracts entities and relationships from text...
         - validation-agent: Validates extracted data against schema constraints...
         - research-assistant: Searches the knowledge graph and synthesizes findings..."
  └── LLM decides: "I need to extract entities → use extraction-worker"
  └── LLM calls: spawn_agents(agent_name="extraction-worker", task="...")
```

The catalog is baked into the tool's own documentation. No extra tool call required.

---

## OpenCode's Exact Mechanism (Reference Implementation)

### Step 1: Agent definition includes a `description` field

```typescript
// agent.ts — each agent definition has a short description
{
  name: "explore",
  description: "Fast agent specialized for exploring codebases. Use this when you need to quickly find files by patterns, search code for keywords, or answer questions about the codebase.",
  mode: "subagent",   // controls visibility: "primary" agents are excluded
  permission: [...]
}
```

The `description` is the only thing the parent agent's LLM sees about a sub-agent. It must be:
- One sentence that says **what it does** (not what it is)
- Specific enough to distinguish it from similar agents
- Written from the perspective of "when should a parent use me?"

### Step 2: `TaskTool.init()` builds the description dynamically

```typescript
// task.ts — the key injection logic
const agents = await Agent.list()
  .then((x) => x.filter((a) => a.mode !== "primary"));  // ← filter gate

const caller = ctx?.agent;
const accessibleAgents = caller
  ? agents.filter((a) =>
      PermissionNext.evaluate("task", a.name, caller.permission).action !== "deny"
    )
  : agents;

// ← The injection: replace {agents} placeholder in the template
const description = task_default.replace(
  "{agents}",
  accessibleAgents
    .map((a) => `- ${a.name}: ${a.description ?? "This subagent should only be called manually by the user."}`)
    .join("\n")
);

return { description, parameters, execute };
```

### Step 3: The description template has a `{agents}` placeholder

```
// task.txt
Launch a new agent to handle complex, multistep tasks autonomously.

Available agent types and the tools they have access to:
{agents}

When using the Task tool, you must specify a subagent_type parameter to select which agent type to use.
...
```

The output the LLM sees in the tool schema:

```
Launch a new agent to handle complex, multistep tasks autonomously.

Available agent types and the tools they have access to:
- general: General-purpose agent for researching complex questions and executing multi-step tasks. Use this agent to execute multiple units of work in parallel.
- explore: Fast agent specialized for exploring codebases. Use this when you need to quickly find files by patterns, search code for keywords, or answer questions about the codebase.
- database: Fast database query agent. Use this agent for any database queries...
- api-client: API testing agent. Use this agent to discover and call API endpoints with automatic authentication.
- diagnostics: Diagnoses and verifies problems by browsing application logs, AI traces, and infrastructure health.

When using the Task tool, you must specify a subagent_type parameter...
```

### Why this works

1. **Zero latency**: The catalog is in the tool schema, which the LLM receives at conversation start. No extra round trip.
2. **Zero token overhead per turn**: The description is part of the tool definition, not injected into each message. Modern LLMs cache tool schemas.
3. **Always current**: The description is built at `init()` time — if agents change between sessions, the new session sees the current catalog.
4. **Naturally constrained**: The LLM can only reference agents that appear in the list. It cannot hallucinate agent names because `execute()` validates `subagent_type` against the registry.

---

## Emergent Implementation Plan

### What to build

Two tools are needed. They serve different purposes:

| Tool | Purpose | When to use |
|---|---|---|
| `spawn_agents` | Dispatches sub-agents and waits for results | Primary spawning tool — catalog injected into its description |
| `list_available_agents` | Returns a detailed agent catalog as tool output | Optional — for agents that need to reason about capability before spawning |

For most agents, `spawn_agents` alone is sufficient. The catalog in the description gives the LLM enough to make good decisions. `list_available_agents` is for orchestrators that need to programmatically inspect capabilities before assembling a workflow.

### `spawn_agents` — catalog injection in Go

The description is built once when the tool is constructed for a given agent execution context:

```go
// domain/agents/tools/spawn_agents.go

const spawnAgentsTemplate = `Spawn one or more sub-agents to handle tasks in parallel.

Available agent types for this project:
{agents}

Each spawned agent runs independently with its own tool set (defined by its agent definition).
Sub-agents cannot spawn further sub-agents unless their definition explicitly allows it.

Parameters:
- agent_name: must be one of the agent types listed above
- task: detailed description of what the sub-agent should accomplish
- timeout: optional maximum duration (default: 5 minutes)
- resume_run_id: optional, resume a previously paused agent run`

func NewSpawnAgentsTool(registry AgentRegistry, projectID string, callerDef *AgentDefinition) adk.Tool {
    // 1. Load agents for this project
    defs := registry.ListDefinitions(ctx, projectID)

    // 2. Apply caller's tool whitelist — if this agent shouldn't spawn others, this
    //    tool won't be in its whitelist at all (enforced upstream). But if a caller
    //    can only spawn certain agents, we filter here.
    accessible := filterAccessibleAgents(defs, callerDef)

    // 3. Build the catalog lines
    catalogLines := make([]string, 0, len(accessible))
    for _, def := range accessible {
        desc := def.Description
        if desc == "" {
            desc = "No description provided — use only if explicitly instructed."
        }
        catalogLines = append(catalogLines, fmt.Sprintf("- %s: %s", def.Name, desc))
    }

    // 4. Inject into template
    description := strings.ReplaceAll(
        spawnAgentsTemplate,
        "{agents}",
        strings.Join(catalogLines, "\n"),
    )

    return adk.NewTool(
        "spawn_agents",
        description,
        spawnAgentsParameters(),
        func(ctx context.Context, params SpawnParams) (SpawnResult, error) {
            // Validate agent_name is in the accessible list
            def := registry.GetDefinition(ctx, projectID, params.AgentName)
            if def == nil {
                return SpawnResult{}, fmt.Errorf(
                    "unknown agent type: %q — must be one of: %s",
                    params.AgentName,
                    strings.Join(agentNames(accessible), ", "),
                )
            }
            // ... execute
        },
    )
}
```

### Filtering logic — what agents appear in the catalog

Not all agents should appear in the catalog for every caller. Three filters apply:

**Filter 1: Visibility level**

```go
func filterAccessibleAgents(defs []AgentDefinition, callerDef *AgentDefinition) []AgentDefinition {
    var result []AgentDefinition
    for _, def := range defs {
        // Agents don't spawn themselves
        if callerDef != nil && def.Name == callerDef.Name {
            continue
        }
        // All visibility levels are spawnable — visibility controls external/UI access,
        // not internal coordination. An "internal" agent is still spawnable by other agents.
        result = append(result, def)
    }
    return result
}
```

**Filter 2: Recursive spawning prevention**

Sub-agents should not appear in their own sub-agent's catalog by default. This is enforced at the tool-set level: `spawn_agents` is not in `SubAgentDeniedTools` but is not included in sub-agent tool whitelists unless explicitly allowed:

```go
// domain/agents/toolpool.go

// SystemDeniedToolsForSubAgents — these tools are stripped from all sub-agent tool sets
// regardless of what the agent definition's whitelist says.
// Applied when depth > 0 (this agent was spawned by another agent).
var SystemDeniedToolsForSubAgents = []string{
    "spawn_agents",          // no recursive spawning by default
    "list_available_agents", // sub-agents work on assigned tasks, not catalog browsing
}

func (p *ToolPool) ResolveTools(agentDef AgentDefinition, depth int) []adk.Tool {
    tools := p.filterByWhitelist(agentDef.Tools)
    if depth > 0 {
        tools = p.removeDenied(tools, SystemDeniedToolsForSubAgents)
    }
    return tools
}
```

**Filter 3: Empty catalog fallback**

If a project has no agent definitions beyond the caller itself, the description should degrade gracefully:

```go
if len(catalogLines) == 0 {
    description = strings.ReplaceAll(
        spawnAgentsTemplate,
        "{agents}",
        "(no sub-agents are configured for this project)",
    )
}
```

### `list_available_agents` — for explicit catalog inspection

This tool returns catalog data as structured output, for orchestrators that need to reason about capabilities before assembling a workflow:

```go
// domain/agents/tools/list_agents.go

const listAgentsDescription = `List all available agent types for this project.

Use this tool when you need to inspect agent capabilities before deciding how to decompose
a complex task. For simple delegation decisions, the catalog in spawn_agents is sufficient.

Returns: array of agent summaries with name, description, and tool capabilities.`

type AgentSummary struct {
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Tools       []string `json:"tools"`       // whitelist, not full resolved set
    FlowType    string   `json:"flow_type"`   // "single" | "loop" | "parallel"
    Visibility  string   `json:"visibility"`  // "external" | "project" | "internal"
}
```

Note: this tool returns `tools` as the whitelist from the agent definition (e.g. `["search_hybrid", "entities_*"]`), not the fully resolved set. The LLM uses this to understand what the agent can *do*, not the exact tool names.

---

## Writing Good Agent Descriptions

The description is the only thing the parent LLM sees when deciding which agent to spawn. A poor description leads to wrong agent selection; a good description makes the right choice obvious.

### The description formula

```
[What it does] + [When to use it] + [Key capability that differentiates it]
```

### Examples — bad vs good

| Agent | Bad description | Good description |
|---|---|---|
| `extraction-worker` | "Extracts things from text" | "Extracts entities and relationships from raw text or documents. Use when you have unstructured content that needs to be parsed into typed graph objects." |
| `validation-agent` | "Validates data" | "Validates extracted entities and relationships against schema constraints and business rules. Use after extraction to catch malformed or inconsistent data before it enters the graph." |
| `research-assistant` | "Researches information" | "Searches the knowledge graph using hybrid search, traverses relationships, and synthesizes findings into structured summaries. Use for open-ended research tasks that require navigating existing knowledge." |

### What to avoid

- **Describing what it is, not when to use it**: "An agent that uses the extraction pipeline" → useless for decision-making
- **Vague capability language**: "handles documents", "processes data" → doesn't differentiate
- **Technical implementation details**: "uses gemini-2.5-flash with 8k context" → irrelevant to the parent
- **Missing differentiation**: if two agents both "process documents", the LLM can't choose between them

### The fallback description

If no description is provided for an agent definition, OpenCode uses: `"This subagent should only be called manually by the user."` — which effectively hides it from autonomous spawning. In emergent, a missing description should produce a similar warning:

```go
const missingDescriptionFallback = "No description provided. Only spawn if explicitly instructed by the user."
```

---

## Where in the Emergent Call Stack to Build the Tool

The tool must be built **per execution context**, not once at server startup. The catalog changes per project and per caller:

```
AgentExecutor.Execute(agentDef, projectID, prompt)
  └── builds tool set for THIS agent:
        toolPool.ResolveTools(agentDef, depth)
          └── includes spawn_agents if in agentDef.Tools whitelist
                └── NewSpawnAgentsTool(registry, projectID, agentDef)
                      └── injects current project's agent catalog into description
```

This matches the ADK-Go pipeline pattern in `domain/extraction/agents/pipeline.go` — tools are assembled at pipeline construction time, not globally.

The registry query is cheap (one DB read for the project's agent definitions, easily cached per-session). The description string is built once per agent execution, not per LLM turn.

---

## Concrete Example: What the LLM Sees

Given a research product with three agents: `research-assistant` (coordinator), `paper-summarizer` (internal), and `citation-extractor` (internal), the `research-assistant` would see this in its tool schema:

```
spawn_agents:
  Launch one or more sub-agents to handle tasks in parallel.

  Available agent types for this project:
  - paper-summarizer: Reads a document and produces a structured summary with key findings,
    methodology, and conclusions. Use when you need to distill a long document into
    actionable insights.
  - citation-extractor: Extracts bibliographic references from academic papers and resolves
    them to existing entities in the knowledge graph. Use when processing papers that cite
    other work you want to track.

  Each spawned agent runs independently...
```

The LLM then makes decisions like:
> "I have 12 papers to process. I'll spawn paper-summarizer for each one in parallel, then run citation-extractor on the ones that have reference sections."

---

## Validation: Preventing Hallucinated Agent Names

The `execute()` function must validate the `agent_name` parameter against the actual registry, not just the injected list:

```go
func (t *SpawnAgentsTool) execute(ctx context.Context, params SpawnParams) (SpawnResult, error) {
    def, err := t.registry.GetDefinition(ctx, t.projectID, params.AgentName)
    if err != nil || def == nil {
        accessible := t.registry.ListDefinitions(ctx, t.projectID)
        names := make([]string, len(accessible))
        for i, a := range accessible {
            names[i] = a.Name
        }
        return SpawnResult{}, fmt.Errorf(
            "agent %q does not exist in this project — available agents: %s",
            params.AgentName,
            strings.Join(names, ", "),
        )
    }
    // proceed with execution
}
```

This gives the LLM a useful error that includes the valid names, helping it self-correct on retry.

---

## Summary: The Three Design Decisions

| Decision | OpenCode | Emergent |
|---|---|---|
| **Where the catalog lives** | In the `task` tool's `description` string | Same: in `spawn_agents` description + optionally as `list_available_agents` tool output |
| **Filter gate** | `mode != "primary"` — primary agents excluded | Visibility + depth: `internal` agents excluded from external API, but all agents visible to other agents via `spawn_agents` |
| **Description content** | `name: description` per line | Same format — one line per agent with name and when-to-use description |

The essential insight from OpenCode: **the tool description IS the agent catalog**. Build it dynamically at tool construction time, keep descriptions decision-oriented ("use when X"), and validate agent names at execution time with a helpful error.
