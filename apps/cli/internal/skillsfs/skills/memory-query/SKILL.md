---
name: memory-query
description: Read from the knowledge graph — natural language questions, semantic search, or hybrid search over objects and documents.
metadata:
  author: emergent
  version: "2.0"
---

> **New to Emergent?** Load the `memory-onboard` skill first to set up the project's knowledge graph schema before querying.
> **Want to write to the graph?** Load the `memory-graph` skill to create, update, or delete objects and relationships.

Query an Emergent project knowledge base using the `memory query` command.

## Rules

- **Project context is auto-discovered** — the CLI walks up the directory tree to find `.env.local` containing `MEMORY_PROJECT` or `MEMORY_PROJECT_ID`. If `.env.local` is present anywhere above the current directory, `--project` is not needed. Only pass `--project <id>` explicitly when overriding or when no `.env.local` exists.

## Modes

**Agent mode (default)** — AI reasoning over the knowledge graph. Best for complex or multi-hop questions.

```bash
memory query "who directed fight club and what are their other movies?"
memory query --project <id> "list all requirements for the auth module"
memory query --show-tools "what are the key relationships between X and Y?"
```

**Search mode** — Direct hybrid search (semantic + lexical), no AI reasoning. Best for finding specific content fast.

```bash
memory query --mode=search "fight club"
memory query --mode=search --result-types=graph --limit=20 "authentication"
memory query --mode=search --result-types=text "API rate limiting"
```

## Session Continuation

After each agent query, a session ID is printed:

```
Session: abc123  (use --session abc123 to continue)
```

Pass it back to continue the conversation with full history:

```bash
memory query "what are the main services?"
# Session: abc123  (use --session abc123 to continue)

memory query --session abc123 "which of those handles auth?"
```

Sessions are scoped to the project. Only supported in `--mode=agent`.

## Key Flags

| Flag | Default | Notes |
|---|---|---|
| `--mode` | `agent` | `agent` or `search` |
| `--project` | from config | Override target project |
| `--session` | — | Continue a previous query session (agent mode only) |
| `--json` | false | Machine-readable output |
| `--show-tools` | false | Show agent tool calls (agent mode only) |
| `--limit` | 10 | Max results (search mode only) |
| `--result-types` | `both` | `graph`, `text`, or `both` (search mode only) |
| `--fusion-strategy` | `weighted` | `weighted`, `rrf`, `interleave`, `graph_first`, `text_first` |

## Workflow

1. If the user's question is broad or relational -> use **agent mode** (default)
2. If the user wants to find specific documents or objects quickly -> use **search mode**
3. If no `--project` is set, the CLI uses the default project from config; ask the user to confirm or specify one if the context is ambiguous
4. Use `--output json` + `--json` for downstream processing or to pass results to another tool
5. To follow up on a previous query, pass `--session <id>` to continue with full conversation history

## Output Format

Agent mode streams the response and prints a final answer. Search mode returns a table (or JSON) of matching objects/documents with scores.

Use `--json` to get structured output suitable for parsing:
```bash
memory query --mode=search --json "authentication" | jq '.[].title'
```
