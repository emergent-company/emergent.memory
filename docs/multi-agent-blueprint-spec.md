# Spec: Multi-Agent Task Management Blueprint for Emergent Memory

## Overview

This spec defines a blueprint that encodes the Dynamic Multi-Agent Orchestration System
described in `docs/multi-agent-work-concept.md` as a first-class knowledge schema inside
Emergent Memory.  The blueprint creates two layers of structure:

1. **Template pack** — the domain schema: all object types and relationship types needed
   to represent tasks, agents, pools, experiments, feedback signals, and KPIs.
2. **Agent definitions** — the live actors: leaf agents, pool managers, an orchestrator,
   and a janitor orchestrator, each wired with the appropriate system prompt, tools,
   model config, and flow type.

After applying this blueprint a project contains:
- A complete, queryable knowledge graph of all work packages, tasks, agents, and
  performance data.
- A running set of AI agents that can create, assign, review, and improve tasks by
  operating directly against the graph.

---

## Part 1 — Template Pack Schema

### Pack metadata

| Field | Value |
|---|---|
| name | `multi-agent-task-pack` |
| version | `1.0.0` |
| description | Schema for dynamic multi-agent task management and orchestration |

---

### Object Types

#### Domain Layer (product knowledge)

| Type | Label | Purpose |
|---|---|---|
| `WorkPackage` | Work Package | Top-level unit of work; owns the full execution tree |
| `Task` | Task | Any unit of work; may have a parent task or work package |
| `AcceptanceCriteria` | Acceptance Criteria | A single checklist item defining what "done" looks like |
| `TaskResult` | Task Result | Structured output produced by an agent when completing a task |
| `FeedbackSignal` | Feedback Signal | An accept/reject signal with optional explanation |

#### System Layer (meta-knowledge)

| Type | Label | Purpose |
|---|---|---|
| `AgentDefinitionRecord` | Agent Definition | Registry entry for an agent variant (type, model, skills, tools) |
| `AgentPool` | Agent Pool | A named pool of agent variants managed by a pool manager |
| `AgentRun` | Agent Run | A single execution of an agent against a task |
| `Experiment` | Experiment | An A/B test: multiple agent variants run against the same task |
| `ExperimentVariant` | Experiment Variant | One arm of an experiment (agent + result + score) |
| `KPI` | KPI | A tracked performance metric at the pool or system level |
| `KPISnapshot` | KPI Snapshot | A point-in-time reading of a KPI |
| `JanitorProposal` | Janitor Proposal | A structural change proposed by the janitor orchestrator |
| `Model` | Model | A registered LLM model available as a resource |

---

### Relationship Types

| Name | Label | Source | Target | Purpose |
|---|---|---|---|---|
| `has_subtask` | Has Subtask | `WorkPackage` / `Task` | `Task` | Task tree decomposition |
| `has_criteria` | Has Criteria | `Task` | `AcceptanceCriteria` | Links criteria to tasks |
| `assigned_to` | Assigned To | `Task` | `AgentDefinitionRecord` | Which agent variant handles this task |
| `produced_result` | Produced Result | `AgentRun` | `TaskResult` | Output of a run |
| `received_feedback` | Received Feedback | `TaskResult` | `FeedbackSignal` | Quality signal on a result |
| `failed_criteria` | Failed Criteria | `FeedbackSignal` | `AcceptanceCriteria` | Which criteria a rejection references |
| `member_of_pool` | Member Of Pool | `AgentDefinitionRecord` | `AgentPool` | Agent variant → pool |
| `managed_by` | Managed By | `AgentPool` | `AgentDefinitionRecord` | Pool → manager agent |
| `ran_for_task` | Ran For Task | `AgentRun` | `Task` | Execution → task |
| `run_by_agent` | Run By Agent | `AgentRun` | `AgentDefinitionRecord` | Execution → agent variant |
| `tested_in` | Tested In | `AgentRun` | `Experiment` | A/B run membership |
| `experiment_for_task` | Experiment For Task | `Experiment` | `Task` | Experiment scope |
| `kpi_for_pool` | KPI For Pool | `KPI` | `AgentPool` | Pool-level metric |
| `kpi_snapshot_of` | KPI Snapshot Of | `KPISnapshot` | `KPI` | Time-series snapshot |
| `proposes_change_to` | Proposes Change To | `JanitorProposal` | `AgentDefinitionRecord` / `AgentPool` | Janitor proposal target |
| `uses_model` | Uses Model | `AgentDefinitionRecord` | `Model` | Agent → model binding |

---

### Key Properties

#### `Task`
```
key           string   — stable slug (e.g. "wp-001:research-01")
title         string
type          enum     — work_package | research | enrichment | coding | review | planning
status        enum     — created | in_progress | blocked | complete | rejected
priority      integer  — 1 (highest) to 5
tokensUsed    integer
timeSpentMs   integer
rejectionCount integer
parentKey     string   — key of parent Task or WorkPackage (null for roots)
```

#### `AgentDefinitionRecord`
```
key           string   — stable slug (e.g. "researcher-v1")
agentType     enum     — leaf | pool_manager | orchestrator | janitor
skills        []string
tools         []string
modelName     string
tier          string   — cheap | standard | premium
```

#### `AgentPool`
```
key           string
name          string
agentType     string   — which leaf type this pool serves
selectionStrategy string — cheapest_first | round_robin | weighted
```

#### `Experiment`
```
key           string
status        enum     — running | concluded
trigger       enum     — rejection | proactive | manual
configRate    float    — 0.0–1.0 proactive test rate
```

#### `KPI`
```
key           string
name          string
unit          string   — percent | ms | tokens | count
targetValue   float
```

#### `JanitorProposal`
```
key           string
summary       string
changeType    enum     — new_agent_type | model_swap | template_update | tool_addition | retire_agent
status        enum     — pending_approval | approved | rejected
humanFeedback string
```

---

## Part 2 — Agent Definitions

### Naming convention

All agent names are prefixed by their role tier:
- `leaf-*` — executes a specific task type
- `pool-manager-*` — owns a pool; selects agents, runs A/B tests
- `orchestrator` — breaks work packages into subtasks, assigns them
- `janitor` — analyzes the system and proposes structural improvements

### Tool set reference

All agents have access to at minimum:
- `search` — semantic search over the knowledge graph
- `graph_query` — structured queries against graph objects and relationships
- `graph_write` — create and update objects and relationships

Orchestrators additionally have:
- `agent_trigger` — trigger another agent run
- `human_checkpoint` — present a plan/result to a human and wait for a response

Janitor additionally has:
- `agent_definition_write` — create or modify agent definition records (in the graph)

---

### Leaf Agents

#### `leaf-enricher`
- **Purpose**: Extract and enrich structured properties from raw content
- **Flow**: agentic
- **Model**: gpt-4o-mini (cheap tier — escalate on rejection)
- **Max steps**: 8

#### `leaf-researcher`
- **Purpose**: Conduct research tasks; synthesise findings into structured `TaskResult` objects
- **Flow**: agentic
- **Model**: gpt-4o-mini
- **Max steps**: 12

#### `leaf-coder`
- **Purpose**: Produce code artifacts that satisfy acceptance criteria
- **Flow**: agentic
- **Model**: gpt-4o (standard tier)
- **Max steps**: 20

#### `leaf-reviewer`
- **Purpose**: Evaluate a `TaskResult` against its `AcceptanceCriteria`; emit a `FeedbackSignal`
- **Flow**: agentic
- **Model**: gpt-4o-mini
- **Max steps**: 8

#### `leaf-designer`
- **Purpose**: Produce design artifacts (architecture diagrams, API specs, UI mockups as text)
- **Flow**: agentic
- **Model**: gpt-4o
- **Max steps**: 15

---

### Pool Managers

#### `pool-manager-research`
- **Purpose**: Manages the researcher pool; selects agent variants, runs A/B tests on rejection
- **Pool**: research pool
- **Flow**: agentic
- **Model**: gpt-4o
- **Max steps**: 15

#### `pool-manager-coding`
- **Purpose**: Manages the coder pool
- **Pool**: coding pool
- **Flow**: agentic
- **Model**: gpt-4o
- **Max steps**: 15

#### `pool-manager-review`
- **Purpose**: Manages the reviewer pool
- **Pool**: review pool
- **Flow**: agentic
- **Model**: gpt-4o-mini
- **Max steps**: 10

---

### Orchestrator

#### `orchestrator`
- **Purpose**: Receives a `WorkPackage`, decomposes it into a task tree, assigns tasks to pools,
  monitors progress, presents plan + three options to humans at key checkpoints
- **Flow**: agentic
- **Model**: gpt-4o (with o1/claude-3-5-sonnet as fallback for complex plans)
- **Max steps**: 40
- **Human checkpoint**: always before execution begins; at each major milestone

---

### Janitor Orchestrator

#### `janitor`
- **Purpose**: Periodic system analysis; proposes structural changes (new agent types, model swaps,
  prompt updates, tool additions, agent retirement). All proposals require human approval.
- **Flow**: agentic
- **Model**: gpt-4o (o1 for major proposals)
- **Max steps**: 30
- **Trigger**: scheduled (after every N closed work packages, configurable)

---

## Part 3 — Seed Data

The seed layer pre-populates the graph with:

1. **Agent pool objects** — one `AgentPool` record per pool (research, coding, review, enrichment, design)
2. **Model registry** — one `Model` record per available LLM (gpt-4o, gpt-4o-mini, claude-3-5-sonnet, o1)
3. **KPI baselines** — one `KPI` object per tracked metric, with null baseline snapshots
4. **Agent definition records** — one `AgentDefinitionRecord` per agent listed above, linked to their pool

KPI set (initial):
| Key | Name | Unit | Target |
|---|---|---|---|
| `kpi-rejection-rate` | Rejection Rate | percent | 10 |
| `kpi-task-completion-ms` | Avg Task Completion Time | ms | 30000 |
| `kpi-token-cost` | Token Cost Per Task | tokens | 5000 |
| `kpi-reopen-rate` | Task Reopen Rate | percent | 5 |
| `kpi-plan-approval` | Orchestrator Plan Approval Rate | percent | 80 |
| `kpi-janitor-approval` | Janitor Proposal Approval Rate | percent | 70 |

---

## Part 4 — Blueprint Directory Layout

```
blueprints/multi-agent/
  packs/
    multi-agent-task-pack.yaml    ← full schema (object types + relationship types)
  agents/
    leaf-enricher.yaml
    leaf-researcher.yaml
    leaf-coder.yaml
    leaf-reviewer.yaml
    leaf-designer.yaml
    pool-manager-research.yaml
    pool-manager-coding.yaml
    pool-manager-review.yaml
    orchestrator.yaml
    janitor.yaml
  seed/
    objects/
      AgentPool.jsonl
      AgentDefinitionRecord.jsonl
      Model.jsonl
      KPI.jsonl
    relationships/
      member_of_pool.jsonl
      uses_model.jsonl
      kpi_for_pool.jsonl
```

---

## Part 5 — Design Decisions & Rationale

### Why two knowledge graph layers?

The concept doc explicitly separates **domain knowledge** (the actual work) from
**system knowledge** (how the system is performing). Storing both in the same graph
enables the janitor to run graph queries across the full system history without any
external database.

### Why `AgentDefinitionRecord` objects in the graph vs. only agent definitions?

Agent definitions in Emergent Memory are operational (they instantiate runners). The
`AgentDefinitionRecord` in the graph is the **data representation** of an agent variant —
it stores skills, tools, model tier, and is linked to runs, experiments, and pool membership.
The janitor can propose changes to `AgentDefinitionRecord` objects and a human can approve
before the operational agent definition is updated.

### Acceptance criteria as first-class objects

Linking `AcceptanceCriteria` objects directly to tasks (and `FeedbackSignal` → `AcceptanceCriteria`)
makes rejection pattern analysis a simple graph traversal — the janitor can query "which
criteria are most frequently cited in rejections per agent type" without parsing free text.

### Progressive escalation via pool managers

Pool managers do not change agent assignment rules live — they create `Experiment` records
and let A/B results accumulate before proposing updates to selection logic. This keeps the
system cheap to experiment with and preserves audit history.

### Human touchpoints

Three checkpoints are encoded in the orchestrator's system prompt:
1. Present plan + 3 options before starting significant work
2. Present results at major milestones
3. All janitor proposals — always require explicit human approval

---

## Part 6 — Lab Phase Validation Plan

Per the concept doc's recommendation, validate with a contained test before connecting to
production workloads:

1. Create a `WorkPackage` with `title: "Build a to-do list app"`
2. Trigger the `orchestrator` agent with the work package key
3. Observe: task tree formation, agent assignments, criteria creation
4. Let at least one rejection cycle complete (reviewer → enricher or coder)
5. After closure, trigger `janitor` and review its `JanitorProposal` objects
6. Calibrate KPI baselines from the observed run data
