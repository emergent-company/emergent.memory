## Context

`kb.projects` has a `kb_purpose text` column that was intended to describe the knowledge base. It is used in two distinct ways:

1. **Discovery jobs** — `discoveryjobs` reads it at job creation time, snapshots it into `kb.discovery_jobs.kb_purpose`, and injects it into LLM prompts for type/relationship discovery.
2. **UI editor** — `KBPurposeEditor` component lets admins edit it; it lives on the Auto-Extraction settings page.

Neither use gives it a proper identity as a first-class "project description". There is no MCP tool for agents to read it, no structured default, and no clear ownership. The field is also limited to 1000 characters in the UI, which is too short for a meaningful project document.

This design replaces `kb_purpose` with `project_info` — a richer, uncapped markdown field — and wires it into the MCP tool system so agents can query it on demand.

## Goals / Non-Goals

**Goals:**
- Single `project_info` text column on `kb.projects` replacing `kb_purpose`
- Full API exposure: GET/PATCH `/api/projects/:id` and access-tree response
- Built-in MCP tool `get_project_info` callable by any agent in the project
- `ProjectInfoEditor` React component with a memory-oriented default template
- Discovery jobs migrated to use `project_info` (same data, new column name)
- Clean removal of `kb_purpose` from all layers in a single migration

**Non-Goals:**
- Auto-injecting `project_info` into every agent system prompt (call-on-demand only)
- Chunking or embedding `project_info` as a document
- A `set_project_info` write tool — edits are admin-only via UI/API
- Versioning or history of changes to `project_info`
- Removing the `kb_purpose` snapshot column from `kb.discovery_jobs` (historical data, keep as-is)

## Decisions

### 1. Replace `kb_purpose` rather than add alongside it

**Decision:** Drop `kb_purpose` from `kb.projects` in the same migration that adds `project_info`. Copy the value during migration with `UPDATE kb.projects SET project_info = kb_purpose`.

**Rationale:** Two overlapping "purpose" fields would create confusion about which one agents and admins should use. A single field with a clear name is easier to reason about. Since `kb_purpose` is only consumed in two places (discovery jobs service and `KBPurposeEditor`), the migration surface is small.

**Alternative considered:** Keep `kb_purpose` and add `project_info` additively. Rejected — adds a naming problem that would need to be cleaned up later, and the migration cost is the same either way.

### 2. Store as plain text column, not a separate table or document record

**Decision:** `project_info text` column on `kb.projects`.

**Rationale:** Same pattern as `kb_purpose` and `chat_prompt_template`. No chunking, embedding, or ingestion pipeline is needed — this is configuration, not knowledge graph content. A separate table would add joins and complexity without benefit.

**Alternative considered:** Store as a `kb.documents` record with `source_type = "project_info"`. Rejected — would trigger chunking/embedding pipelines, create noise in document lists, and complicate reads (needs a query instead of a column projection).

### 3. `get_project_info` as a call-on-demand MCP tool, not auto-injected

**Decision:** Agents call `get_project_info` when they need project context. It is not prepended to every system prompt.

**Rationale:** Auto-injection would add tokens to every single LLM call, even for agents that don't need project context (e.g. graph search agents). The existing builtin tool pattern (`query_entities`, `list_entity_types`) is call-on-demand — `get_project_info` follows the same convention. Agents that need it can call it in their first step; orchestrator agents can include it in spawned-agent context.

**Alternative considered:** Prepend to all agent system prompts. Rejected — token cost scales with every LLM call across all agents; not all agents benefit from project context.

### 4. `get_project_info` reads directly via `bun.IDB` in `mcp.Service`

**Decision:** Implement `executeGetProjectInfo` in `mcp/service.go` as a raw `s.db.NewSelect()` query against `kb.projects`. No new dependency on `projects.Service` or `projects.Repository`.

**Rationale:** `mcp.Service` already has `bun.IDB` and issues raw queries for other tools (e.g. `executeListEntityTypes`, `executeQueryEntities`). Adding a projects-domain dependency would introduce an import path and potential fx cycle. A single-column SELECT is trivial.

**Alternative considered:** Inject `projects.Repository` into `mcp.Service`. Rejected — adds an fx dependency and import cycle risk for a one-liner query.

### 5. Default template pre-populated in the UI editor

**Decision:** When `project_info` is null or empty, `ProjectInfoEditor` pre-fills the textarea with a memory-oriented markdown template (visible, editable, not placeholder text). Saving it writes the template content to the DB.

**Rationale:** An empty field gives no guidance. A template with suggested sections helps admins immediately understand what to write and ensures the field has useful content from the start. Pre-filled (not placeholder) means the template is persisted on first save — making it available to agents from the very beginning.

**Default template:**
```markdown
# About this project

This knowledge base is about...

## Purpose
<!-- What is this knowledge base for? -->

## Audience
<!-- Who uses this knowledge base? -->

## Key Topics
<!-- What subjects and domains does it cover? -->

## What belongs here
<!-- Types of knowledge, documents, and entities that should be captured -->

## What does NOT belong here
<!-- Out of scope topics or entity types -->
```

## Risks / Trade-offs

- **Data loss risk on migration** → Mitigation: Migration does `UPDATE kb.projects SET project_info = kb_purpose` before `ALTER TABLE kb.projects DROP COLUMN kb_purpose`, so no data is lost. The Goose Down migration restores `kb_purpose` from `project_info`.

- **Discovery jobs referencing dropped column** → Mitigation: `discoveryjobs/repository.go` `GetProjectKBPurpose()` and `discoveryjobs/entity.go` are updated to use `project_info` before migration is deployed. The snapshot column `kb.discovery_jobs.kb_purpose` is untouched — historical job records keep their snapshotted value.

- **SDK consumers reading `kb_purpose`** → Mitigation: `pkg/sdk/projects/client.go` field is renamed. Any external callers using the SDK will get a compile error (Go type safety) rather than silent failure. This is intentional — `kb_purpose` was not a stable public API.

- **`EnsureBuiltinServer` caches tool list in memory** → Adding `get_project_info` to `GetToolDefinitions()` is picked up on next server start; the in-memory `builtinRegistered` guard means the DB is updated on first access per project after restart.

## Migration Plan

Single Goose migration `00055_add_project_info.sql`:

```sql
-- +goose Up
ALTER TABLE kb.projects ADD COLUMN project_info text;
UPDATE kb.projects SET project_info = kb_purpose WHERE kb_purpose IS NOT NULL AND kb_purpose != '';
ALTER TABLE kb.projects DROP COLUMN kb_purpose;

-- +goose Down
ALTER TABLE kb.projects ADD COLUMN kb_purpose text;
UPDATE kb.projects SET kb_purpose = project_info WHERE project_info IS NOT NULL AND project_info != '';
ALTER TABLE kb.projects DROP COLUMN project_info;
```

`apps/server/internal/testutil/schema.sql` updated in the same PR to replace `kb_purpose text` with `project_info text` in the `kb.projects` block.

Deploy order: migration runs first (server startup handles this), then the new binary is live. No downtime window needed — the column rename is non-blocking; the old column is dropped only after the new column is populated.

## Open Questions

- None — all decisions are resolved above.
