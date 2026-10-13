## Why

Installing a GitHub blueprint whose agents pin a model the project does not have
(or a malformed bare name) currently forces the user to rewrite the manifest at
import time. `POST /api/blueprints/import` accepts a `modelOverrides` map and
bakes it into the **stored** manifest before the blueprint is created. That
rewrites the blueprint's canonical content, which then:

1. **Breaks the update-check baseline.** The stored manifest no longer matches
   the upstream source, so provenance-based update detection has to special-case
   an "overridden import" and hold a second raw-upstream hash just to avoid a
   false positive.
2. **Couples the mapping to the import moment.** The user's model choice cannot
   be revisited later, and it cannot survive a version upgrade (a new version is
   a new row, so a re-import loses the mapping).
3. **Mixes two concerns.** Importing (fetch + publish) and installing (applying
   to the project) are different steps; the model mapping belongs to the
   install, not the fetch.

This change moves `modelOverrides` from import-time manifest rewriting to
**apply-time overrides with per-project persistence**: the stored manifest stays
pristine, the mapping is applied in memory at apply time (and persisted
per-project keyed by blueprint name), and two new endpoints read/write the
mapping directly so the gallery and CLI can present and persist a model-mapping
step before install.

## What Changes

- `ApplyOptions` gains `ModelOverrides` (`map[string]string`); `Apply` resolves
  the effective overrides with precedence **explicit request > stored
  per-project > manifest pin**, applies them to the in-memory manifest before
  validation/materialization, and persists the explicit map after a successful
  apply.
- New entity + table `kb.blueprint_model_overrides`, keyed by `(project_id,
  blueprint_name, agent_name)` — NOT by blueprint id — so a user's mapping
  survives a version upgrade.
- GitHub import no longer rewrites the stored manifest: `apply:true` routes
  overrides through apply; `apply:false` persists them project-scoped for a
  later apply (a global import has no project scope and skips persistence). The
  import preview still applies overrides in memory so the caller sees the
  manifest as it will be applied.
- New endpoints:
  - `GET /api/blueprints/model-overrides?name=<name>` → `{modelOverrides}`.
  - `PUT /api/blueprints/model-overrides` with `{name, modelOverrides}` →
    upsert, return the resulting map.
  Both require a project context (`X-Project-ID` header or API token).

## Capabilities

### Modified Capabilities

- `blueprint-api`: GitHub import stops baking `modelOverrides` into the stored
  manifest; the mapping is apply-time + persisted per-project; two
  model-overrides endpoints are added.
- `blueprint-gallery`: every install source (bundled, registry, draft, GitHub
  import) renders a model-review step before applying when any agent pins a
  model, pre-filled with the saved per-project choice, else the pinned model
  when the project serves its provider, else the project default; confirm
  applies with `modelOverrides` (persisted per-project).
- `cli-blueprints`: `memory blueprints install` maps each pinned agent's model
  at install time (interactive prompt on a TTY, `--map-model` / `--no-map`
  otherwise), with the same resolution precedence, and persists the choices
  per project.

## Impact

**Server (`apps/server/`)**
- `domain/blueprints/apply.go`: `ApplyOptions.ModelOverrides`, effective-override
  resolution in `Apply`, `applyModelOverrides` helper, persistence after apply.
- `domain/blueprints/entity.go`: `BlueprintModelOverride` entity.
- `domain/blueprints/repository.go`: `UpsertModelOverrides`,
  `ListModelOverrides`, `DeleteModelOverrides`.
- `domain/blueprints/service.go`: `GetModelOverrides`, `UpsertModelOverrides`.
- `domain/blueprints/github_fetch.go`: import unification (no import-time
  rewrite); preview applies overrides in memory.
- `domain/blueprints/handler.go` + `routes.go`: GET/PUT model-overrides handlers
  and routes.
- `route-authority.yaml`: two new entries.
- `migrations/00238_blueprint_model_overrides.sql`: new table + index.

**Web UI (`apps/web-ui/gateway/`)**
- `blueprints_handlers.go`: `pinnedAgentsFromManifest` / `buildModelMapRows` /
  `blueprintModelMapContext` / `loadInstallManifest` / `modelOverridesFromForm`
  / `applyBlueprintPRG` / `uiInstallBlueprintConfirm`; `uiInstallBlueprint`,
  `uiInstallBlueprintById`, and `uiImportGitHubBlueprint` gate on the review.
- `blueprints_model_map.templ`: source-generic `BlueprintModelMapData` /
  `BlueprintModelMapRow` (selected state, per-row provider link).
- `blueprint_client.go` + `backend.go`: `ApplyBlueprintWithOverrides`,
  `GetModelOverrides`; `main.go`: `POST /blueprints/install/confirm`.
- Tests + a Playwright e2e for the bundled `operator` review step.

**CLI (`apps/cli/`)**
- `internal/blueprints/types.go`: `ProjectFile` gains `name`/`version`/
  `description`/`author` (override-store identity).
- `internal/blueprints/modelmap.go`: provider/servability view + resolution
  precedence.
- `internal/cmd/blueprints_modelmap.go` + `blueprints.go`: `--map-model` /
  `--no-map`, interactive prompt, advisory warning, `PUT` persistence.

## Non-Goals

- No change to how a model reference itself is parsed/validated (the
  `provider/model` form and the shared agent model funnel are unchanged).
- No import-time manifest rewrite remains anywhere.
- No change to the update-check job machinery beyond the now-unconditional raw
  upstream baseline.
