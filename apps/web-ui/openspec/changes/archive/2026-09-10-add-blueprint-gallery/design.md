## Context

Alfred's gateway (`gateway/`) is an echo v4 app that talks to Emergent Memory over plain REST via a
hand-rolled `MemoryClient` (`memory.go`, `do`/`doH`/`successEnvelope` pattern), behind the
`MemoryBackend` interface (`backend.go`). UI is a-h/templ + go-daisy + daisyUI + htmx. The existing
MCP-server CRUD (`extras.go` + `extras_handlers.go`) is the closest structural analogue to a
blueprint gallery and is the template this design follows.

Emergent Memory already implements the full blueprint lifecycle on its HTTP API
(`/api/schemas...`): global registry CRUD, per-project `available`/`installed`/`compiled-types`
lists, `assign` (install, with merge/conflict/dry-run), per-assignment toggle/remove, and migration.
Schema extension is dynamic (per-project cache, invalidated on assign). Alfred ships two local pack
directories under `/root/alfred/blueprints/` (`personal-memory`, `agent-notes`) that are installed
out-of-band today. Motivation and scope are in proposal.md.

## Goals / Non-Goals

**Goals:**

- Expose list/install/toggle/remove/inspect over a small `/api/blueprints*` surface.
- Drive install through memory's HTTP `assign` path so merge/conflict/dry-run semantics are preserved.
- Surface the bundled `blueprints/` directory packs as installable "available" items.
- Keep the gateway stateless: read local pack files on demand, never cache schema state locally.

**Non-Goals:**

- No pack authoring/editing UI or schema YAML editor.
- No GitHub/remote catalog browsing (deferred; memory CLI already supports `FetchGitHubRepo`).
- No migration UI (preview/execute/rollback) — deferred; requires `schema:migrate` scope.
- No changes to the iOS client.

## Decisions

### 1. Use memory's HTTP schema API, not the MCP schema tools

**Why:** HTTP `POST /assign` returns `{installed_types, skipped_types, merged_types, conflicts,
dry_run, migration_preview}` and runs the merge/conflict gate; the MCP `schema-assign` tool does a
direct SQL upsert that bypasses merge/conflict logic. Removal via MCP `schema-uninstall` has a
parameter bug (executor reads `schema_id` instead of the documented `assignment_id`).

**Alternative considered:** Use MCP tools (`schema-list-installed`, `schema-assign`,
`schema-uninstall`) for symmetry with the existing `ListMemories` MCP JSON-RPC path. Rejected: weaker
install semantics and the uninstall bug make it riskier than plain REST.

### 2. Two-tier "available" catalog: registry + bundled

**Why:** memory's `GET /available` only lists packs already in the registry but not installed, and
only project-visible ones. Alfred's bundled `blueprints/` directory packs are not in the registry
until someone creates them, so they would never appear in `available`.

**Decision:** `GET /api/blueprints/available` merges two sources: (a) memory's registry `available`
list (`source: "registry"`), and (b) the bundled `blueprints/` packs (`source: "bundled"`), with
bundled packs filtered against the installed list so an installed pack never shows installable.
The bundled packs are **embedded in the gateway binary** (go:embed, moved to `gateway/blueprints/`)
so the gallery is self-contained in any deployment; `BLUEPRINTS_DIR` is an override for reading
extra packs from disk (empty default = embedded). Bundled packs carry only name/version/
description/author parsed from their `project.yaml` + pack/schema YAML.

**Alternative considered:** Pre-register all bundled packs into memory at deploy time, then rely on
`/available` alone. Rejected: couples deploy ordering to the feature and pollutes the global registry
with packs the user may never install.

### 3. Single install endpoint handles both sources

**Why:** a bundled pack install needs two memory calls (register `POST /api/schemas` → then `assign`),
while a registry pack needs only `assign`. Hiding this behind one endpoint keeps the client simple.

**Decision:** `POST /api/blueprints/install` accepts either `{schemaId}` (registry) or
`{source:"bundled", name}` (read local files, `POST /api/schemas` to register, then `POST /assign`).
Both return the normalized assignment result.

**Alternative considered:** separate `POST /api/blueprints/register` + `POST /api/blueprints/install`.
Rejected: two-step client flow with more failure states; the bundled path is the only one that needs
register, and it is always followed by assign in the gallery UX.

### 4. Soft-remove via `DELETE /assignments/:id`

**Why:** memory's `DELETE /api/schemas/projects/:pid/assignments/:id` is a soft delete
(`active=false, removed_at=now`), keeping type rows recoverable and history intact. `schema-delete`
(global registry removal) would refuse while assigned and is not what "remove from my project" means.

**Decision:** the gateway exposes `DELETE /api/blueprints/assignments/:id` mapped 1:1 to memory's
assignment delete; toggle maps to `PATCH .../assignments/:id` with `{active}`.

**Alternative considered:** expose global `schema-delete` too. Rejected for MVP — destructive and
outside the "manage my project's schema" scope.

### 5. Follow the existing 3-layer gateway pattern

**Why:** consistency and testability. New structs + `MemoryClient` methods in `memory.go`; interface
additions in `backend.go`; thin echo handlers in a new `blueprints_handlers.go`; unit tests against
the existing `fakeMemory`/`dumpFake` fixtures.

**Decision:** add methods `ListInstalledSchemas`, `ListAvailableSchemas` (memory side), `ListBundledBlueprints`
(local fs), `GetCompiledTypes`, `AssignSchema`, `RegisterSchema`, `ToggleSchemaAssignment`,
`RemoveSchemaAssignment`. Error mapping mirrors `agentMemoryError` (404 not_found → 404, else 502).

**Alternative considered:** a standalone `blueprintclient` package separate from `MemoryClient`.
Rejected: the bundled-pack fs read is the only non-memory concern, and it is small enough to live in
the same file.

### 6. Token scope expansion is a deployment change, not code

**Why:** install/remove require `schema:write` (and `schema:migrate` for migrations). Spec 02 records
Alfred's token as `schema:read` only.

**Decision:** document the required scopes in the tasks and spec; treat as an ops step. The gateway
code assumes the token has the scopes and surfaces 403/502 as it does today.

**Alternative considered:** feature-flag install behind a scope probe. Rejected: over-engineering for a
single-owner setup; the 502 path already covers the failure.

## Risks / Trade-offs

- [Install conflicts overwrite types] → Surface `conflicts`/`merged_types` from `assign` verbatim in
  the response and UI; never force-merge implicitly. Memory's additive default prevents silent
  overwrite.
- [Bundled pack read is filesystem-dependent] → Bundled packs are embedded in the binary by default,
  removing the runtime filesystem dependency. `BLUEPRINTS_DIR` (empty default = embedded) lets tests
  and advanced users point at a temp dir or external pack directory; the fs read stays small and
  behind a seam for unit tests.
- [Registry "available" list is project-scoped] → If a globally-created pack is not visible to the
  project, it won't appear; acceptable for single-project Alfred, documented as a limitation.
- [Memory token lacks `schema:write`] → Install/toggle/remove return 502 until scopes are added; the
  read-only parts (list/compiled-types) still work. Flagged in tasks as a required ops step.

## Migration Plan

No data migration. New routes are additive; the existing `/agents`, `/chat`, `/api/mcp-servers`
surfaces are untouched. The only external step is expanding the memory `emt_*` token to include
`schema:write` (and `schema:migrate` when migrations ship). Rollback = revert the gateway binary; no
state is written locally.

## Open Questions

- Whether to later add a GitHub-URL catalog and pack authoring (deferred; would add `schema:migrate`
  scope and a `schema-create` UI). Does not affect this change's spec or tasks.
