# registry-blueprints — Design

## Context

The gallery lists "available" packs from `GET /api/schemas/projects/:pid` (project-owned
`kb.graph_schemas` rows) merged with packs embedded in the gateway binary (`//go:embed blueprints`).
The two have different lifecycles, scopes, and sources, and the merge hides real state:

- Bundled packs are compiled into the web-ui binary and can only be updated by a deploy; they are
  seeded into whichever project installs them.
- `kb.blueprints` already supports global (`project_id NULL`) and project-private rows plus
  `kb.blueprint_applications` for installed state — but the gallery never lists blueprints, so a
  published-unapplied blueprint is invisible and cannot be deleted.
- `applyPacks` looks schema packs up by `(name, version)` **without a project filter**, so the first
  project to apply a blueprint silently "owns" the pack and later projects reuse it.

## Goals

- Platform packs are delivered through the registry as shared, built-in entries with an explicit
  provenance marker.
- The gallery lists registry blueprints and shows install, built-in, and remove affordances.
- A user can remove a stale registry entry; a user/import blueprint survives uninstall and stays
  listed; re-import is idempotent.
- User/import blueprints stay project-private; only built-ins are shared.

## Decisions

### D1 — Explicit `origin` column, not inferred scope

Add `kb.blueprints.origin` (`builtin` | `user` | `import`). Scope (`project_id IS NULL`) is currently
a proxy for "built-in", but once users can publish into a shared registry the proxy breaks. `origin`
keeps "delivered by us" independent of sharing.

### D2 — Built-ins are seeded server-side as global published blueprints + global schema packs

A new fx-lifecycle seeder embeds the pack fixtures (moved to
`apps/server/domain/blueprints/builtin_packs/`) and upserts, idempotently by `(name, version)`:

- a global published `kb.blueprints` row with `origin=builtin`, and
- a global `kb.graph_schemas` row with `source='builtin'` (the existing partial unique index
  `graph_schemas_builtin_name_version_key` covers identity).

The server is the right owner — the registry lives there, and startup seeding is already the
established pattern (`schemas/builtin_seeder.go`). The layout loader in `github_fetch.go` is
refactored to `fs.FS` so the GitHub import path and the embedded path share one parser.

### D3 — Apply scopes the materialized pack by the blueprint's origin

`applyPacks` receives the blueprint `origin`:

- `builtin` → create/reuse the **global** `source='builtin'` pack, then assign it.
- `user`/`import` → create/reuse a **project-scoped** pack only (no cross-project lookup).

This preserves the idempotent-assign behavior while removing the accidental shared-pack ownership.

### D4 — Gallery lists blueprints, not schema packs

"Available" is published blueprints in scope, latest version per name, minus applied. Built-in badge
is `origin=builtin`. Install applies a blueprint by id. Drafts remain a separate section.

### D5 — Removal is blocked while applied; cleanup is owned by the server

`DELETE /api/blueprints/:id` deletes a project-private blueprint only when it has no applied
application for the caller's project (409 otherwise, "uninstall first"). On success the server clears
the blueprint's pack claims and deletes project-owned schema packs no longer referenced by any claim
or assignment. Global built-ins are immutable. This is what clears the dangling `personal-kb` entry.

### D6 — Import adopts an existing `(name, version)` in scope

On create conflict, the importer looks the row up in the same scope and adopts it (refresh a draft,
reuse a published row), then publishes/applies as requested. The previous hard 409 made a project
permanently unable to re-import.

## Alternatives considered

- **Auto-delete unapplied blueprints on uninstall** — rejected: uninstall and registry removal are
  distinct user intents; deleting on uninstall would defeat "stays listed after uninstall".
- **Keep bundled packs and add a registry overlay** — rejected (hard cut chosen): dual-source listing
  is the cause of the current confusion and dedupe defects.
- **Cleanup migration for existing per-project copies of formerly-bundled packs** — deferred: the
  global built-ins win the listing dedupe, and touching existing project data is a separate, riskier
  change.

## Risks

- Existing per-project pack rows for `agent-notes`/`dreaming-agent`/`operator`/`task-board` remain and
  are deduped by listing; Remove is hidden for built-ins. Accepted for this change.
- `origin` defaults to `user`; global rows are backfilled to `builtin`. Any pre-existing global
  non-builtin row would be mislabeled `builtin` — none exist in practice (globals were only ever the
  seeded built-ins).
