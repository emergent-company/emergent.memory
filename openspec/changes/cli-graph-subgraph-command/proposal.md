## Why

`CreateSubgraph` already exists on the server — it atomically creates objects and relationships in one call using client-side `_ref` placeholders instead of UUIDs. The CLI has no way to use it, forcing agents into a slow, fragile two-pass workflow: batch-create objects, capture stdout IDs, then batch-create relationships separately. Extending `memory graph objects create-batch` to detect and route the richer format eliminates the ID-capture problem without introducing new terminology.

## What Changes

- Extend `memory graph objects create-batch` to accept two input formats:
  - **Flat array** (existing): `[{"type": "...", ...}]` — routes to existing batch endpoint, unchanged behaviour
  - **Subgraph format** (new): `{"objects": [...], "relationships": [...]}` with `_ref` placeholders — routes to `POST /api/graph/subgraph`
- Print created object/relationship counts and the `ref_map` (placeholder → UUID) on success for subgraph format
- Support `--output json` for machine-readable output in both formats
- Update `memory-graph` skill to show the subgraph format as the preferred approach when relationships are involved

## Capabilities

### New Capabilities

- `cli-graph-create-batch-subgraph`: Extended `create-batch` that detects `{"objects", "relationships"}` shape and routes to `POST /api/graph/subgraph`, supporting `_ref` placeholders and returning a `ref_map`

### Modified Capabilities

- `memory-graph-skill`: Add subgraph format as the primary recommended pattern for any population that includes relationships, with a worked example and chunking strategy for >100 objects

## Impact

- **CLI**: `graphObjectsCreateBatchCmd` in `tools/cli/internal/cmd/graph.go` — format detection, new routing branch
- **Server**: No changes — `POST /api/graph/subgraph` (`CreateSubgraph`) already implemented, max 100 objects + 200 relationships per call
- **Skills**: `memory-graph` SKILL.md updated in `tools/cli/internal/skillsfs/skills/memory-graph/` (synced to `.agents/skills/` and legalplant-api)
