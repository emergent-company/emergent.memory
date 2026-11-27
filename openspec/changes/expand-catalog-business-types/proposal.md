## Why

Discovery aligns a proposed object type against the built-in catalog before inventing a new one. Common business entities have no canonical home today: a role or position a person holds, a hardware device, an architectural or business decision, and energy-storage equipment all fall through to ad-hoc names ("Decision", "EnergyStorageTechnology"). Adding these as schema.org-first catalog types, each with labels (aliases), lets discovery converge on a stable name and property shape instead of inventing a near-duplicate, and lets the discovery eval scorer count a correct synonym as a hit rather than a miss.

## What Changes

- Add `Role` (schema.org/Role) and `Device` (schema.org/Device) to the core schema.org pack.
- Add a `business` domain pack defining `DecisionRecord`, `Equipment`, and `StorageSystem`, loaded automatically from `data/*.yaml`.
- Bump the catalog version to 1.3.0 (five new object types).
- Make the discovery eval type matcher alias-aware so a synonymous discovered type counts toward its canonical golden type.

## Capabilities

### New Capabilities

- `catalog-business-types`: Business object types in the catalog — `Role`, `Device`, `DecisionRecord`, `Equipment`, and `StorageSystem` — each with schema.org-aligned properties, labels (aliases), and alias-aware discovery alignment.

### Modified Capabilities

<!-- none — this is additive only -->

## Impact

- `apps/server/domain/catalog/data/core.yaml` — add `Role` and `Device` object types
- `apps/server/domain/catalog/data/business.yaml` — new domain pack (`DecisionRecord`, `Equipment`, `StorageSystem`)
- `apps/server/domain/catalog/data/00-manifest.yaml` — version 1.2.0 → 1.3.0
- `apps/server/domain/catalog/catalog_test.go` — object-type count 66 → 71
- `apps/server/cmd/pipeline-eval/main.go` — alias-aware `scoreTypes` matching
