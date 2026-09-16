## Why

Alfred's knowledge graph schema is fixed at install time: the two bundled blueprint packs
(`personal-memory`, `agent-notes`) are installed once and there is no surface to add, remove,
inspect, or toggle the schema packs that define what the graph can store. Emergent Memory
already ships a full blueprint system (install/remove/toggle/dynamic schema extension, no
restart), but Alfred exposes none of it. This change adds a blueprint gallery so users can
manage and extend the graph schema from the web UI.

## What Changes

- New **backend API** (`/api/blueprints*`) in the Go gateway that proxies Emergent Memory's
  schema/blueprint endpoints: list installed packs, list available packs, install (assign) a
  pack, register a new pack, toggle a pack active, remove (soft-delete) a pack, and view the
  project's compiled object/relationship types.
- New **web UI page** (`/blueprints`) with an "Installed" view (list, toggle active, remove,
  inspect types) and an "Available" view (registry packs not yet installed plus the bundled
  `blueprints/` directory packs, with one-click install).
- New sidebar navigation entry "Blueprints" in the Alfred web shell.
- New `MemoryClient` methods + `MemoryBackend` interface entries following the existing
  `do`/`successEnvelope` pattern.
- **Token scope expansion** — the gateway's memory token needs `schema:write` (and
  `schema:migrate` for migration actions) to drive install/remove; this is a deployment/config
  change, not code.

## Capabilities

### New Capabilities

- `blueprint-api`: The gateway HTTP API surface (`/api/blueprints*`) that lists, installs,
  registers, toggles, and removes blueprint packs, and returns compiled schema types. Defines
  request/response shapes and error semantics.
- `blueprint-gallery`: The web UI behavior — sidebar entry, Installed/Available views, install
  flow, toggle/remove interactions, and compiled-type inspection.

### Modified Capabilities

<!-- none -->

## Impact

- **Code**: `gateway/memory.go` (new `MemoryClient` methods + structs), `gateway/backend.go`
  (`MemoryBackend` interface), `gateway/blueprints_handlers.go` (new), `gateway/ui.go`
  (`sidebarGroups`, `uiBlueprints` handler), `gateway/blueprints.templ` (new), `gateway/main.go`
  (routes + nav). Tests: `gateway/blueprints_handlers_test.go` (new, fake `MemoryBackend`),
  `gateway/memory_test.go` (new client-method cases).
- **APIs**: consumes Emergent Memory HTTP schema endpoints
  (`GET /api/schemas/projects/:pid/installed`, `/available`, `/compiled-types`;
  `POST /api/schemas`; `POST /api/schemas/projects/:pid/assign`;
  `PATCH`/`DELETE /api/schemas/projects/:pid/assignments/:id`).
- **Dependencies**: none new (reuses existing echo/templ/go-daisy stack).
- **Config**: memory `emt_*` token must gain `schema:write` (+ `schema:migrate` for migrations).
