# registry-blueprints — Tasks

## 1. Server registry model

- [x] 1.1 Migration `00224_blueprint_origin.sql`: add `kb.blueprints.origin` (`builtin|user|import`,
      default `user`), backfill `project_id IS NULL` rows to `builtin`, add an origin index.
- [x] 1.2 Add `Origin` to the `Blueprint` entity + origin constants; add `Origin` to
      `CreateBlueprintRequest` and `AppliedBlueprint`.
- [x] 1.3 Move the platform pack fixtures to `apps/server/domain/blueprints/builtin_packs/`
      (`agent-notes`, `dreaming-agent`, `operator`, `task-board`).

## 2. Built-in seeder

- [x] 2.1 Refactor the blueprint layout loader (`github_fetch.go`) to read from `fs.FS` so the GitHub
      import path and the embedded built-in path share one parser.
- [x] 2.2 Add `builtin_seeder.go`: embed `builtin_packs`, load each pack, and idempotently upsert a
      global published blueprint (`project_id NULL`, `origin=builtin`) and a global
      `source='builtin'` schema pack by `(name, version)`.
- [x] 2.3 Register the seeder on the fx lifecycle in `blueprints/module.go`.

## 3. Origin-scoped apply

- [x] 3.1 `applyPacks` receives the blueprint `origin`: built-in → global `source='builtin'` pack;
      user/import → project-scoped pack with no cross-project lookup.
- [x] 3.2 Keep idempotent assign, `Merge=true`, `AutoUninstall=true`, and pack-claim recording.

## 4. Remove from the registry

- [x] 4.1 Relax `DeleteBlueprint`: project-private only, blocked while applied (409, "uninstall
      first"); global built-ins immutable.
- [x] 4.2 On success, clear the blueprint's pack claims and delete project-owned schema packs no
      longer referenced by any claim or assignment.

## 5. Idempotent import

- [x] 5.1 `ImportFromGitHub` adopts an existing `(name, version)` in scope (refresh draft, reuse
      published) and publishes/applies instead of returning 409.

## 6. Gateway: registry-backed gallery

- [x] 6.1 Retire the bundled embed, loader, manifest builder, and install-by-name paths (hard cut).
- [x] 6.2 "Available" = published blueprints in scope, newest per name, minus applied; each row
      carries `source` (`builtin`/`registry`) and `origin`.
- [x] 6.3 Render a Built-in badge; install applies by blueprint id.
- [x] 6.4 Add `MemoryClient.DeleteBlueprint` + `POST /blueprints/:id/remove` with a confirm dialog and
      a 409 → "uninstall first" flash; Remove shown only for non-built-in rows.

## 7. Tests and docs

- [x] 7.1 Server tests: migration, seeder idempotency, origin-scoped apply, delete guards/cleanup,
      import adoption.
- [x] 7.2 Gateway tests: available-from-blueprints mapping, built-in badge, remove handler + 409,
      install-by-id; remove bundled tests.
- [x] 7.3 Update gateway Go build/test and `templ generate` artifacts.
- [x] 7.4 Update e2e specs that referenced the bundled packs; add built-in install + remove coverage.

## 8. Verification

- [x] 8.1 `go build ./...` + `go test ./...` (server `domain/blueprints`, `domain/schemas`).
- [x] 8.2 `templ generate` + `go build ./...` + `go test ./...` from `apps/web-ui/gateway`.
- [x] 8.3 `openspec validate registry-blueprints --strict`.
- [ ] 8.4 Live Playwright e2e run — deferred to a live stack (not run in this change).
