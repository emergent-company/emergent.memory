# Dynamic Agent Catalog: Blueprint vs. System — Gap Analysis & Proposal

_Written: March 2026_
_Context: workspace-memory-blueprint agent configuration + emergent server architecture_

## The Problem in One Sentence

The orchestrator's system prompt says `"Call list_agents to get the UUIDs of runtime agents"` — but `list_agents` returns runtime state (last_run_at, cron_schedule, enabled), not capabilities. The agent has to blindly match names to find the right UUID and has no description, task type, or capability metadata to reason about. It's a UUID lookup, not a catalog.

---

## Current State: Two Separate Worlds

The blueprint currently defines agents in two disconnected places that serve different purposes but do not talk to each other:

### World 1: YAML files (`agents/*.yaml`)

Each agent YAML is the **operational definition** — system prompt, model, tools, max steps, timeout. This is consumed by the blueprint applier to create runtime `Agent` records in the DB.

```yaml
# agents/orchestrator.yaml
name: orchestrator
description: >
  The primary orchestrator. Receives a WorkPackage, decomposes it into a task tree...
model:
  name: gemini-2.5-flash
tools:
  - list_agents
  - trigger_agent
```

**Problem**: once applied, the description lives only in the YAML file. It is not surfaced to agents at runtime.

### World 2: Seed data (`seed/objects/AgentDefinitionRecord.jsonl`)

Each `AgentDefinitionRecord` is a **knowledge graph object** — it records the *existence* of an agent as data inside the graph for other agents to query. This is what the orchestrator and pool managers read at runtime to understand the pool composition.

```json
{
  "type": "AgentDefinitionRecord",
  "key": "agent-def-leaf-researcher",
  "properties": {
    "name": "leaf-researcher",
    "agentType": "leaf",
    "skills": "research,synthesis,graph-writing",
    "tier": "cheap",
    "taskTypes": "research",
    "operationalDefinitionId": ""  ← EMPTY: no link to the actual agent
  }
}
```

**Problem 1**: `operationalDefinitionId` is an empty string in all seed records. There is no link between the graph-side data object and the actual operational agent record.

**Problem 2**: The `description` from the YAML is not copied into `AgentDefinitionRecord.properties`. Agents querying the graph get `skills` (a comma-separated tag string) but not the human-readable description.

**Problem 3**: `AgentDefinitionRecord` is a **static snapshot** created at blueprint-apply time. It doesn't update when the agent YAML is edited and re-applied unless the seeder explicitly handles deduplication/upserts.

### World 3: What `list_agents` actually returns

The `list_agents` MCP tool returns runtime `Agent` records from `kb.agents`:

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "leaf-researcher",
    "strategyType": "...",
    "enabled": true,
    "lastRunAt": "2026-03-08T12:00:00Z",
    "lastRunStatus": "success"
  }
]
```

This is what the orchestrator calls to get the UUID needed for `trigger_agent`. **It has no description, no task type, no tier, no skills.** The orchestrator must match by name ("leaf-researcher") to get the UUID, then separately query `AgentDefinitionRecord` objects in the graph to get capability data. Two tool calls to do what one should do.

---

## The Fundamental Gap: No Live Connection

```
Blueprint YAML ──(apply)──► kb.agents (runtime)
                                  ↑
                             list_agents ← orchestrator uses this for UUIDs
                             
                ──(seed)───► kb.graph (AgentDefinitionRecord objects)
                                  ↑
                             query_entities ← orchestrator uses this for capabilities
                             
The two are never linked. operationalDefinitionId = "" always.
```

The result is that the orchestrator has to do this dance:

1. Call `list_agents` → get UUIDs (no description)
2. Call `query_entities(type_name="AgentDefinitionRecord")` → get capabilities (no UUID)
3. Match by `name` across the two results
4. Mentally join them to make a decision

This is 2 tool calls and a manual join that the LLM has to perform correctly every time.

---

## What "Dynamic" Means in This Context

The user wants agent catalog information to come **from the system** rather than being hardcoded in the orchestrator's system prompt. There are two levels of dynamism to consider:

**Level 1: Dynamic at session start** — the catalog is built fresh when an agent session starts, reflecting the current set of live agents in the project. This is what OpenCode does: `Task` tool description is built at `init()` from the current agent registry. No hardcoded agent names in any system prompt.

**Level 2: Dynamic at runtime** — agents can query the catalog mid-run and receive up-to-date information (e.g., after the janitor activates a new agent variant, pool managers see it on their next `list_available_agents` call without being restarted).

Both levels require the same fix: a single tool that returns name + description + capabilities + UUID in one call, from a source that is kept in sync with the operational definitions.

---

## Root Cause: `AgentDefinitionRecord` is Redundant Data

The design has `AgentDefinitionRecord` as a graph object that duplicates information from the operational agent definition. This creates a sync problem — two sources of truth that drift apart.

OpenCode avoids this entirely: the agent catalog is built at runtime from the **live agent registry**, not from a static snapshot stored somewhere. There is no separate "AgentDefinitionRecord" concept. The catalog IS the live agent definitions.

The `AgentDefinitionRecord` type served a purpose in the design: it gave agents in the graph a typed object to reason about (pool membership, KPI scoping, experiment tracking). But it should be a **pointer to** the operational definition, not a copy of it.

---

## Proposed Solution

### Option A: Enrich `list_agents` — minimal change, maximum impact

The single highest-leverage fix is to make `list_agents` return the fields that agents actually need for decision-making.

**Current response** (from `kb.agents`):
```json
{ "id": "uuid", "name": "leaf-researcher", "enabled": true, "lastRunAt": "..." }
```

**Proposed response** (join `kb.agents` with `kb.agent_definitions`):
```json
{
  "id": "uuid",
  "name": "leaf-researcher",
  "description": "Leaf agent that conducts research tasks and synthesises findings into structured TaskResult objects...",
  "agentType": "leaf",
  "taskTypes": ["research"],
  "tier": "cheap",
  "model": "gemini-2.5-flash",
  "enabled": true,
  "lastRunAt": "2026-03-08T12:00:00Z"
}
```

This joins `kb.agents` (runtime state: UUID, enabled, lastRunAt) with `kb.agent_definitions` (capability metadata: description, agentType, tier, taskTypes, model).

**What changes in the server**: `ExecuteListAgents` in `domain/agents/mcp_tools.go` already calls `repo.FindAll()`. Extend it to also load the linked `AgentDefinition` by `name` match and include the capability fields in `AgentDTO`. The `AgentDTO` already has a `Description` field from `Agent.Description` — the question is whether `Agent.Description` is populated from `AgentDefinition.Description` at blueprint-apply time.

**What changes in the blueprint**: remove the "Step 6: Call list_agents then query AgentDefinitionRecord and join by name" pattern from orchestrator and pool manager system prompts. Replace with a single call to `list_agents`.

**Verdict**: This is the right immediate fix. It eliminates the two-call join. The tradeoff is that `AgentDefinitionRecord` objects in the graph become partially redundant for capability lookup — but they still serve the pool/experiment/KPI tracking purpose.

---

### Option B: Link `AgentDefinitionRecord` to the live agent — populate `operationalDefinitionId`

The `operationalDefinitionId` field in `AgentDefinitionRecord` was designed to hold the UUID of the corresponding `kb.agent_definitions` row. Currently it is always empty string.

**Fix**: when the blueprint applier creates agents, it should:
1. Create the `Agent` record in `kb.agents`
2. Create the `AgentDefinition` record in `kb.agent_definitions`  
3. Upsert the `AgentDefinitionRecord` graph object with `operationalDefinitionId = <agent_definition_uuid>`

This gives agents in the graph a direct pointer to the operational record, enabling tools like `get_agent_definition` to fetch the full spec.

**Verdict**: This is the correct long-term fix for the graph-side data model. It doesn't replace Option A — they complement each other. Option A fixes the `list_agents` tool. Option B fixes the graph data model so graph queries return accurate, live-linked records.

---

### Option C: Inject catalog into `trigger_agent` description (OpenCode pattern)

Fully apply the catalog-injection-into-tool-description pattern from the guide. The `list_agents` tool description becomes the agent catalog, dynamically built at session init:

```
list_agents:
  List all runtime agents for the current project.
  
  Available agents:
  - leaf-researcher (id: <uuid>): Conducts research tasks and synthesises findings.
    Type: leaf | Tasks: research | Tier: cheap | Model: gemini-2.5-flash
  - leaf-coder (id: <uuid>): Produces code artifacts satisfying acceptance criteria.
    Type: leaf | Tasks: coding | Tier: standard | Model: gemini-2.5-flash
  - orchestrator (id: <uuid>): Decomposes work packages and drives execution.
    Type: orchestrator | Tasks: orchestration | Tier: standard
```

The orchestrator would never need to call `list_agents` at all — it already knows the UUIDs and capabilities from the tool description. It just calls `trigger_agent` directly.

**What changes in the server**: `NewListAgentsTool()` in coordination_tools.go (or equivalently, the MCP tool registration in mcp_tools.go) builds the description dynamically by calling `repo.FindAll()` at tool construction time and injecting the catalog.

**Verdict**: This is the OpenCode-native pattern and gives the cleanest LLM experience — zero extra tool calls to discover agents. The tradeoff is that the catalog is a snapshot at session start, not live during the run. For most cases this is fine. Pool managers that run long A/B experiments might need a mid-run refresh — that's what `list_agents` (as a callable tool) covers.

---

## Recommended Path: All Three, in Order

These three options are not mutually exclusive. Apply them in order:

### Step 1 (Blueprint side) — Populate `operationalDefinitionId` in seed data

The quickest fix that requires no server code change. When the blueprint is applied, the server returns the created agent UUIDs. The blueprint applier should write these back to the `AgentDefinitionRecord` graph objects.

In the short term, the seed data should at minimum have non-empty `operationalDefinitionId` values. This requires the blueprint to be applied, IDs retrieved, and the JSONL updated. Add a note to the blueprint README explaining this step.

**In `AgentDefinitionRecord.jsonl`**, the `operationalDefinitionId` field should be populated with the emergent agent definition ID, or the blueprint applier needs to support a `$ref` lookup to fill it post-apply.

### Step 2 (Server side) — Enrich `list_agents` response

In `domain/agents/mcp_tools.go`, `ExecuteListAgents` should join `kb.agents` with `kb.agent_definitions` on `name` and include `description`, `agentType` (from config), `taskTypes` (from config), `tier` (from config), and `model.name` in the response.

This is a one-function change in `ExecuteListAgents` + a DTO extension. The most important field to add is `description` — without it, an LLM cannot make an informed selection.

### Step 3 (Server side) — Inject catalog into tool description at session init

For the `trigger_agent` tool (or a combined `discover_and_trigger_agent` tool), build the description dynamically by injecting the live agent catalog as described in `agent-catalog-injection-guide.md`. This eliminates the need for agents to call `list_agents` before `trigger_agent`.

This is the full OpenCode pattern. It requires the agent executor to build tools per-session with the current project's agent catalog injected.

---

## What to Remove from System Prompts Once Fixed

The current orchestrator system prompt contains this procedural instruction:

> "Call list_agents to get the UUIDs of runtime agents in this project. The trigger_agent tool requires a UUID (not a name). Use the id field from list_agents results. Match agents by name to find the right UUID."

Once Steps 2 and 3 above are done, this entire section is eliminated. The orchestrator doesn't need to be told how to get UUIDs — it either has them in the tool description (Step 3) or gets them with description in one call (Step 2).

Similarly, pool manager prompts contain:

> "Call list_agents to get the runtime UUID of the selected leaf-researcher agent. The trigger_agent tool requires a UUID (not a name)."

This goes away too. Pool managers can reason directly: "I need to run leaf-researcher → trigger_agent(agent_name='leaf-researcher')" if the tool accepts names, or have the UUID already from the description.

---

## Side Proposal: Accept Names in `trigger_agent`

While enriching `list_agents`, also consider accepting `agent_name` as an alternative to `agent_id` in `trigger_agent`:

```json
{ "agent_id": "uuid" }       ← existing, still works
{ "agent_name": "leaf-researcher" }  ← new, resolves by name within the project
```

This eliminates the UUID lookup entirely. Agents reason about other agents by name (which is meaningful and stable), not by UUID (which is implementation detail). The server resolves the name to an ID internally.

This is a small change in `ExecuteTriggerAgent` in `mcp_tools.go` — accept either `agent_id` or `agent_name`, resolve to agent record, proceed. It makes all the procedural "look up UUID by name" instructions in system prompts unnecessary.

---

## Summary Table

| Fix | Location | Effort | Impact |
|---|---|---|---|
| Populate `operationalDefinitionId` in seed JSONL | Blueprint (`seed/objects/AgentDefinitionRecord.jsonl`) | Small — requires ID lookup after apply | Fixes broken graph link |
| Add `description` + capability fields to `list_agents` response | Server (`domain/agents/mcp_tools.go`) | Small — DTO extension + join query | Eliminates 2-call join in orchestrator/pool managers |
| Accept `agent_name` in `trigger_agent` | Server (`domain/agents/mcp_tools.go`) | Small — input resolution | Eliminates UUID-lookup instructions from all system prompts |
| Inject catalog into tool description at session init | Server (`domain/agents/coordination_tools.go` or `mcp_tools.go`) | Medium — tool construction becomes per-session | Full OpenCode pattern — zero discovery overhead |
| Remove procedural UUID-lookup instructions from system prompts | Blueprint (`agents/*.yaml`) | Small — prompt editing | Cleaner, shorter, more robust agent prompts |
