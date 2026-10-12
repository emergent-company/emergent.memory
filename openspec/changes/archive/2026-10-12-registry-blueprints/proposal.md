## Why

The blueprint gallery's "Available" list is assembled from two sources: project-owned schema packs
in the server's schema registry, and blueprint packs embedded in the gateway binary
(`apps/web-ui/gateway/blueprints/`). The embedded ("bundled") packs are compiled into the web-ui
binary, are seeded per-project on install, and cannot be shared across projects or updated
independently of a deploy. This makes the platform's own packs second-class next to registry packs.

Two related defects fall out of the same model:

- A user-created or imported blueprint that is uninstalled keeps its `kb.blueprints` row, but the
  gallery only lists drafts and applied packs, so a published-but-unapplied blueprint is invisible
  and unremovable. Re-importing the same `(name, version)` then fails forever with
  `409 conflict: blueprint with name and version already exists`.
- Registry schema packs have no removal path in the UI (only Install), so stale packs — e.g.
  `personal-kb` — hang on the available list indefinitely.

## What Changes

- **Registry provenance.** Add an `origin` column to `kb.blueprints` (`builtin` | `user` | `import`),
  so "delivered by us" is an explicit, queryable property rather than an inference from scope.
- **Built-in packs move to the registry.** The platform packs currently embedded in the gateway
  (`agent-notes`, `dreaming-agent`, `operator`, `task-board`) ship as **global, published, built-in
  blueprints** (`project_id NULL`, `origin=builtin`) with **global `source='builtin'` schema packs**,
  seeded idempotently at server startup from fixtures moved into the server domain. Only built-ins are
  shared across projects; user/import blueprints stay project-private (a future feature lets a user
  opt a blueprint into sharing).
- **Retire the bundled machinery (hard cut).** Remove the gateway's embedded pack directory, loader,
  manifest builder, and install-by-name paths. `source="bundled"` is no longer emitted.
- **Gallery is blueprint-registry-backed.** The "Available" list becomes published blueprints in the
  caller's read scope (global built-ins + the project's own), latest version per name, minus those
  already applied. Built-in rows carry a **Built-in** badge.
- **Remove from registry.** Add `DELETE /api/blueprints/:id` and a gallery Remove action for
  non-built-in blueprints. Removal is blocked while the blueprint is applied (409 — uninstall first)
  and, on success, clears the blueprint's pack claims and deletes schema packs it orphaned.
- **Idempotent import.** GitHub import adopts an existing `(name, version)` in scope (refreshing a
  draft, reusing a published row) instead of returning 409.
- **Origin-scoped pack materialization.** Applying a built-in blueprint materializes the global shared
  pack; applying a user/import blueprint materializes a project-private pack only — fixing accidental
  cross-project pack reuse.

## Impact

- Server: `apps/server/domain/blueprints/*` (entity, apply, service, github_fetch, new `builtin_seeder`,
  module), a new migration, and `apps/server/domain/schemas/repository.go`.
- Gateway: `apps/web-ui/gateway/*` (blueprints page, client, handlers, templ, ui/schema wiring).
- Specs: new capability `blueprint-registry`; modified `blueprint-gallery` and `blueprint-api`.
- Retention: existing per-project `kb.graph_schemas` rows for formerly-bundled pack names are left in
  place; the global built-ins win the listing dedupe. A cleanup migration is deliberately out of scope.
