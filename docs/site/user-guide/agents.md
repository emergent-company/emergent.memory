# Agents

Agents are AI workers that read and write your knowledge graph autonomously. You define what they do (system prompt, tools, model) and when they run (on demand, on a schedule, or in response to events).

## Concepts

| Concept | Description |
|---|---|
| **Agent Definition** | Reusable blueprint: system prompt, model, tools, flow type |
| **Agent** | Runtime instance: trigger type, schedule, enabled state |
| **Agent Run** | One execution: status, steps, messages, tool calls |

An **Agent** points to an **Agent Definition**. You trigger an **Agent**, which creates an **Agent Run**.

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

| Flag | Description |
|---|---|
| `--name` | Unique name within the project (required) |
| `--system-prompt` | Instructions for the LLM |
| `--model` | Model name, e.g. `gemini-2.0-flash`, `gemini-2.5-flash` |
| `--tools` | Comma-separated tool names the agent may call |
| `--flow-type` | `single` (one LLM), `multi` (parallel), `coordinator` (spawns sub-agents) |
| `--visibility` | `project` (default), `internal` (system only), `external` (ACP-exposed) |
| `--max-steps` | Hard cap on LLM steps per run (default: 500) |

---

## Step 2 — Create a Runtime Agent

The agent controls *when* the definition runs.

=== "Manual (on demand)"
    ```bash
    memory agents create \
      --name "doc-summarizer" \
      --trigger-type manual
    ```

=== "Scheduled (cron)"
    ```bash
    memory agents create \
      --name "nightly-report" \
      --trigger-type schedule \
      --cron "0 0 2 * * *"
    ```

    Cron format: `sec min hour dom month dow` (6-field).

=== "Reaction (graph events)"
    ```bash
    memory agents create \
      --name "auto-tagger" \
      --trigger-type reaction \
      --reaction-events "created,updated" \
      --reaction-object-types "document"
    ```

=== "Webhook (external trigger)"
    ```bash
    memory agents create \
      --name "ci-trigger" \
      --trigger-type webhook
    ```

    See [Webhook Hooks](#webhook-hooks) below for the token setup.

---

## Step 3 — Trigger a Run

```bash
memory agents trigger <agent-id>
```

---

## Step 4 — Monitor Runs

```bash
memory agents runs <agent-id> --limit 10
```

```
1. Run a1b2c3d4
   Status:    completed
   Started:   2026-03-08 14:22:01
   Completed: 2026-03-08 14:22:15
   Duration:  14230ms

2. Run e5f6g7h8
   Status:    failed
   Error:     max steps exceeded
```

**Statuses:** `running` · `completed` · `failed` · `skipped` · `cancelled`

---

## Human-in-the-Loop

If the agent includes the `ask_user` tool, execution **pauses** when the agent calls it and waits for your response.

### See pending questions

```bash
memory agents questions list-project --status pending
```

### Respond to resume execution

```bash
memory agents questions respond <question-id> "My answer here"
```

---

## Webhook Hooks

Webhooks let external systems (CI/CD, GitHub Actions) trigger an agent via HTTP.

### Create a hook

```bash
memory agents hooks create <agent-id> \
  --label "GitHub Actions" \
  --rate-limit 30 \
  --burst-size 5
```

!!! warning "Save the token"
    The plaintext bearer token is shown **once only** at creation time. Store it securely.

### Trigger from an external system

```bash
curl -X POST https://api.dev.emergent-company.ai/api/webhooks/agents/<hook-id> \
  -H "Authorization: Bearer <token>"
```

### Manage hooks

```bash
memory agents hooks list <agent-id>
memory agents hooks delete <agent-id> <hook-id>
```

---

## Available Tools

Agents can be given access to tools from the following categories:

| Category | Tool names |
|---|---|
| Knowledge graph | `graph_query`, `graph_create_object`, `graph_update_object`, `graph_search` |
| Search | `search` |
| Workspace (bash/file) | `workspace_bash`, `workspace_read`, `workspace_write`, `workspace_edit`, `workspace_glob`, `workspace_grep`, `workspace_git` |
| Human-in-the-loop | `ask_user` |
| Coordination | `spawn_agents`, `list_available_agents` |

Specify tools as a comma-separated list in the agent definition. Glob patterns are supported (e.g. `graph_*`).

---

## Multi-Agent Coordination

Set `--flow-type coordinator` to create an orchestrator agent that can spawn sub-agents via the `spawn_agents` tool:

```bash
memory defs create \
  --name "research-coordinator" \
  --flow-type coordinator \
  --tools "spawn_agents,list_available_agents,graph_query"
```

The coordinator discovers available agents with `list_available_agents` and delegates work to them. Each sub-agent run is linked to the parent via `parentRunId`.

---

## Blueprints (GitOps)

Define agent definitions as YAML and apply them repeatably:

```yaml
# agents/document-summarizer.yaml
name: document-summarizer
systemPrompt: |
  You are an expert at summarizing technical documents.
model:
  name: gemini-2.0-flash
  temperature: 0.7
  maxSteps: 20
tools:
  - search
  - graph_query
flowType: single
visibility: project
```

```bash
memory blueprints ./my-blueprints --project my-project
memory blueprints ./my-blueprints --dry-run   # preview only
memory blueprints ./my-blueprints --upgrade   # update existing
```

---

## Quick Reference

```bash
# Definitions
memory defs list
memory defs create --name <n> --system-prompt <p> --model <m> --tools <t1,t2>
memory defs update <id> [flags]
memory defs delete <id>

# Agents
memory agents list
memory agents create --name <n> --trigger-type manual|schedule|reaction|webhook
memory agents trigger <id>
memory agents runs <id> [--limit <n>]
memory agents update <id> [flags]
memory agents delete <id>

# Human-in-the-loop
memory agents questions list-project [--status pending]
memory agents questions respond <question-id> "<answer>"

# Webhook hooks
memory agents hooks list <agent-id>
memory agents hooks create <agent-id> --label <l>
memory agents hooks delete <agent-id> <hook-id>
```

---

## Safety Limits

| Limit | Default |
|---|---|
| Max steps per run | 500 (configurable via `--max-steps`) |
| Doom loop detection | Halts after 5 consecutive identical tool calls |
| Webhook rate limiting | Configurable per hook via `--rate-limit` (req/min) |
