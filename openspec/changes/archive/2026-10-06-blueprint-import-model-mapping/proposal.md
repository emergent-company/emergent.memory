## Why

The web-UI GitHub blueprint import is non-atomic. `POST /api/blueprints/import`
fetches, parses, creates (draft), and publishes a blueprint; the caller then
calls `POST /api/blueprints/:id/apply`. Imported agent manifests carry bare model
names (e.g. `gemini-2.5-flash`), which the server rejects at `applyAgents` with a
400 — but only *after* `Apply` has already materialized packs (packs run before
agents in `apply.go`), and the already-published blueprint row blocks a retry
with a 409. The user is left with a half-applied blueprint they cannot cleanly
retry.

Two gaps:

1. **Apply is not validate-before-materialize.** `Apply` unmarshals the manifest
   and immediately starts materializing packs; a static manifest error (bad agent
   model reference, bad visibility, missing pack name/version, invalid skill)
   is only discovered *after* earlier steps have written rows.
2. **Import has no way to map models or to fail atomically.** There is no way to
   rewrite a bare model name to a valid `provider/model` reference before apply,
   and a failed apply leaves the published row behind so a retry hits a 409.

## What Changes

- `Apply` gains a pure, no-write `validateManifest` step that checks every agent
  model reference (through the same `agents.NormalizeAgentModelConfig` funnel the
  write path uses), visibility, pack name/version, and skill validity *before*
  any pack/agent/skill is materialized. Static errors return 400 with zero rows
  created.
- `ImportGitHubRequest` gains `modelOverrides` (map agent name → `provider/model`
  or empty = project default) and `apply` (atomically publish-and-apply with
  rollback). A preview endpoint (`POST /api/blueprints/import/preview`) performs
  the same fetch+parse+override but creates nothing, so the UI can map models
  before committing.

## Capabilities

### Modified Capabilities

- `blueprint-api`: GitHub import becomes atomic (rollback on apply failure),
  accepts model overrides, and gains a create-nothing preview endpoint; apply
  validates the manifest before materializing anything.
- `blueprint-gallery`: the gallery import flow maps agent models before install
  and guarantees no partial install.

## Impact

**Server (`apps/server/`)**
- `domain/blueprints/apply.go`: new `validateManifest`; called at the top of
  `Apply` before `applyPacks`.
- `domain/blueprints/github_fetch.go`: `ImportGitHubRequest` fields,
  `BlueprintPreview`, refactored fetch+override helper, `PreviewFromGitHub`,
  atomic apply + rollback in `ImportFromGitHub`, `callerUserID`.
- `domain/blueprints/handler.go`: `PreviewImport` handler.
- `domain/blueprints/routes.go`: register `POST /import/preview`.

**Web UI (parallel lane, not in this PR)**
- `apps/web-ui/**`: the gallery import form maps agent models via the preview
  endpoint and submits `modelOverrides` + `apply: true`.

## Non-Goals

- No cross-domain apply rollback (packs/agents/skills materialization remains
  stepwise and idempotent; only the *blueprint row* is rolled back on a failed
  atomic import).
- No change to per-step apply semantics beyond the added pre-validation.
