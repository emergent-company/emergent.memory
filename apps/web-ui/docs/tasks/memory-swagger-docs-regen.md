# Regenerate Emergent Memory swagger docs

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-object-schema-widgets](../sessions/2026-09-08-object-schema-widgets.md)

## What

Regenerate `apps/server/docs/swagger/{docs.go,swagger.json,swagger.yaml}` in the
emergent-company/emergent.memory repo so the API docs reflect current handlers/entities
(including `ObjectTypeSchema.ui` added in PR #385).

## Why

Both `swag v1.16.6` and `swag/v2 v2.0.0-rc5` (both pinned in go.mod) produce thousands of drift
lines against the committed docs — a pre-existing generator/version mismatch that made updating
docs alongside #385 impractical. The committed docs are therefore stale. Needs a one-off
reconciliation (pick the generator that reproduces the committed output closely, regenerate, and
review the diff) so future DTO changes can include doc updates.

## Depends on

- Repo's swagger tooling (Taskfile `swagger` target: `swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal`).

## Notes

- Cross-repo task (emergent-company/emergent.memory, at `/root/emergent.memory`), tracked here for
  visibility; not part of the alfred gateway.
