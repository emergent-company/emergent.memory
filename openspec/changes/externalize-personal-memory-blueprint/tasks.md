<!-- openspec:archive-hold: E2E GitHub-import run pending a live stack -->

## 1. Remove the bundled pack

- [x] 1.1 Delete `apps/web-ui/gateway/blueprints/personal-memory/` from the gateway bundle.
- [x] 1.2 Confirm no remaining embedded-FS read of `personal-memory` (only fixture/fake data and
      comment strings keep the literal; the embedded dir is gone).

## 2. Update bundled-discovery unit test

- [x] 2.1 In `apps/web-ui/gateway/blueprints_test.go`, drop the `personal-memory` expectation from
      `TestListEmbeddedBlueprints`; keep `agent-notes`, `operator`, `task-board`, and the
      `Source == "bundled"` loop.
- [x] 2.2 Add a negative assertion that `personal-memory` is absent (fails if it is ever re-bundled).
- [x] 2.3 Update the test's doc comment to name the remaining packs.
- [x] 2.4 Leave untouched every other test that uses `"personal-memory"` as fake fixture data
      (fake `SchemaInfo`/`CompiledType` literals, `writeBundledPack(...)` temp fixtures, comments).

## 3. Repoint e2e at the GitHub import path

- [x] 3.1 Add `importGitHubBlueprint(page, url, opts?)` to
      `apps/web-ui/tests/e2e/helpers/blueprints.ts`, mirroring `installBlueprint`'s success/failure
      surfacing (`/blueprints/migrations` or `/blueprints?installed=1`), with an import-specific
      `IMPORT_TIMEOUT_MS = 90_000` budget.
- [x] 3.2 Export `PERSONAL_MEMORY_BLUEPRINT_URL` and `PERSONAL_MEMORY_BLUEPRINT_REF` (pinned to the
      `v3.0.0` tag) from the helper so the fixture cannot drift.
- [x] 3.3 In `apps/web-ui/tests/e2e/auth.setup.ts`, replace the bundled install block with
      `importGitHubBlueprint(page, PERSONAL_MEMORY_BLUEPRINT_URL, { ref: PERSONAL_MEMORY_BLUEPRINT_REF })`
      guarded by the installed-name check; keep `SCHEMA_PACK = 'personal-memory'`; raise the setup
      timeout (`setup.setTimeout(180_000)`).
- [x] 3.4 In `apps/web-ui/tests/e2e/scenarios/blueprint-object-chat.spec.ts`, convert
      `installBlueprint(page, 'personal-memory')` to the pinned
      `importGitHubBlueprint(page, PERSONAL_MEMORY_BLUEPRINT_URL, { ref: PERSONAL_MEMORY_BLUEPRINT_REF })`.

## 4. Documentation

- [x] 4.1 Repoint `operator.yaml`'s built-in pack example from `personal-memory` to `task-board`.
- [x] 4.2 Update `apps/web-ui/tests/e2e/README.md` to describe the external GitHub import (public repo,
      ref `v3.0.0`) instead of a bundled `personal-memory` install.
- [x] 4.3 Update `schema-overrides-shadowing-ui.spec.ts` comments: the seeded `personal-memory` pack
      is imported from the external GitHub repo in setup, not bundled.

## 5. Verification

- [x] 5.1 `go build ./...` and `go test ./...` from `apps/web-ui/gateway`.
- [x] 5.2 `openspec validate externalize-personal-memory-blueprint --strict`.
- [ ] 5.3 e2e (Playwright) — run separately on a live stack; not run in this change.
