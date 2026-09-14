## Why

Skills are stored in `kb.skills` and exposed through REST, MCP tools, the CLI, and blueprints — but there is **no authoritative spec for the management surface**. The existing `agent-skill-catalog` spec covers only *injection* (how a skill catalog reaches an agent's tool pipeline). The write/read/lifecycle side is undocumented, has real gaps, and cannot be tested against a contract:

1. **Provenance is absent.** Imported skills (CLI `skills import`, blueprint `skills/` dirs) carry no record of *where they came from*, their *license*, *version*, or *content hash*. This makes a future skill repository/marketplace (see `design.md`) impossible to back safely — the DB stores content with no way to trace origin or detect drift.
2. **Embedding is inconsistent.** The REST path generates a description embedding at write time; the MCP `skill-create` path does not (`embedding=nil`), so MCP-created skills silently never surface in semantic retrieval when a project exceeds the 50-skill threshold.
3. **`AutoLoadSkills` is implemented but unspec'd.** The prefix convention (`{agentName}.{suffix}` → auto-load) has no contract, no tests, and no documentation.
4. **Validation is implicit.** Name pattern, description/content requirements, and content-size limits are enforced only as side effects of handlers — never stated as requirements.

This change adds a canonical `skill-management` capability spec so the full lifecycle (create, read, update, delete, import, scope, provenance, auto-load) has an authoritative, testable contract.

## What Changes

- **New capability spec** `skill-management` — data model, name/content validation, scope rules, REST + MCP write/read surface, provenance metadata, description-embedding on *every* write path, and `AutoLoadSkills` semantics.
- **New: provenance metadata** — extend the skill `metadata` (jsonb) with `source`, `license`, `version`, `content_hash`, `source_url`, `origin_id`. Content hash computed server-side at write for idempotent re-import and drift detection.
- **Fix: embedding on MCP write path** — `skill-create`/`skill-update` populate the description embedding like the REST path does.
- **Codify `AutoLoadSkills`** — spec the `{agentName}.{suffix}` prefix match, merge-with-explicit-`skills` dedup, and injection gating.
- **Validation as requirements** — name slug pattern + length, non-empty description/content, content-size cap.

## Capabilities

### New Capabilities

- `skill-management`: Full lifecycle contract for skills — storage model, validation, scoping (global/org/project + precedence), REST CRUD, MCP `skill-*` tools, provenance/license/version/hash tracking, description-embedding on all write paths, and `AutoLoadSkills` attachment semantics.

### Modified Capabilities

_(none — `agent-skill-catalog` continues to own the injection/catalog-filtering side and is unchanged.)_

## Impact

- `apps/server/domain/skills/entity.go` — extend `SkillMetadata` with provenance fields (jsonb, additive).
- `apps/server/domain/skills/store.go` — compute/persist `content_hash` on create/update; store provenance on import.
- `apps/server/domain/skills/handler.go` — pass provenance into create/update; MCP write path emits description embedding.
- `apps/server/domain/mcp/skills_tools.go` — `skill-create`/`skill-update` populate embedding + provenance; align validation with spec.
- `apps/server/domain/agents/executor.go` — verify `AutoLoadSkills` prefix matching matches spec (no behavioral change expected).
- `apps/cli/internal/cmd/skills.go` + `install_skills.go` + `blueprints/applier.go` — populate provenance (`source`, `license`, `version`, `source_url`) when importing.
- No new tables — metadata stays jsonb; additive-only, no breaking API change.

## Non-Goals (explicitly out of scope)

- **Background learning loop** — agents autonomously *creating* skills from experience (nudge counter, background review, curator, usage telemetry, dedup/consolidation). Deferred to a future change; this spec only establishes the storage/write/import foundation it will build on.
- **Marketplace sync** — pulling skills from external registries (skills.sh, hermes skills-index, cline catalog, `.well-known/skills/index.json`). Deferred. Provenance fields here are the enabling prerequisite; no network fetch in this change.
