# Agents Guide

A practical reference for creating, running, and managing AI agents in Emergent Memory using the `memory` CLI.

## Prerequisites

Install the CLI (if not already):

```bash
task cli:install
```

Set your default project so you don't need `--project` on every command:

```bash
export MEMORY_PROJECT=my-project
# or: export MEMORY_PROJECT_ID=<uuid>
```

Server default is `http://localhost:3012`. For a remote server, override:

```bash
export MEMORY_SERVER_URL=http://localhost:3012
```

---

## Core Concepts

There are three distinct things you work with:

| Concept | Table | What it is |
|---|---|---|
| **Agent Definition** | `kb.agent_definitions` | Reusable blueprint: system prompt, model, tools whitelist |
| **Agent** | `kb.agents` | Runtime instance: trigger type, schedule, enabled state |
| **Agent Run** | `kb.agent_runs` | One execution: status, steps, messages, tool calls |

An **Agent** points to an **Agent Definition** by name. You trigger an **Agent**, which creates an **Agent Run**.

---

## Step 1 — Create an Agent Definition

The definition controls *what* the agent does.

```bash
memory defs create \
  --name "document-summarizer" \
  --system-prompt "You are an expert at summarizing technical documents. Extract key points and write a concise summary." \
  --model "gemini-2.0-flash" \
  --tools "search,graph_query,graph_create_object" \
  --flow-type single \
  --visibility project \
  --max-steps 20
```

**Key flags:**

| Flag | Description |
|---|---|
| `--name` | Unique name within the project (required) |
| `--system-prompt` | The system-level instructions for the LLM |
| `--model` | Model name, e.g. `gemini-2.0-flash`, `gemini-2.5-flash` |
| `--tools` | Comma-separated tool names the agent is allowed to call |
| `--flow-type` | `single` (one LLM), `multi` (parallel), `coordinator` (spawns sub-agents) |
| `--visibility` | `project` (default), `internal` (system only), `external` (ACP-exposed) |
| `--max-steps` | Hard cap on LLM steps per run (server default: 500) |

**List and inspect definitions:**

```bash
memory defs list
memory defs get <def-id>
```

---

## Step 2 — Create a Runtime Agent

The agent controls *when* the definition runs.

### Manual trigger (on demand)

```bash
memory agents create \
  --name "doc-summarizer" \
  --trigger-type manual
```

### Scheduled (cron)

```bash
memory agents create \
  --name "nightly-report" \
  --trigger-type schedule \
  --cron "0 0 2 * * *"
```

Cron format: `sec min hour dom month dow` (6-field, standard cron notation).

### React to graph events

```bash
memory agents create \
  --name "auto-tagger" \
  --trigger-type reaction \
  --reaction-events "created,updated" \
  --reaction-object-types "document"
```

The agent fires automatically whenever a matching object type is created or updated in the knowledge graph.

### Webhook (external trigger)

```bash
memory agents create \
  --name "ci-trigger" \
  --trigger-type webhook
```

See [Webhook Hooks](#webhook-hooks) below for how to generate the hook token.

**List and inspect agents:**

```bash
memory agents list
memory agents get <agent-id>
```

---

## Step 3 — Trigger a Run

```bash
memory agents trigger <agent-id>
```

Output:
```
Agent triggered successfully!
  Run queued.
```

---

## Step 4 — Monitor Runs

```bash
memory agents runs <agent-id> --limit 10
```

Output:
```
1. Run a1b2c3d4
   Status:    completed
   Started:   2026-03-08 14:22:01
   Completed: 2026-03-08 14:22:15
   Duration:  14230ms

2. Run e5f6g7h8
   Status:    failed
   Started:   2026-03-08 12:10:05
   Error:     max steps exceeded
```

**Run statuses:** `running` · `completed` · `failed` · `skipped` · `cancelled`

---

## Human-in-the-Loop

If the agent definition includes the `ask_user` tool and the LLM calls it, the run **pauses** and waits for your input before continuing.

### See pending questions

```bash
memory agents questions list-project --status pending
```

Output:
```json
[{
  "id": "q_abc",
  "question": "I found 3 documents matching 'Q4 report'. Which one should I summarize?",
  "options": ["Q4-Finance.pdf", "Q4-Engineering.pdf", "Q4-Marketing.pdf"],
  "status": "pending"
}]
```

### Respond to resume the run

```bash
memory agents questions respond q_abc "Q4-Finance.pdf"
```

The agent resumes from exactly where it paused. Resuming creates a new linked `AgentRun` that reuses the same conversation session.

### List questions for a specific run

```bash
memory agents questions list <run-id>
```

---

## Webhook Hooks

Webhooks let external systems (CI/CD, GitHub Actions, etc.) trigger an agent via HTTP.

### Create a hook

```bash
memory agents hooks create <agent-id> \
  --label "GitHub Actions" \
  --rate-limit 30 \
  --burst-size 5
```

Output:
```
Webhook hook created successfully!
  ID:    hook_xyz
  Label: GitHub Actions

  Token: eyJ...rawtoken...

  WARNING: Save this token now. It will not be shown again.
```

The plaintext token is shown **once only**. Store it in your secrets manager.

### Trigger from an external system

```bash
curl -X POST https://api.dev.emergent-company.ai/api/webhooks/agents/hook_xyz \
  -H "Authorization: Bearer <token>"
```

No project auth is required — the bearer token identifies both the hook and the agent.

### Manage hooks

```bash
memory agents hooks list <agent-id>
memory agents hooks delete <agent-id> <hook-id>
```

---

## Inspect Conversation History (ADK Sessions)

Each agent run is backed by an ADK session that stores the full LLM conversation history. Useful for debugging what the agent actually did.

```bash
memory sessions list
memory sessions get <session-id>
```

The session output includes every LLM turn, tool call input, and tool call output in chronological order.

---

## Apply Agents via Blueprints (GitOps)

For repeatable setups across environments, define agent definitions as YAML files and apply them with a single command.

### Directory layout

```
my-blueprints/
  agents/
    document-summarizer.yaml
    auto-tagger.yaml
  packs/
    my-template-pack.yaml
```

### Agent definition YAML format

```yaml
name: document-summarizer
systemPrompt: |
  You are an expert at summarizing technical documents.
  Extract key points and write a concise summary.
model:
  name: gemini-2.0-flash
  temperature: 0.7
  maxSteps: 20
tools:
  - search
  - graph_query
  - graph_create_object
flowType: single
visibility: project
```

### Apply blueprints

```bash
# From a local directory
memory blueprints ./my-blueprints

# From GitHub
memory blueprints https://github.com/acme/memory-blueprints

# From a specific tag
memory blueprints https://github.com/acme/memory-blueprints#v1.2.0

# Preview without making changes
memory blueprints ./my-blueprints --dry-run

# Update existing definitions (default: skip if name exists)
memory blueprints ./my-blueprints --upgrade
```

### Export current state as a blueprint

```bash
memory blueprints dump ./my-export
```

Writes per-type JSONL seed files for all graph objects and relationships — useful as a starting point or for migrating between projects.

---

## Enable / Disable an Agent

```bash
# Disable (stops cron and reaction triggers)
memory agents update <agent-id> --enabled false

# Re-enable
memory agents update <agent-id> --enabled true
```

---

## Update and Delete

```bash
# Update agent definition
memory defs update <def-id> \
  --system-prompt "Updated instructions..." \
  --max-steps 50

# Update agent schedule
memory agents update <agent-id> \
  --cron "0 0 6 * * *"

# Delete (irreversible)
memory agents delete <agent-id>
memory defs delete <def-id>
```

---

## Quick Reference

```bash
# Definitions
memory defs list
memory defs create --name <n> --system-prompt <p> --model <m> --tools <t1,t2>
memory defs get <id>
memory defs update <id> [flags]
memory defs delete <id>

# Agents
memory agents list
memory agents create --name <n> --trigger-type manual|schedule|reaction|webhook
memory agents get <id>
memory agents update <id> [flags]
memory agents trigger <id>
memory agents runs <id> [--limit <n>]
memory agents delete <id>

# Human-in-the-loop
memory agents questions list-project [--status pending]
memory agents questions list <run-id>
memory agents questions respond <question-id> "<answer>"

# Webhook hooks
memory agents hooks list <agent-id>
memory agents hooks create <agent-id> --label <l> [--rate-limit <n>]
memory agents hooks delete <agent-id> <hook-id>

# Session history
memory sessions list
memory sessions get <session-id>

# Blueprints
memory blueprints <source> [--upgrade] [--dry-run]
memory blueprints dump <output-dir>
```

---

## Safety Limits

| Limit | Default | Notes |
|---|---|---|
| Max steps per run | 500 | Cumulative across resumes; configurable per definition via `--max-steps` |
| Doom loop detection | 5 consecutive identical tool calls | Warns at 3, halts at 5 |
| Webhook rate limiting | Configurable per hook | Set via `--rate-limit` (req/min) and `--burst-size` |
| Sub-agent depth | No hard limit | Tracked via `Depth` field on each run; monitor to avoid infinite recursion |
