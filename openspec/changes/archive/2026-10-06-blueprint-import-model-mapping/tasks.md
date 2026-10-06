## 1. Validate-before-materialize in Apply

- [x] 1.1 Add `validateManifest(m *BlueprintManifest) error` to `apply.go`: a pure, no-DB-write check of agents (non-empty name; visibility normalized via `agents.NormalizeVisibility`; model reference via `agents.NormalizeAgentModelConfig` with the write path's exact shape and a `agent %q: %v` bad-request wrap, empty model name allowed), packs (name + version non-empty), and skills (`skills.ValidateCreateSkill` with `s.skillsRepo.MaxContentSize()`).
- [x] 1.2 Call `s.validateManifest(&manifest)` in `Apply` immediately after `json.Unmarshal` and before `applyPacks`, so static errors return 400 with zero rows materialized. Verify `go build ./...` and `go test ./domain/blueprints/ -run ValidateManifest`.

## 2. Atomic import with model overrides

- [x] 2.1 Add `ModelOverrides map[string]string` (`json:"modelOverrides,omitempty"`) and `Apply bool` (`json:"apply,omitempty"`) to `ImportGitHubRequest`.
- [x] 2.2 In `ImportFromGitHub`, after marshalling the manifest, apply model overrides (rewrite each named agent's `Model` to `&AgentModelManifest{Name: override}`; empty string clears the name). Re-marshal before create.
- [x] 2.3 When `req.Apply` is true, require a non-empty projectID (else `400 project context required (X-Project-ID header or API token)`); after publish call `s.Apply(...)`; on error hard-delete the row via `s.repo.Delete(ctx, bp.ID)` and return the Apply error. Read the caller user id from ctx via `auth.UserFromContext`.
- [x] 2.4 Add `PreviewFromGitHub` returning `BlueprintPreview` (name/version/description/author/manifest), performing the same fetch+parse+override but creating nothing. Add the `BlueprintPreview` struct to `github_fetch.go`.

## 3. Handler + route

- [x] 3.1 Add `PreviewImport` (POST /api/blueprints/import/preview) mirroring `ImportBlueprint`'s scope/authority gating; return 200 JSON with Swagger annotations (`@Router`, `@Security`).
- [x] 3.2 Register `g.POST("/import/preview", h.PreviewImport)` in `routes.go` near `g.POST("/import", h.ImportBlueprint)`.

## 4. Tests

- [x] 4.1 Apply pre-validation: a manifest with an agent model missing the provider prefix returns 400 and creates no pack.
- [x] 4.2 Import atomic rollback: a stub so Apply fails leaves no published row and a retry succeeds (no 409).
- [x] 4.3 Model override rewrites the persisted manifest before apply (and empty override clears the name).
- [x] 4.4 Preview creates nothing.

## 5. Verification

- [x] 5.1 `go build ./...` from `apps/server/`.
- [x] 5.2 `go vet ./domain/blueprints/...`.
- [x] 5.3 `go test ./domain/blueprints/...`.
