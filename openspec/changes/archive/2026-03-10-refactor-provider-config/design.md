## Context

Provider config is currently spread across three tables and requires two separate writes (credentials then models) to be fully operational. The resolution path has four outcomes (`project`, `organization`, `environment`, `nil`) and a `policy` enum that adds indirection without benefit. The generative model stored in the DB is silently ignored at call sites because callers pass a hardcoded name that takes precedence. There is no UI for any of this.

## Goals / Non-Goals

**Goals:**
- One table per scope (org, project) with credentials and model selection as a single row
- Resolution: project row present → use it; else org row → use it; else hard error
- Credentials + models written atomically in one API call; save triggers live test and model catalog sync
- DB-stored `generative_model` is the authoritative model name at generative call sites
- Env-var fallback removed from request-context resolution path
- CLI reshaped to match: `provider configure` replaces `set-credentials` + `set-models`

**Non-Goals:**
- UI (no frontend changes)
- Multi-provider fan-out (one active provider per scope remains the model)
- Preserving existing data (tables dropped, reconfigure required)
- Env-var standalone mode for local dev (env vars still work for `pkg/adk` directly; only the request-context resolver drops them)

## Decisions

### Decision 1: Two tables of identical shape, not one polymorphic table

**Chosen:** `kb.org_provider_configs` (FK → `kb.orgs`) and `kb.project_provider_configs` (FK → `kb.projects`).

**Rationale:** A single polymorphic table with a `scope_type` + `scope_id` column cannot have a DB-level FK to two parent tables simultaneously. Losing cascade deletes and FK integrity is a meaningful regression. Two tables with identical column layouts give us everything: proper FKs, cascade deletes, uniform unique constraints, and simple resolution logic (two sequential lookups).

**Alternative considered:** Single table with `scope_type ENUM('org','project')` and a `scope_id UUID`. Rejected: no DB-level referential integrity, no cascade delete without triggers, more complex queries.

### Decision 2: Presence-of-row implies "use this config"; absence means "inherit"

**Chosen:** No policy enum. A `project_provider_configs` row for `(project_id, provider)` means the project has its own credentials. No row means fall through to the org. No org row means hard error.

**Rationale:** The old `policy` enum had three values but only two meaningful states: "use project creds" vs "use org creds". `policy='none'` (fall to env vars) is removed. Implicit inheritance via absence is simpler and more consistent with how most permission systems work.

**Alternative considered:** Keep `policy` enum but with only two values (`project`/`organization`). Rejected: the row's presence already encodes the intent; the enum is redundant.

### Decision 3: Credentials + models in one write; auto-default models if omitted

**Chosen:** `PUT /api/v1/organizations/:orgId/providers/:provider` accepts `{ apiKey, serviceAccountJson, gcpProject, location, generativeModel, embeddingModel }`. If `generativeModel`/`embeddingModel` are omitted, the handler picks the top-ranked model from the synced catalog for each type.

**Rationale:** Eliminates the "forgot to call set-models" failure mode. The user gets a fully operational config from a single command.

**Risk:** Catalog sync happens in the same request. If the provider API is slow, the save endpoint is slow. Mitigated by: short timeout (5s) for catalog sync during save; use static fallback models if catalog fetch fails.

### Decision 4: DB-stored `generative_model` wins at call sites

**Chosen:** In `pkg/adk/model.go` `CreateModelWithName`, the precedence becomes: (1) `cred.GenerativeModel` from resolved config, (2) caller's `modelName` parameter (fallback for callers that pass an explicit name), (3) `f.cfg.Model` env var (fallback of last resort for non-request contexts like background jobs).

**Rationale:** The caller's hardcoded name currently overrides the user's DB selection, making the model selection feature ineffective for generative calls. The DB value should win because it represents explicit user intent.

**Note for callers:** Callers that pass a specific model name (e.g. probe in `QueryStream`, extraction jobs) should pass empty string `""` to let the DB selection apply, or their hardcoded name will override. We'll audit call sites and update them to pass `""` where the user's selection should apply.

### Decision 5: Hard error when no config found for request context

**Chosen:** When `projectID` or `orgID` is present in context and no config row is found, `Resolve` returns a descriptive error, not `nil, nil`.

**Rationale:** Silent `nil, nil` returns cause confusing downstream failures. A clear "no provider configured for project X" error is directly actionable.

**Exception:** When both `projectID` and `orgID` are empty in context (background/test contexts), `Resolve` returns `nil, nil` to allow callers to fall back gracefully.

## Risks / Trade-offs

- **[Risk] Existing credentials lost** — The three old tables are dropped. Any org that configured credentials via CLI/API must reconfigure. → Accepted: data is test data only; document in migration notes.
- **[Risk] Callers passing hardcoded model names** — Several call sites in extraction/chat pass explicit model names. After Decision 4, they'll override the DB selection. → Mitigation: audit all `CreateModelWithName` callers during implementation and pass `""` where user selection should apply.
- **[Risk] `provider test` in `QueryStream` probe** — The probe calls `CreateModelWithName(ctx, "gemini-2.0-flash")`. After the precedence fix, the DB `generative_model` will be used regardless. → This is the desired behavior; the probe just needs a non-empty fallback for the case where no DB config exists at all.

## Migration Plan

1. Write migration `00042_refactor_provider_configs.sql`:
   - Drop `kb.organization_provider_model_selections`
   - Drop `kb.organization_provider_credentials`
   - Drop `kb.project_provider_policies` + `kb.provider_policy` enum
   - Create `kb.org_provider_configs`
   - Create `kb.project_provider_configs`
2. Update Go: entity → repository → service → handler → routes
3. Update `pkg/adk/model.go` model precedence
4. Update CLI commands
5. Update skills (emergent-onboard, emergent-providers)
6. Run migration, rebuild server, rebuild CLI, redeploy
7. Reconfigure org credentials with `emergent provider configure google-ai --api-key ...`

**Rollback:** Revert migration (down removes new tables, but old tables are gone — rollback restores schema only, not data).

## Open Questions

- Should `project_provider_configs` also support a "use org but override models only" row (i.e. no credential columns populated, just model columns)? **Decision: No — keep it simple. Project row = project owns its own full config.**
- Should `provider configure` print the selected default models after save so the user knows what was chosen? **Decision: Yes — always print the effective config after save.**
