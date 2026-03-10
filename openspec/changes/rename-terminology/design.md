## Context

The platform has three domain concepts whose names have accumulated confusion over time:

1. **TemplatePack** — a versioned, reusable bundle of object/relationship type definitions. The name is too close to "Blueprint" (a different mechanism for project scaffolding) and too vague about what the bundle actually contains. Users and developers must learn the term; it does not self-describe.

2. **TypeRegistry** — the per-project installed catalog of active type definitions derived from installed TemplatePacks and custom types. "Registry" is fine but "Type" is generic; combined with "ProjectObjectTypeRegistry" (the DB table name) it becomes unwieldy. The concept is the *schema* of a project — what shapes knowledge objects can take.

3. **AgentWorkspace** — an isolated compute VM/container where AI agents execute code. "Workspace" typically means a UI work area (files, tabs, panels). The mismatch is low-friction today because the concept is not yet exposed in the UI, but as the sandbox surface is intentionally expanded, the collision will confuse users.

All three renames are pure identifier changes. No behavior, logic, or data structure changes. The implementation is mechanical but has wide surface area: 2 Go package directories per rename, ~35 Go files, ~10 UI files, 2 CLI files, 2 DB migrations, ~13 docs files.

## Goals / Non-Goals

**Goals:**
- Rename `TemplatePack` → `Schema` / `MemorySchema` throughout all layers (Go, CLI, API, DB, UI, docs)
- Rename `TypeRegistry` → `Schema Registry` / `SchemaRegistry` throughout all layers
- Rename `AgentWorkspace` → `Sandbox` / `AgentSandbox` throughout all layers
- Rename `ENABLE_AGENT_WORKSPACES` env var → `ENABLE_AGENT_SANDBOXES`
- Two Goose SQL migrations covering all DB-level renames
- Hard cut — no backward-compat aliases or redirects

**Non-Goals:**
- No behavior changes; this is naming only
- No API versioning strategy — old paths simply stop existing
- No DB schema prefix rename (`kb.` stays as-is)
- No rename of `Skill`, `Task`, `Branch`, `Document`, `Chunk`, or other domain terms
- No frontend changes for TypeRegistry (user-facing label already says "Objects") or AgentSandbox (not yet in UI)

## Decisions

### Decision 1: Go internal type name `MemorySchema` vs `Schema`

**Decision:** Use `MemorySchema` as the Go struct/type name, `schemas` as the package name, and `schema`/`schemas` as the user-facing term (CLI, API, UI).

**Rationale:** `Schema` is too generic in Go — it collides with JSON Schema types, DB schema references, and the standard library. `MemorySchema` scopes the name to this platform and avoids ambiguity in any file that imports both `MemorySchema` and, say, a JSON schema type. Externally (CLI help text, API docs, UI labels) we just say "schema" since there is no ambiguity in user-facing contexts.

**Alternative considered:** `KnowledgeSchema` — more descriptive but verbose. `TypeBundle` — accurate but too implementation-focused.

---

### Decision 2: TypeRegistry → `SchemaRegistry` (not `ProjectSchema`)

**Decision:** Use `SchemaRegistry` as the Go package/type name and `/api/schema-registry` as the API prefix.

**Rationale:** "Schema Registry" is a well-known term (Confluent, Kafka ecosystem) that precisely describes a catalog of type schemas. It distinguishes the *catalog* from the *schema content*: a Schema is the bundle of definitions, a Schema Registry is the per-project installed index of those definitions. This avoids confusion between `MemorySchema` (the bundle) and `SchemaRegistry` (the installed catalog).

**Alternative considered:** `ProjectSchema` — cleaner but loses the "registry/catalog" connotation that makes the read/write distinction clear.

---

### Decision 3: AgentWorkspace → `AgentSandbox` (not just `Sandbox`)

**Decision:** Use `AgentSandbox` as the Go struct name, `sandbox` as the package name, `/api/v1/agent/sandboxes` as the API prefix.

**Rationale:** "Sandbox" alone is understood in developer contexts (isolated execution environment) and directly maps to the product capability. Keeping the `Agent` prefix on the Go struct (`AgentSandbox`) avoids ambiguity in packages that deal with multiple execution contexts. The URL path `/api/v1/agent/sandboxes` preserves the existing `/agent/` grouping pattern.

**Alternative considered:** `ExecutionEnvironment` — too verbose. `Container` — technically accurate but implies Docker specifically, which is not the only provider.

---

### Decision 4: Single migration for Schema + SchemaRegistry, separate migration for Sandbox

**Decision:** Combine TemplatePack and TypeRegistry DB renames into one migration file. Put AgentWorkspace renames in a separate migration.

**Rationale:** The `project_object_type_registry` table has a `template_pack_id` column touched by both Rename 1 and Rename 2. Processing them in the same migration avoids a double-rename sequence and keeps the column rename atomic. The AgentWorkspace rename is independent (different tables, different domain) and includes a data UPDATE (`container_type` value change) that is safer to isolate.

---

### Decision 5: `ENABLE_AGENT_WORKSPACES` env var renamed to `ENABLE_AGENT_SANDBOXES`

**Decision:** Rename the env var. Document the breaking change in migration notes.

**Rationale:** Leaving the env var as `ENABLE_AGENT_WORKSPACES` while everything else is `sandbox` creates a confusing inconsistency at the operator level. The env var is only set in deployment configs (`.env` files, Compose, K8s), not in application code constants — a grep-and-replace in deployment configs is straightforward. The migration plan calls this out explicitly.

**Alternative considered:** Keep the old env var as an alias — rejected because hard cut is the stated approach and it adds complexity to the config loading code.

---

### Decision 6: `workspace_config` column on `kb.agent_definitions` renamed to `sandbox_config`

**Decision:** Rename the JSONB column `workspace_config` → `sandbox_config` on `kb.agent_definitions`.

**Rationale:** Consistency. If the Go field is `AgentSandboxConfig`, the DB column should be `sandbox_config`. The column stores the serialized config struct — the names must align for Bun ORM tag clarity.

## Risks / Trade-offs

**[Risk] Wide blast radius across two repos** → Mitigate by executing renames in order: migrations first, then Go packages (with `go build` check after each), then CLI, then UI. Keep a checklist.

**[Risk] Missed reference causes runtime 404 / nil pointer** → Mitigate with `go build ./...` after each package rename and `pnpm run build` in the UI repo before merging.

**[Risk] Operator env var `ENABLE_AGENT_WORKSPACES` not updated on existing deployments** → Mitigate by logging a startup warning if the old env var is detected (check both names during a transition period if desired, or simply document clearly in release notes).

**[Risk] DB migration applied while server is running, causing FK violations mid-request** → Mitigate by stopping server before migration on the upgrade path (standard practice for this platform already).

**[Risk] Goose `template_pack_studio_sessions.pack_id` FK reference points to old table name** → Handled in the migration: rename the table before renaming the FK to avoid temporary broken state. Migration must be written in the correct order (rename referenced table before renaming FK).

**[Trade-off] Hard cut means any external integrations calling `/api/template-packs` break immediately** → Accepted. There are no documented external API consumers and the rename is happening before external SDK stabilization.

## Migration Plan

### Execution order

1. **Write and test DB migrations locally**
   - Migration A: `kb.graph_template_packs` → `kb.graph_schemas`, `kb.project_template_packs` → `kb.project_schemas`, `kb.template_pack_studio_*` → `kb.schema_studio_*`, `kb.project_object_type_registry` → `kb.project_object_schema_registry`, column `template_pack_id` → `schema_id` everywhere
   - Migration B: `kb.agent_workspaces` → `kb.agent_sandboxes`, `kb.workspace_images` → `kb.sandbox_images`, `kb.agent_definitions.workspace_config` → `sandbox_config`, data UPDATE for `container_type`

2. **Rename Go packages** in order: `templatepacks` → `schemas`, `typeregistry` → `schemaregistry`, `workspace` → `sandbox`, `workspaceimages` → `sandboximages`; update all cross-package imports; verify `go build ./...` passes

3. **Update CLI** (`tools/cli/`): rename file, update command strings, update SDK import

4. **Update UI** (`emergent.memory.ui`): update API URLs, interface names, user-visible strings

5. **Update test suite** (`tools/opencode-test-suite/`): rename assert helpers, update CLI invocations

6. **Regenerate Swagger** (`swag init` or equivalent)

7. **Update docs**: rename and update ~13 markdown files

8. **Update `AGENTS.md`** domain layout list

### Rollback strategy

All DB changes are table/column renames with no data loss — rollback is a reverse rename. The Goose `down` sections of both migrations must implement the reverse renames. Code rollback is a git revert of the PR.

### Deployment checklist

- [ ] Update `.env` / deployment config: `ENABLE_AGENT_WORKSPACES` → `ENABLE_AGENT_SANDBOXES`
- [ ] Stop server before applying migrations
- [ ] Run `goose up` (both migrations)
- [ ] Start server
- [ ] Verify `/health` and `/ready`

## Open Questions

None — all decisions have been made in conversation with the product owner. Hard cut, no aliases, all three renames in one PR.
