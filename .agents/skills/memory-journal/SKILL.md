---
name: memory-journal
description: Query and annotate the project journal — an automatic server-side log of all graph mutations (create, update, delete, batch, merge) plus manual notes. Use when you want to understand what changed in the knowledge graph, add context to a change, or review recent activity.
metadata:
  author: emergent
  version: "1.0"
---

The project journal is an **automatic, server-side activity log** for the knowledge graph. Every mutation (object created/updated/deleted, relationships batched, branches merged) is logged automatically. You can also add manual markdown notes — standalone or attached to a specific entry.

> This is distinct from `.memory/journal.md`, which is a local agent session file. The project journal lives in the database and is queryable via CLI, API, and MCP tools.

## CLI Commands

### List recent activity

```bash
memory journal list                    # last 7 days (default)
memory journal list --since 24h        # last 24 hours
memory journal list --since 30m        # last 30 minutes
memory journal list --since 2026-01-15T10:00:00Z  # since ISO-8601 timestamp
memory journal list --limit 50         # cap results
memory journal list --output json      # machine-readable
```

### Add a note

```bash
memory journal note "Skipped worker services — need schema clarification first."
memory journal note                                   # opens $EDITOR
echo "Some context" | memory journal note             # pipe
memory journal note --entry <entry-id> "explanation"  # attach to a specific entry
```

## Output Format

```
2026-03-21 14:32:11  [agent]   CREATED   Service        svc-auth       "authentication service"
2026-03-21 14:32:18  [agent]   CREATED   APIEndpoint    ep-login       "POST /auth/login"
2026-03-21 14:32:20  [agent]   RELATED   svc-auth -> calls -> svc-payments

2026-03-21 15:10:44  [system]  BATCH     47 objects created (Service x12, APIEndpoint x35)

2026-03-21 14:33:01  [agent]   NOTE

  Skipped worker services — need schema clarification first.
  Will revisit once the infra schema pack is finalized.
```

- Actor shown as `[agent]`, `[user]`, or `[system]`
- Notes attached to an entry appear indented below it
- Standalone notes appear at the end

## Event Types

| Event | Triggered by |
|---|---|
| `CREATED` | `graph objects create`, agent tool `create-object` |
| `UPDATED` | `graph objects update` |
| `DELETED` | `graph objects delete` |
| `RESTORED` | object restore |
| `RELATED` | `graph relationships create` |
| `BATCH` | `graph objects bulk-create`, `graph relationships bulk-create` |
| `MERGE` | `graph branches merge` |
| `NOTE` | `memory journal note` |

## MCP Tools

When operating via MCP, use these tools:

- **`journal-list`** — list recent journal entries; accepts `since` (e.g. `"7d"`) and `limit`
- **`journal-add-note`** — add a note; accepts `body` (required) and optional `journal_id` to attach to an entry

## API

```
GET  /api/graph/journal?since=7d&limit=100   # list entries + standalone notes
POST /api/graph/journal/notes                # add a note
```

## Workflow Tips

- **After a bulk population session**: run `memory journal list --since 1h` to verify what was created
- **Before continuing a session**: read the journal to understand the current state without re-querying the graph
- **Annotating decisions**: use `memory journal note` to record why a change was made; future agents will see it in the journal
- **Attaching context to a change**: use `--entry <id>` to link a note to the specific journal entry for a mutation
