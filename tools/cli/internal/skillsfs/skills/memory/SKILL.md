---
name: memory
description: Experimental skill that delegates all Memory operations to `memory ask` instead of direct CLI commands. Use when you want to drive Memory through a single natural-language interface rather than composing individual CLI commands.
experimental: true
metadata:
  author: emergent
  version: "1.0"
---

Use `NO_PROMPT=1 memory ask "<task>"` as the primary interface for all Memory operations. This skill delegates everything to the `memory ask` command, which routes requests to the server-side CLI assistant agent with access to 30+ tools covering graph read/write, agents, schemas, MCP servers, documents, skills, projects, and more.

## Rules

- **Never run `memory browse`** — it launches a full interactive TUI that blocks on terminal input and will hang in an automated agent context.
- **Always prefix `memory` commands with `NO_PROMPT=1`** (e.g. `NO_PROMPT=1 memory ask "<task>"`). Without it, the CLI may show interactive pickers when no project, agent, MCP server, skill, or agent-definition ID is provided. Do not add this to `.env.local` — it must only apply to agent-driven invocations.
- **Always supply a project** with `--project <id>` when the task is project-scoped, or ensure `MEMORY_PROJECT` is set.

---

## How to invoke

```bash
# General task (uses MEMORY_PROJECT from environment or .env.local)
NO_PROMPT=1 memory ask "<describe what you want to do>"

# Explicit project
NO_PROMPT=1 memory ask "<describe what you want to do>" --project <project-id>

# Structured output
NO_PROMPT=1 memory ask "<describe what you want to do>" --project <project-id> --json
```

### Examples

```bash
# List all agents in a project
NO_PROMPT=1 memory ask "list all agents" --project abc123

# Create a graph object
NO_PROMPT=1 memory ask "create a Service object named auth-service that handles authentication" --project abc123

# Query the knowledge graph
NO_PROMPT=1 memory ask "what are the main components and how do they relate?" --project abc123

# Add an MCP server
NO_PROMPT=1 memory ask "register an SSE MCP server named my-tools at http://localhost:8080/sse" --project abc123

# Check provider configuration
NO_PROMPT=1 memory ask "is a provider configured and working?"

# Install a template pack
NO_PROMPT=1 memory ask "install the template pack with id <pack-id>" --project abc123
```

---

## When `memory ask` is sufficient

Use `memory ask` for tasks that are:
- **Read operations** — listing, querying, inspecting any resource
- **Simple write operations** — creating/updating/deleting a single resource
- **Configuration checks** — verifying providers, projects, agents, MCP servers
- **Graph operations** — creating objects, relationships, running queries

## When `memory ask` is NOT sufficient

Fall back to direct `NO_PROMPT=1 memory <cmd>` commands for tasks that require:
- **Writing files to disk** — `memory ask` cannot create `pack.json`, `.env.local`, blueprint directories, or any local files. Use the Write/Edit tools for that, then call `memory` CLI commands as needed.
- **Multi-step wizards with confirmation loops** — `memory ask` is single-turn. For interactive onboarding workflows, load the `memory-onboard` skill instead.
- **Large data exports** — use `NO_PROMPT=1 memory blueprints dump <output-dir>` directly.
- **Binary/streaming operations** — use direct CLI commands.

---

## Checking the project context

Before making project-scoped calls, confirm which project to use:

```bash
# Check if MEMORY_PROJECT is set in .env.local
grep MEMORY_PROJECT .env.local 2>/dev/null

# Or list available projects
NO_PROMPT=1 memory projects list
```

Always pass `--project <id>` explicitly in `memory ask` rather than relying on ambient config — it avoids ambiguity and prevents the interactive project picker from triggering.

---

## Notes

- `memory ask` never triggers the interactive project picker — it is safe to call without a project configured.
- The `--json` flag returns structured output from `memory ask` when the assistant emits JSON.
- `memory ask` is single-turn: it does not support multi-step clarification loops. If the task requires multiple rounds of input, break it into separate `memory ask` calls.
- The CLI assistant agent has read/write access to the full Memory API surface. It can create, update, and delete resources — phrase requests carefully.
