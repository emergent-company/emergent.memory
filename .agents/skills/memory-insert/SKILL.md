# memory-insert skill

Insert information into the Memory knowledge graph using natural language with `memory remember`.

## When to use

Use `memory remember` when you want to store facts, tasks, relationships, or events in the graph without writing structured data manually. The agent handles:
- Entity extraction from natural language
- Deduplication against existing graph data
- Schema type reuse or creation
- Branch-based safe writes with auto-merge

## CLI usage

```bash
# Basic — store a fact
memory remember "I have to buy toilet paper at Lidl"

# Task with metadata
memory remember "Meeting with Sarah tomorrow at 3pm about Q3 roadmap"

# Technical fact
memory remember "API server runs on aws-eu-west-1 using Go 1.22"

# Control schema creation
memory remember --schema-policy reuse_only "fix login bug, priority high"

# Preview without merging to main
memory remember --dry-run "note: team offsite on 15 June in Berlin"

# Continue a multi-turn session (e.g. add more context)
memory remember --session <session-id> "also invite the design team"

# See what tools the agent used
memory remember --show-tools "buy milk and eggs"

# JSON output (for scripting)
memory remember --json "user prefers dark mode"
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--project` | config default | Project ID |
| `--schema-policy` | `auto` | `auto` / `reuse_only` / `ask` |
| `--dry-run` | false | Write branch, skip merge |
| `--show-tools` | false | Print tool calls |
| `--show-time` | false | Print elapsed time |
| `--session` | — | Continue previous session |
| `--json` | false | JSON output |

## Schema policies

| Policy | Behaviour |
|--------|-----------|
| `auto` | Creates new schema types when no existing type fits |
| `reuse_only` | Always reuses closest existing type, never creates |
| `ask` | Pauses and asks before creating any new type |

## HTTP API

```http
POST /api/projects/:projectId/remember
Content-Type: application/json

{
  "message": "buy toilet paper at Lidl",
  "schema_policy": "auto",
  "dry_run": false,
  "conversation_id": "<optional-session-id>"
}
```

Response: SSE stream with `meta`, `token`, `mcp_tool`, `done` events — same format as `/query`.

## What the agent does (steps)

1. **Parse** — extract entities, properties, relationships from input
2. **Check schema** — `schema-compiled-types` → find matching types
3. **Dedup** — `search-hybrid` per entity → update if match found
4. **Branch** — `graph-branch-create` with name `remember/<slug>`
5. **Write** — `entity-create` + `relationship-create` on branch
6. **Merge** — `graph-branch-merge execute=true` → main (skip if `dry_run`)
7. **Cleanup** — `graph-branch-delete`
8. **Report** — markdown summary of what was created/updated

## Example output

```
## Remembered

**Entities created:**
- `task-buy-toilet-paper` (Task) — "Buy toilet paper"
- `location-lidl` (Location) — "Lidl"

**Relationships created:**
- task-buy-toilet-paper → located_at → location-lidl

**Branch:** remember/lidl-shopping-2026-05-09 → merged ✓
```
