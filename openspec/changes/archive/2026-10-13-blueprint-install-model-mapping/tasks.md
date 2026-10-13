## 1. Apply-time overrides (server)

- [x] 1.1 Add `ModelOverrides map[string]string` to `ApplyOptions` (JSON `modelOverrides`). Verify `go build ./...` from `apps/server`.
- [x] 1.2 Add `applyModelOverrides` helper: set/replace an agent's model name (creating `Model` when nil), preserve `temperature`/`maxTokens`/`enableThinking`, and treat an empty value as clearing the name (project default).
- [x] 1.3 In `Apply`, resolve effective overrides (stored per-project merged with explicit request; explicit wins) before validation/materialization, and persist the explicit map after a successful apply. Verify `go test ./domain/blueprints/... -count=1`.

## 2. Persistence (server)

- [x] 2.1 Add `BlueprintModelOverride` entity (`kb.blueprint_model_overrides`).
- [x] 2.2 Migration `00238_blueprint_model_overrides.sql`: table + unique `(project_id, blueprint_name, agent_name)` + `(project_id, blueprint_name)` index; goose Up/Down. Verify `task migrate:up`.
- [x] 2.3 Repository methods `UpsertModelOverrides` (upsert by agent name), `ListModelOverrides`, `DeleteModelOverrides`. Verify `go test ./domain/blueprints/... -count=1`.

## 3. GitHub import unification (server)

- [x] 3.1 `ImportFromGitHub`: stop rewriting the stored manifest; `apply:true` routes `ModelOverrides` through `Apply`; `apply:false` persists them project-scoped (skip for a global import).
- [x] 3.2 `PreviewFromGitHub`: apply `modelOverrides` in memory so the preview reflects the manifest as it will be applied.
- [x] 3.3 `fetchAndLoadManifest`: return the pristine manifest for both the stored and baseline slots (no override rewriting). Verify `go test ./domain/blueprints/... -count=1`.

## 4. Model-overrides API (server)

- [x] 4.1 Service `GetModelOverrides` / `UpsertModelOverrides`.
- [x] 4.2 Handlers `GetModelOverrides` (GET `?name=`) and `UpsertModelOverrides` (PUT `{name, modelOverrides}`), both 400 without project context, with Swagger annotations.
- [x] 4.3 Routes registered before `/:id` with `RequireProjectID`.
- [x] 4.4 `route-authority.yaml` entries (GET + PUT) at `project-member` with `RequireAuth, RequireProjectTokenScope, RequireProjectMember, RequireProjectID`.

## 5. Tests (server)

- [x] 5.1 Rewrite `github_fetch_test.go` model-override tests for the new behavior (manifest pristine; project-scoped persistence) and add a preview-applies-overrides test.
- [x] 5.2 `apply_test.go`: `applyModelOverrides` unit tests + apply-time precedence/persistence integration test.
- [x] 5.3 `repository_test.go`: `UpsertModelOverrides`/`ListModelOverrides`/`DeleteModelOverrides` round-trip + upsert overwrite + empty-value override.
- [x] 5.4 `handler_authz_test.go`: GET/PUT model-overrides endpoints (400 without project, 400 without name, PUT→GET round-trip).

## 6. Verification (server)

- [x] 6.1 `go build ./...` from `apps/server`.
- [x] 6.2 `go test ./domain/blueprints/... -count=1` (against a local Postgres).
- [x] 6.3 `gofmt -l` clean on all touched files.
- [x] 6.4 Migration applies to head (goose `version: 238`).

## 7. Web UI (model review before install)

- [x] 7.1 Bundled/registry installs (`POST /blueprints/install`) and draft installs (`POST /blueprints/:id/install`) render a model-review step when any agent pins a model; confirm (`POST /blueprints/install/confirm`) applies with `modelOverrides`. Verify `cd apps/web-ui/gateway && go test ./... -count=1`.
- [x] 7.2 The GitHub import flow shows the same review (pre-filled), submitting overrides at apply time. Verify handler tests + Playwright spec `blueprint-model-review-ui.spec.ts`.
- [x] 7.3 Review pre-fill precedence: saved per-project override > pinned model (servable) > project default; unservable pins show the blueprint's suggestion + a provider-settings link.

## 8. CLI (install-time mapping)

- [x] 8.1 `ProjectFile` reads `name`/`version`/`description`/`author` (override-store identity). Verify `go test ./internal/blueprints/... -count=1`.
- [x] 8.2 `ModelMap` provider-servability view + resolution precedence (a stored empty value is an explicit project default). Verify `go test ./internal/blueprints/...`.
- [x] 8.3 `--map-model` / `--no-map` flags + interactive TTY prompt; advisory (non-blocking) warning for unservable pins. Verify `go test ./internal/cmd/... -count=1`.
- [x] 8.4 Persist the user's choices via `PUT /api/blueprints/model-overrides` after a successful non-dry-run install (advisory on failure).
