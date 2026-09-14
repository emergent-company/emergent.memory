# Memory — Emergent Memory MCP Guide

## Connection

Memory connects to Emergent Memory via MCP. Credentials in `.env`:
```
MEMORY_URL=http://localhost:5300
MEMORY_TOKEN=emt_<redacted — real value lives in .env, never commit>
MEMORY_PROJECT_ID=7d018080-16f7-45cc-b498-9233fb0d7ab0
```

MCP endpoint: `http://localhost:5300/api/mcp`

**91 tools available** (scope-filtered from 117 total). All key memory tools present.

## REST API (gateway)

The gateway calls Memory's HTTP API directly (not MCP) for agents/chat/settings/etc. Auth: `Authorization: Bearer <token>`.

- **Source**: full clone `/root/emergent.memory` (where backend edits go); vendored read-only copy `/root/alfred/.slim/clonedeps/repos/emergent-company__emergent.memory` (for reading internals). Memory is a `go.work` monorepo — build/test from `apps/server`.
- **`X-Project-ID` header required** for non-`emt_*` (Zitadel user-session) calls: the user token is user-scoped, not project-scoped, and `GetProjectID` errors without it. `emt_*` tokens are project-bound and skip the header.
- **Route split**: some routes under `/api/v1/...` (e.g. backups: `/api/v1/organizations/:orgId/...`), most under `/api/...`, some under `/api/superadmin/...`. Several endpoints (data-sources, some admin) are ABSENT from repo-root `openapi.yaml` — grep `apps/server/**/routes.go` / the full swagger / SDK client instead.
- **Known gaps**: restore endpoint is a hardcoded 501 (unavailable); data-source OAuth (`gmail_oauth`/`google_drive`) has no callback endpoint.

## MCP servers (gateway UI)

External tool providers are managed in the gateway at **Settings → MCP Servers**
(`/settings/mcp-servers`): register (name + transport `stdio`/`sse`/`http` + URL/headers for
remote, command/args/env for stdio), edit, delete; per-row **Sync** discovers tools via
`tools/list` and **Inspect** probes the connection; each cached tool has an enable toggle.
Builtin servers are read-only.

- Agent tool whitelists store the **bare tool name** memory's tools API returns
  (`web_search_exa`). Memory keys external pool tools as `<slugified server>_<tool>` and
  resolves bare names back to those keys; tool calls route to the raw server name.
- **Requires memory ≥ the release containing PRs #406/#410.** Older builds silently drop
  whitelisted tools — the model only ever sees `set_session_title`.
- Dev test targets (project `bf10f0e4…`): `exa` (keyless), `firecrawl` (keyless sync/list;
  key needed for calls). See `INFRASTRUCTURE.md`.

### Relay nodes (external MCP tools)

Machines running the connector app show up as **relay nodes** on
**Settings → MCP nodes** (`/settings/mcp-nodes`). The page lists live sessions
**and** previously-seen nodes: live rows show a green `Connected` badge, offline
rows a gray `Disconnected` badge + `last seen <rel>` (tools come from the cached
snapshot). A confirmed **Remove** deletes a stored node; a still-connected host
reappears on the next load. Presence/last-seen is reconciled when the page loads
(no background poller yet — see `docs/tasks/mac-connector-presence-ttl.md`).

## LLM Providers (project-level)

Two providers configured (as of 2026-09-07):

| provider | base_url | generative model | embedding model |
|---|---|---|---|
| `openai` (OpenAI-compatible → LiteLLM) | `http://litellm:4000/v1` | `deepseek-v4-flash` | — |
| `google` (official API) | — | `gemini-3.1-flash-lite-preview` | `gemini-embedding-2-preview` |

API keys are encrypted server-side; the `openai` key matches `.env` `LLM_API_KEY`.

Two config layers, both editable in **Settings → Providers**:

- **Default models** (`kb.project_model_config`, "Default models" panel) — the project default:
  generative `openai/deepseek-v4-flash`, embedding `google/gemini-embedding-001`. Values are
  **credential-prefixed** (`provider/model`); the dropdowns are grouped by configured (credential)
  provider.
- **Provider fallback** (per-provider config, "Provider configuration" → Edit) — the
  generative/embedding model used when the project default is unset. Values are prefixed
  (`provider/model`); memory strips the prefix when resolving (memory PR #383).

Backs the `remember` extraction pipeline (generate + embed). Live credential test passes both.

**Model catalog**: since memory PR #377, the OpenAI-compatible provider syncs its **full**
model list from `GET {base_url}/models` into `provider_supported_models` (feeding the gateway's
Providers rates panel + default-model dropdowns). The sync runs only on provider-config upsert —
to refresh after changing LiteLLM's model list, re-save the provider (Settings → Providers →
Edit → Save); there is no startup/cron re-sync (see `docs/tasks/provider-model-catalog-resync.md`).

**Pricing & usage**: retail rates live in `kb.provider_pricing`, synced from an embedded
static list (the external `model-pricing` registry 404s — see
`docs/tasks/restore-model-pricing-registry.md`). Embedding retail prices were added in memory
PR #405 (`gemini-embedding-001` $0.15/1M input, `gemini-embedding-2` $0.20). Since PR #408,
embedding calls through an OpenAI-compatible/LiteLLM provider emit `llm_usage_events`
(`operation=embed`, provider `openai`) and appear in usage/budget; the rate rows only land
after the memory pricing sync runs (startup or 02:00 cron).

## Installed Schemas

Two blueprint packs combined: **personal-memory** (Diane) + **agent-notes** (OpenCode plugin).

### Personal Types (9 types, 8 relationships)

| Type | Key Properties | Used For |
|------|---------------|----------|
| `person` | first_name, last_name, relationship, emails, phones, organization, birthday, notes | People in user's life |
| `task` | title, status, priority, due_date, source, tags | To-dos from conversations/emails |
| `project` | name, status, start_date, target_date, tags | Collections of related tasks |
| `calendar_event` | event_id, summary, start, end, attendees, location | Synced calendar events |
| `financial_transaction` | source, amount, payee, category, date | Budget transactions |
| `contact` | source, first_name, last_name, emails, phones | Apple Contacts synced |
| `place` | place_id, name, address, lat, lng, rating | Google Places |
| `note` | title, content, source, tags | Free-form notes |
| `habit` | name, frequency, streak_current, status | Habits tracking |

### Relationships

| Name | Source → Target |
|------|----------------|
| `assigned_to` | task → person |
| `belongs_to_project` | task → project |
| `involves_person` | calendar_event → person |
| `located_at` | calendar_event → place |
| `related_to_contact` | person → contact |
| `has_transaction` | project → financial_transaction |
| `references_file` | note → file |
| `triggered_task` | calendar_event → task |

### Agent Notes (2 types, 3 relationships)

| Type | Properties | Used For |
|------|-----------|----------|
| `Note` | content, category, source, confidence, tier, use_count | Observations, preferences, corrections, facts |
| `NoteCluster` | summary, confidence | Grouped semantically-related notes |

| Relationship | Source → Target |
|-------------|----------------|
| `ANNOTATES` | Note → any entity |
| `BELONGS_TO_CLUSTER` | Note → NoteCluster |
| `SUPERSEDES` | Note → Note |

## MCP Tools

### Knowledge Graph

| Tool | Purpose | Example |
|------|---------|---------|
| `entity-create` | Create entities (array form) | `entity-create(entities=[{type:"person", key:"mom", properties:{first_name:"Anna"}}])` |
| `entity-query` | List entities by type | `entity-query(type_name=task, filters={status:"pending"})` |
| `entity-update` | Modify properties | `entity-update(entity_id=..., properties={status:"done"})` |
| `entity-delete` | Soft-delete (restorable) | `entity-delete(entity_id=...)` |
| `entity-search` | Search by text | `entity-search(query="Mom birthday")` |

### Search

| Tool | Purpose | Example |
|------|---------|---------|
| `search-hybrid` | Full-text + semantic search | `search-hybrid(query="flight to London", types=["calendar_event"])` |
| `search-knowledge` | NL question → LLM-generated answer | `search-knowledge(question="What tasks are overdue?")` |

### Agent Memory (Notes)

> **Migration note (2026-08):** `save_note`/`recall_notes`/`get_note`/`manage_notes` are **removed** from the memory service — do not use. (`save_note` had a data-corruption dedup bug; `recall_notes` only searched type `Note`, hiding other entity types.) Write notes/preferences via `entity-create` and recall via `search-hybrid` (searches **all** object types semantically); fetch specific entities via `entity-query`; list notes via `entity-query(type_name="Note")`.

| Tool | Purpose | Example |
|------|---------|---------|
| ~~`save_note`~~ | **Removed** — write via `entity-create` | `entity-create(entities=[{type:"Note", key:"coffee-8am", properties:{content:"User prefers coffee at 8am", category:"preference"}}])` |
| ~~`recall_notes`~~ | **Removed** — recall via `search-hybrid` | `search-hybrid(query="user preferences morning routine")` |
| ~~`get_note`~~ | **Removed** — fetch via `entity-query` | `entity-query(ids=["<entity-id>"])` |
| ~~`manage_notes`~~ | **Removed** — list via `entity-query`, update via `entity-update` | `entity-query(type_name="Note", limit=100)` |
| `project-briefing` | Project info + core notes + relevant notes | `project-briefing(query="morning routine")` |

### Remember / Forget (MCP Tools)

| Tool | Purpose | Example |
|------|---------|---------|
| `remember` | **Heavyweight ingest.** Text → document → classify → AI extract → structured entities + relationships. Uses installed schemas. | `remember(message="Meeting with John about Q3 budget: agreed on $500k allocation, deadline September 30", schema_policy=reuse_only)` |
| `forget` | **Soft-delete by NL query.** Agent finds matching entities and removes them. Reversible via entity-restore. | `forget(message="old Project Alpha details from 2025", strategy=confirm, dry_run=true)` |
| `entity-create` | Create typed entities | `entity-create(type=person, key=mom, properties={...})` |
| `entity-delete` | Soft-delete single entity | `entity-delete(entity_id=...)` |

### Remember (Heavyweight Pipeline)

```
remember(message="text", schema_policy=reuse_only, mode=sync)
  ↓
Server persists text as document
  ↓
Pre-classification against installed schemas
  ↓
AI extraction: LLM creates entities matching schema types
  ↓
Graph: typed entities + relationships created
```

**When to use `remember`:**
- Ingesting meeting notes → people, decisions, action items
- Processing documents → structured knowledge
- Capturing complex decisions with context
- Bulk text ingestion

**When to use `entity-create` instead:**
- Single observation, preference, or fact (write as a `Note` entity: `entity-create(entities=[{type:"Note", key:..., properties:{content, category, source}}])`)
- No structured entity extraction needed
- Fast, instant, no LLM cost

### Forget

## Common Patterns

### Remember a fact about a person
```
entity-create(entities=[{type:"Note", key:"mom-birthday", properties:{content:"Mom's birthday is March 15", category:"fact"}}])
entity-create(entities=[{type:"person", key:"mom", properties:{first_name:"Anna", birthday:"03-15", relationship:"family"}}])
relationship-create(relationships=[{type:"related_to_contact", source_id:"<person_id>", target_id:"<contact_id>"}])
```

### Remember a preference
```
entity-create(entities=[{type:"Note", key:"afternoon-meetings", properties:{content:"User prefers meetings in the afternoon", category:"preference", source:"explicit"}}])
```

### Remember a task to do
```
entity-create(entities=[{type:"task", key:"call-dentist", properties:{title:"Call dentist", status:"pending", priority:"high", tags:["health","phone"]}}])
```

### Recall context for a topic
```
search-hybrid(query="dentist health appointments")   # searches all entity types semantically
entity-query(type_name=task, filters={status:"pending"})
```

### Forget outdated info
```
entity-delete(entity_id=<old-task>)  # soft-delete, restorable
entity-query(type_name=Note, filters={category:"fact"}, limit=50)  # find the stale note first, then entity-delete it
```

### Find what's happening today
```
search-hybrid(query=today, types=["calendar_event"])
entity-query(type_name=task, filters={status:"pending"}, sort_by="created_at")  # sort_by ∈ created_at|updated_at|name
```

### Morning briefing
```
project-briefing(query="today schedule tasks priorities")
search-hybrid(query="important preferences today")
```

## Note Categories

| Category | When to Use |
|----------|------------|
| `preference` | User likes/dislikes: "Prefers coffee before 9am" |
| `pattern` | Recurring behavior: "Always checks email first thing" |
| `correction` | Fixing a mistake: "Mom's birthday is 03-15 not 03-16" |
| `fact` | Verifiable info: "Passport expires 2027" |
| `instruction` | Standing orders: "Always confirm appointments 24h before" |
| `convention` | Agreed rules: "Family dinner every Sunday" |

## Confidence

| Source | Confidence | Meaning |
|--------|-----------|---------|
| `explicit` | 1.0 | User directly stated it |
| `corrected` | 0.9 | User corrected a previous error |
| `inferred` | 0.7 | Memory noticed a pattern |

Confidence decays over time if notes are never recalled. Frequently-recalled notes gain confidence.

## Privacy

Wrap sensitive content in `<private>...</private>` tags — they are automatically stripped before storage:
```
entity-create(entities=[{type:"Note", key:"q4-budget-meeting", properties:{content:"Meeting notes: discussed Q4 budget. <private>Client: Acme Corp, deal size: $2M</private>. Agreed on timeline."}}])
```
Stored as: "Meeting notes: discussed Q4 budget. [redacted]. Agreed on timeline."
