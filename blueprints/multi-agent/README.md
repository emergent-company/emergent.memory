# Multi-Agent Blueprint

An example blueprint that installs a complete hierarchical multi-agent orchestration system into your Memory project.

## What it installs

**Schema pack** — `multi-agent-task-pack` with object types for the full work lifecycle:
- `WorkPackage` → `Task` → `AcceptanceCriteria` → `TaskResult` → `FeedbackSignal`
- System layer: `AgentDefinitionRecord`, `AgentPool`, `AgentRun`, `Experiment`, `ExperimentVariant`, `KPI`, `KPISnapshot`, `JanitorProposal`, `Model`

**11 agent definitions** across three tiers:

| Agent | Role |
|---|---|
| `orchestrator` | Decomposes WorkPackages into task trees, presents plans to humans at checkpoints |
| `coding-manager` | Pool manager — selects coders, runs A/B experiments on rejection |
| `research-manager` | Pool manager — routes tasks to web-researcher or code-researcher |
| `review-manager` | Pool manager — monitors reviewer quality |
| `coder` | Produces code artifacts satisfying acceptance criteria |
| `web-researcher` | Researches external web content, public docs, news |
| `code-researcher` | Analyses codebases inside a sandboxed workspace container |
| `reviewer` | Evaluates TaskResults against AcceptanceCriteria, emits FeedbackSignals |
| `enricher` | Extracts structured entities from raw content into the graph |
| `designer` | Produces architecture diagrams, API specs, data models, design docs |
| `janitor` | Periodic system analysis — proposes model swaps, pool restructuring, new agent types (all require human approval) |

**Seed data** — pre-wired agent pools, KPIs, and model registry.

## Install

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/multi-agent --project <your-project-slug>
```

Or from a local clone:

```bash
memory blueprints install ./blueprints/multi-agent --project <your-project-slug>
```

## How it works

The orchestrator receives a `WorkPackage` key, decomposes it into a task tree, and presents the plan to the human before execution. Pool managers route tasks to leaf agents, manage A/B experiments on repeated rejections, and escalate to higher-tier models when standard agents fail. The janitor runs periodically, analyzes closed work packages, computes KPI readings, and proposes structural improvements — all requiring human approval.

Human checkpoints occur at:
1. Plan approval (before any execution starts)
2. Every 25% milestone during execution
3. Janitor proposals (batch, after every N closed work packages)

## Model

Agents in this blueprint use your project's default model. No model is pinned — configure your preferred model in your Memory project settings before installing.
