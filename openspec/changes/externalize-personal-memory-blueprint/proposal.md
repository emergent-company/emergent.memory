## Why

The `personal-memory` blueprint pack (schema + agents) currently ships inside the compiled gateway
binary as an embedded bundle under `apps/web-ui/gateway/blueprints/personal-memory/`. The pack is now
maintained externally — it lives in the public GitHub repo
[`mkucharz/personal-memory-blueprint`](https://github.com/mkucharz/personal-memory-blueprint), pushed at
`v3.0.0`, and is installed at runtime via the gallery's "Import from GitHub" path (`/blueprints/import`).

The external repo combines the former registry pack `personal-kb` with the bundled `personal-memory`
pack and evolves the schema: it adds a `Fact` object type, renames `Memo`→`Note`, and
`CalendarEvent`→`Event`. The app should stop compiling the pack into the binary so that the external
repo becomes the single source of truth, and the bundled copy no longer drifts from it.

## What Changes

- Remove the embedded `personal-memory` pack directory from the gateway bundle.
- Update `TestListEmbeddedBlueprints` so it no longer expects `personal-memory` (the remaining
  embedded packs `agent-notes`, `operator`, and `task-board` are still asserted).
- Repoint e2e setup and the object-chat scenario at the GitHub import path: a new
  `importGitHubBlueprint` helper drives the `/blueprints/import` form instead of installing the
  (now-removed) bundled pack. The blueprint keeps its name (`personal-memory`), so all name-based
  assertions remain valid.
- Update operator documentation that cited `personal-memory` as a built-in pack example.

## Impact

- The `personal-memory` blueprint is no longer available as a bundled pack; fresh tenants install it
  from the external GitHub repo.
- No other embedded-pack read path references `personal-memory` (only the generic
  `listEmbeddedBlueprints`/`loadBundledFromFS` reads, which the e2e/test assertions no longer exercise
  for this pack).
- The blueprint name `personal-memory` is unchanged.
