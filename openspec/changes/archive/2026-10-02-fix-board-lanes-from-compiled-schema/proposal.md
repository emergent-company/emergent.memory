## Why

The gateway board (`/board`) derives its lane order from the compiled object
types' `boardEnabled` / `allowedStatuses` work config, but the server's
compiled-types path silently dropped those fields. The flagship
`blueprints/task-board/schemas/task-board.yaml` declares its object types as a
JSON **array**, and `parseObjectTypeSchemasToMap` reconstructed each array entry
with only `properties`/`ui`/`scopeKey`/`label`/`description` — discarding
`boardEnabled`, `allowedStatuses`, and the operational skip flags. As a result
the compiled types carried zero board config and `boardStatusesFromCompiled`
always fell back to the canonical lanes; the schema/compiled preview was blank.

The defect was masked for the `task-board` sample only because its declared
statuses happen to equal the canonical order. A blueprint with custom statuses
(e.g. `todo`, `doing`) rendered canonical lanes instead. The runtime extraction
path (`extraction.normalizeSchemaToMap`) already preserves the whole entry, so
the compiled path was the inconsistent one.

A second, latent bug: `boardStatusesFromCompiled` ignored `Shadowed`, so an
overridden board type could leak stale lane statuses. Every other compiled-type
consumer filters via `visibleCompiledTypes`.

## What Changes

- **Server compiled-types field preservation.** `parseObjectTypeSchemasToMap`
  (array branch) now carries `boardEnabled`, `allowedStatuses`,
  `skipEmbeddings`, `skipExtraction`, and `excludeFromSearch` through to the
  reconstructed per-type schema object, matching what the runtime extraction
  normalisation already preserves. Compiled types now return the declared board
  config for both array- and map-form stored object-type schemas.
- **Gateway shadowed-type filtering.** `boardStatusesFromCompiled` skips
  shadowed (losing) duplicates via `visibleCompiledTypes`, consistent with the
  schema browser, so an overridden board type cannot leak stale lanes.
- **Regression coverage.** A server-side test parses the array-form
  `task-board` object type and asserts the compiled type carries
  `boardEnabled` / `allowedStatuses` / skip flags; a gateway test asserts a
  shadowed board type's statuses are excluded from the lane order.

Preserved good behaviour: the empty/missing-schema fallback to the canonical
lanes and unknown-status preservation in the gateway; no hook removals;
`/blueprints`, `/schema`, `/proposal` unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `blueprint-api`: the compiled-types path must preserve the board work config
  declared on array-form (blueprint) object-type schemas.
- `object-driven-agent-work`: the Kanban projection derives its lane order from
  the compiled schema's board-enabled allowed statuses, falling back to the
  canonical order only when the schema declares none, and excludes shadowed
  types.

## Impact

- **Server** (`apps/server/domain/schemas/repository.go`):
  `parseObjectTypeSchemasToMap` array branch carries the work-config fields.
- **Gateway** (`apps/web-ui/gateway/board.go`): `boardStatusesFromCompiled`
  filters shadowed types.
- **Tests**: `apps/server/domain/schemas/repository_test.go`,
  `apps/web-ui/gateway/board_test.go`.

## Out of Scope

- Reconciliation of the remaining manifest-only fields (`labels`, `embedding`,
  `extraction`) between the two normalisation paths — those are not surfaced on
  `ObjectTypeSchema` and do not affect compiled output.
- Any change to runtime extraction behaviour or the schema registry contract.
