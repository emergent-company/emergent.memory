## 1. Gateway data layer (manifest edit + fork + release)

- [ ] 1.1 Add a blueprint-manifest edit helper in `gateway/schema_write.go`: read the draft via `GetBlueprint`, decode its `blueprintManifest`, apply an `ObjectTypeEdit` to `Packs[0].ObjectTypes` with `applyObjectTypeEdit`, synthesize a pack from the blueprint identity when the manifest has none, and persist via `UpdateBlueprint`. Verify: unit tests assert unrelated types are preserved, add/remove is exact, the pack name is immutable, and the update targets the draft id.
- [ ] 1.2 Add a fork-on-published helper: when the target version is published, call `CreateBlueprintVersion` first and return the draft id; when it is already a draft, return it unchanged. Verify: unit tests assert a published base triggers `CreateBlueprintVersion` and the edit lands on the fork while the base is never passed to `UpdateBlueprint`.
- [ ] 1.3 Add release and install-version helpers that call `PublishBlueprint` and `ApplyBlueprint`, refusing a still-draft version until it is released. Verify: unit tests assert release calls `PublishBlueprint(id)`, install calls `ApplyBlueprint` with the chosen version id, and a draft install without release returns a validation error.
- [ ] 1.4 Add a downgrade guard: compare the candidate version against the version currently applied for the blueprint (reusing `versionGreater` semantics) and reject an older candidate before any apply call; equal (re-install) and newer are allowed. Verify: unit tests cover older-blocked, equal-allowed, newer-allowed, and that no apply call happens on a blocked install.

## 2. Gateway handlers and routes

- [ ] 2.1 Add `GET /blueprints/:id/edit` (draft-aware; forks a draft when the version is published) and the editor view model. Verify: handler test renders the editor for a draft, and a published version redirects to the freshly forked draft.
- [ ] 2.2 Add `POST /blueprints/:id/edit` to persist an edited draft, re-rendering with preserved values on validation/service error (PRG to the detail on success). Verify: handler tests cover success redirect, invalid type/property, and service failure.
- [ ] 2.3 Add `POST /blueprints/:id/release` (confirmed publish) and `POST /blueprints/:id/versions/:versionId/install` (release-then-install for drafts). Verify: handler tests cover release success, install-by-version, draft-install-without-release, and error flashes.
- [ ] 2.4 Gate all new mutation routes with `requireSchemaWrite`. Verify: unit test that a caller without the capability gets an authorization error and no blueprint/apply call is made.

## 3. UI (templ)

- [ ] 3.1 Add edit and release actions to the drafts list and blueprint detail header, and an install action per version row, gated on project-owned drafts for edit. Verify: render tests show the actions for a draft, hide edit for a published/global version, and show install per version row.
- [ ] 3.2 Reuse the Schema-area object-type editor templ for the blueprint draft editor (immutable pack name, editable description and object types). Verify: render test covers existing values, add/remove property rows, and the unknown-version state.
- [ ] 3.3 Add a release confirmation dialog naming the version and warning that release makes it immutable. Verify: render test asserts the dialog appears and that cancelling performs no POST.

## 4. Verification

- [ ] 4.1 Run `templ generate` and `go build ./...` from `gateway/`; verify both succeed.
- [ ] 4.2 Run `go test ./...` from `gateway/`; verify all unit tests pass.
- [ ] 4.3 Run `PATH="/root/go/bin:$PATH" task lint`; verify no new issues.
- [ ] 4.4 Manual browser pass on `task dev`: edit a draft, release it, install the released version, and confirm a published version forks on edit; verify each flow reaches its expected end state with no console errors.
- [ ] 4.5 Update `openspec/specs/blueprint-gallery/spec.md` and create `openspec/specs/blueprint-authoring/spec.md` at archive time; verify `openspec validate --specs --strict` passes.
