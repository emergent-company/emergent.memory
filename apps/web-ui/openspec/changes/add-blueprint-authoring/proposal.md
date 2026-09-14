## Why

Blueprints can be browsed, installed, and derived from the project's current
schema, but there is no way to author or revise a blueprint's contents in the UI.
Every blueprint record is read-only on the detail page; the only write path is
"Save as new version", which copies the project's compiled schema wholesale and
cannot adjust pack metadata or individual types. Users must edit blueprints out
of band, and because published blueprints are immutable, correcting a draft means
recreating it.

## What Changes

- Add a **manual blueprint draft editor** in the web UI: edit a project-private
  draft blueprint's pack metadata and its object types (description, type-level
  ui accent, typed properties, widget hints), reusing the Schema-area object-type
  editor.
- Add a **Release** action that publishes a draft blueprint, after which it is
  immutable, with an explicit confirmation.
- Add **install-from-version**: the blueprint version list gains an install
  action per version; installing a still-draft version offers release-then-install.
- Editing a **published** version forks a new draft version first and then opens
  the editor (published versions are immutable).
- Require the **schema write** capability for every authoring/release/install
  action.
- No breaking changes: the surface is additive.

## Capabilities

### New Capabilities

- `blueprint-authoring`: manual authoring of a blueprint draft in the web UI —
  editing pack metadata and object types, forking a new draft from a published
  version, releasing (publishing) a draft, and installing a chosen version.

### Modified Capabilities

- `blueprint-gallery`: the gallery and blueprint detail gain edit, release, and
  install-version affordances; the drafts list becomes an authoring surface
  rather than install-only.

## Impact

- **Gateway** (`gateway/`): new UI routes/handlers for draft edit, release, and
  install-version; changes to `blueprints.go`, `blueprints.templ`,
  `blueprints_handlers.go`, `main.go`. Manifest read-modify-write reuses
  `ObjectTypeEdit`/`applyObjectTypeEdit`/`objectTypeMapFromEdit` (currently used
  for schema packs) against a blueprint manifest's pack.
- **Memory API**: reuses existing `GetBlueprint`, `UpdateBlueprint`,
  `CreateBlueprintVersion`, `PublishBlueprint`, `ApplyBlueprint`. No backend
  change expected — published-blueprint immutability is already enforced by
  memory's publish checksum.
- **Tests**: TDD — unit tests for manifest edit merge, fork-on-published,
  release gating, and install-version selection are the minimum bar; e2e UI
  coverage is optional.
