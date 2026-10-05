## 1. Remove the bundled pack

- [x] 1.1 Delete `apps/web-ui/gateway/blueprints/personal-memory/` from the gateway bundle.
- [x] 1.2 Confirm no remaining embedded-FS read of `personal-memory` (only fixture/fake data and
      comment strings keep the literal; the embedded dir is gone).

## 2. Update bundled-discovery unit test

- [x] 2.1 In `apps/web-ui/gateway/blueprints_test.go`, drop the `personal-memory` expectation from
      `TestListEmbeddedBlueprints`; keep `agent-notes`, `operator`, `task-board`, and the
      `Source == "bundled"` loop.
- [x] 2.2 Update the test's doc comment to name the remaining packs.
- [x] 2.3 Leave untouched every other test that uses `"personal-memory"` as fake fixture data
      (fake `SchemaInfo`/`CompiledType` literals, `writeBundledPack(...)` temp fixtures, comments).

## 3. Repoint e2e at the GitHub import path

- [x] 3.1 Add `importGitHubBlueprint(page, url, opts?)` to
      `apps/web-ui/tests/e2e/helpers/blueprints.ts`, mirroring `installBlueprint`'s success/failure
      surfacing (`/blueprints/migrations` or `/blueprints?installed=1`).
- [x] 3.2 In `apps/web-ui/tests/e2e/auth.setup.ts`, replace the bundled install block with
      `importGitHubBlueprint(page, GITHUB_BLUEPRINT_URL)` guarded by the installed-name check;
      keep `SCHEMA_PACK = 'personal-memory'`.
- [x] 3.3 In `apps/web-ui/tests/e2e/scenarios/blueprint-object-chat.spec.ts`, convert
      `installBlueprint(page, 'personal-memory')` to
      `importGitHubBlueprint(page, GITHUB_BLUEPRINT_URL)`.

## 4. Documentation

- [x] 4.1 Repoint `operator.yaml`'s built-in pack example from `personal-memory` to `task-board`.

## 5. Verification

- [x] 5.1 `go build ./...` and `go test ./...` from `apps/web-ui/gateway`.
- [x] 5.2 `openspec validate externalize-personal-memory-blueprint --strict`.
- [ ] 5.3 e2e (Playwright) — run separately on a live stack; not run in this change.
