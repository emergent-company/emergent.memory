## Context

`POST /api/blueprints/import` (github_fetch.go) fetches a GitHub tarball, loads a
`BlueprintManifest`, creates a draft, and publishes it. The web UI then applies
the blueprint separately. `Apply` (apply.go) materializes packs first, then
agents, then skills, then seed. Agent model references are validated in
`buildAgentDefinition` / `applyAgentManifestToExisting` via
`agents.NormalizeAgentModelConfig`, which requires a `provider/model` reference
(`domain/agents/model_ref.go`).

Imported manifests often carry bare model names, so apply fails at the agents
step — after packs have already been created — and the published row blocks a
retry with a 409 on the duplicate `(name, version)` unique index.

## Goals / Non-Goals

**Goals**

- Static manifest errors fail before any materialization.
- Import can rewrite agent model references before apply.
- A failed atomic apply leaves no published row, so a retry re-imports cleanly.
- The UI can preview a manifest (with overrides) without creating anything.

**Non-Goals**

- Cross-domain apply rollback (packs/agents/skills/seed remain stepwise and
  idempotent; only the blueprint row itself is rolled back).
- Replacing the stepwise apply with a transaction.

## Decisions

### D1 — Validate before materialize, not during

`validateManifest` mirrors the *exact* validation each write path performs, so an
error surfaced by it is indistinguishable from an error the write path would have
surfaced — except nothing has been written yet. Agent model references go through
the same `agents.NormalizeAgentModelConfig` funnel with `Provider: ""`, matching
`buildAgentDefinition`/`applyAgentManifestToExisting`. An empty model name is
allowed (provider default). Skill validation reuses `skills.ValidateCreateSkill`
and `skills.Repository.MaxContentSize()`.

### D2 — Model overrides rewrite the manifest before create

`ImportGitHubRequest.ModelOverrides` maps an agent `Name` to a `provider/model`
string (or `""` to clear the name and select the project default). The override
is applied by unmarshalling the marshalled manifest, replacing the matching
agent's `Model` with `&AgentModelManifest{Name: override}`, and re-marshalling.
This keeps the stored manifest canonical (the override becomes the manifest, not
a side channel), so apply sees the same bytes the preview showed.

### D3 — Atomic apply rolls back only the blueprint row

When `Apply` is true, the import publishes and then applies. On an apply error the
just-created row is hard-deleted via `Repository.Delete` (unconditional by id),
and the apply error is returned. This is safe because packs/agents/skills may
still be partially materialized by a failed apply (that path is stepwise and
idempotent by design), but the *blueprint row* is gone, so a retry re-imports
instead of colliding. Apply is project-scoped, so atomic apply requires a project
context.

### D4 — Preview is create-nothing fetch+parse+override

`PreviewFromGitHub` shares the fetch+parse+override helper with `ImportFromGitHub`
but returns a `BlueprintPreview` and never calls `CreateBlueprint`/`PublishBlueprint`.
The handler gates it with the same scope/authority as import (project context →
project scope; no project → global write requires superadmin), because fetching
an arbitrary GitHub URL on behalf of a caller is a capability-guarded operation
even though nothing is written.

## Risks / Trade-offs

- `validateManifest` reads `s.skillsRepo.MaxContentSize()`; a manifest with skills
  applied against a service with a nil skills repo would already panic in
  `applySkills`, so the pre-validation does not introduce a new failure mode.
- Empty-string overrides deliberately produce a model struct with an empty name
  (project default) rather than deleting the `Model` field, matching the create
  path's "empty `ModelConfig.Name` selects the provider default" semantics.
