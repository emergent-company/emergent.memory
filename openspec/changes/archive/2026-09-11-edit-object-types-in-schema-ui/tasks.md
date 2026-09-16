## 1. Gateway write paths (data layer)

- [x] 1.1 Reconcile with `ui-data-authoring`: confirm whether `POST/PUT /api/schema/object-types` already exists; if so reuse it, otherwise implement the edit route against the project schema pack. Verify: a written note in the PR/commit stating which endpoint family is used, and `grep -rn "schema/object-types" gateway/` shows exactly one family.
  - Note: `ui-data-authoring` is NOT implemented in this worktree (`grep -rn "schema/object-types" gateway/` shows no `/api/schema/object-types` routes). The edit family implemented here is the Schema-area family `GET/POST /schema/object-types/:name` + `GET /schema/object-types/:name/edit`, backed by the new `MemoryClient` pack-mutation methods (`CreateSchemaPack`/`UpdateSchemaPack`/`AssignSchemaPack`). When `ui-data-authoring` lands it maps `POST/PUT /api/schema/object-types` onto those same methods.
- [x] 1.2 Add `MemoryBackend` methods for pack mutation: `UpdateSchemaPack` (GetSchema → merge one type → `PUT /api/schemas/:packId`) and `CreateSchemaPack` (`POST /api/schemas`). Verify: unit tests assert the full read-modify-write payload preserves unrelated types and that a create request carries the project-scoped pack name/version.
- [x] 1.3 Add `MemoryBackend`/client methods for blueprint derivation: `UpdateBlueprint` (`PUT /api/blueprints/:id`) and `CreateBlueprintVersion` (`POST /api/blueprints/:id/versions`). Verify: unit tests (httptest) assert method/path/body for both, including the new-version then manifest-replace sequence.
- [x] 1.4 Implement the override-pack helper: upsert an edited blueprint-derived type into a project-owned override pack and (re)assign it after the blueprint pack. Verify: unit test asserts a `CreateSchemaPack`+assign on first override and an `UpdateSchemaPack` on repeat, and that no call targets the blueprint's original pack id.
- [x] 1.5 Implement the gateway-side blueprint provenance resolver (applied blueprints → manifests → map `schemaName`/`schemaVersion` to blueprint). Verify: unit tests cover blueprint-derived match, project-authored miss, version mismatch, and two-blueprints-same-pack ambiguity.
- [x] 1.6 Implement effective-type blueprint manifest assembly from current compiled types (reuse `gateway/blueprint_manifest.go`). Verify: unit test that the derived manifest contains exactly the effective object types (post-override) and their relationship types, and excludes shadowed/losing duplicates.

## 2. Gateway handlers and routes

- [x] 2.1 Add Schema-area routes in `gateway/main.go` and handlers in `gateway/schema.go`: `GET /schema/add`, `POST /schema/add` (install), `GET /schema/object-types/:name/edit`, `POST /schema/object-types/:name` (edit). Verify: handler unit tests return 200/redirect on success, 4xx on unknown type, and a 502-style error when the memory call fails.
- [x] 2.2 Add derivation routes: `POST /schema/blueprints/derive` (new blueprint) and `POST /schema/blueprints/:id/versions/derive` (new version). Verify: handler unit tests assert 201/redirect on success, 409 surfaced for duplicate name+version, and 400 when the project has no compiled types.
- [x] 2.3 Resolve provenance and history into the Schema view models used by both the list and the type detail page. Verify: unit tests assert a derived type renders its owning blueprint name/version and matching history entries, and a project-authored type renders no badge.
- [x] 2.4 Require the schema write capability on all mutation routes. Verify: unit test that a caller without the capability gets an authorization error and no pack/blueprint call is made.

## 3. Schema-area UI (templ)

- [x] 3.1 Add the "Add schema" action and available-schema list to the Schema page, reusing the existing install handler. Verify: `templ generate` + `go build ./...` clean, and a render test shows the action and empty state.
- [x] 3.2 Add the object-type editor (immutable name, editable description, add/remove/edit typed properties) reachable from the list and detail views. Verify: render test covers existing values, adding/removing a property row, and the unknown-type state.
- [x] 3.3 Render the blueprint-derived badge, the owning-blueprint link, and the schema history section on `/schema/object-types/:name`. Verify: render tests for derived vs project-authored types and for available vs unavailable history.
- [x] 3.4 Add the blueprint-derived edit warning confirmation flow (names the owning blueprint, states the edit diverges and may be overwritten) and ensure cancel persists nothing. Verify: render/JS test asserts the warning appears only for derived types and that dismissing keeps the editor values without a POST.
- [x] 3.5 Add the "Save as blueprint" UI (new blueprint and new version of an existing one) to the Schema area and blueprints gallery. Verify: render test shows the action; manual test creates a draft that appears in the drafts list.

## 4. Verification

- [x] 4.1 Run `templ generate` and `go build ./...` from `gateway/`; verify both succeed.
- [x] 4.2 Run `PATH="/root/go/bin:$PATH" task lint`; verify no new issues.
- [x] 4.3 Run `go test ./...` from `gateway/`; verify all unit tests pass.
- [ ] 4.4 Manual browser test via `task dev` (port from `ALFRED_PORT`, default 8095): add a schema, edit a project-authored type, edit a blueprint-derived type and confirm the warning, view history, and derive a blueprint draft. Verify: each flow reaches its expected end state with no console errors.
- [x] 4.5 Update `openspec/specs/blueprint-api/spec.md` to reconcile the documented `PATCH/DELETE /api/blueprints/assignments/:id` routes with the implemented behavior if the implementation changes that surface. Verify: `openspec validate edit-object-types-in-schema-ui --strict` passes. Note: no-op — this change did not alter the assignment toggle/remove surface, so no main-spec edit was warranted. The pre-existing drift (documented PATCH/DELETE assignments vs implemented `POST /api/blueprints/:id/unapply`) is left for a separate cleanup. `openspec validate edit-object-types-in-schema-ui --strict` → valid.

## 5. Cross-repo follow-ups (tracked, not blocking)

- [x] 5.1 File an issue in `emergent-company/emergent.memory` to expose the stored blueprint→pack provenance (`kb.project_schemas.source_blueprint_id` / `kb.blueprint_pack_claims`) and a per-type version-history endpoint. Verify: issue URL recorded. Done: https://github.com/emergent-company/emergent.memory/issues/423
- [x] 5.2 Open a follow-up change note for editing relationship types and deleting project-authored types. Verify: note recorded in `docs/tasks/`. Done: `docs/tasks/schema-object-type-editing-followups.md`.
