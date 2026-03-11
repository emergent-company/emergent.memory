---
name: memory-query
description: Query an Emergent project knowledge base using natural language or direct hybrid search. Use when the user wants to search, explore, or ask questions about content in an Emergent project.
metadata:
  author: emergent
  version: "1.0"
---

> **New to Emergent?** Load the `memory-onboard` skill first to set up the project's knowledge graph schema before querying.

Query an Emergent project knowledge base using the `emergent query` command.

## Modes

**Agent mode (default)** — AI reasoning over the knowledge graph. Best for complex or multi-hop questions.

```bash
emergent query "who directed fight club and what are their other movies?"
emergent query --project-id <id> "list all requirements for the auth module"
emergent query --show-tools "what are the key relationships between X and Y?"
```

**Search mode** — Direct hybrid search (semantic + lexical), no AI reasoning. Best for finding specific content fast.

```bash
emergent query --mode=search "fight club"
emergent query --mode=search --result-types=graph --limit=20 "authentication"
emergent query --mode=search --result-types=text "API rate limiting"
```

## Key Flags

| Flag | Default | Notes |
|---|---|---|
| `--mode` | `agent` | `agent` or `search` |
| `--project-id` | from config | Override target project |
| `--json` | false | Machine-readable output |
| `--show-tools` | false | Show agent tool calls (agent mode only) |
| `--limit` | 10 | Max results (search mode only) |
| `--result-types` | `both` | `graph`, `text`, or `both` (search mode only) |
| `--fusion-strategy` | `weighted` | `weighted`, `rrf`, `interleave`, `graph_first`, `text_first` |

## Workflow

1. If the user's question is broad or relational → use **agent mode** (default)
2. If the user wants to find specific documents or objects quickly → use **search mode**
3. If no `--project-id` is set, the CLI uses the default project from config; ask the user to confirm or specify one if the context is ambiguous
4. Use `--output json` + `--json` for downstream processing or to pass results to another tool

## Output Format

Agent mode streams the response and prints a final answer. Search mode returns a table (or JSON) of matching objects/documents with scores.

Use `--json` to get structured output suitable for parsing:
```bash
emergent query --mode=search --json "authentication" | jq '.[].title'
```
