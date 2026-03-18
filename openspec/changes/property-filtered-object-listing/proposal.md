## Why

`memory graph objects list` has no property-level filtering flag, forcing users to fetch all objects of a type and filter client-side. The server API (`property_filters`) and Go SDK (`PropertyFilters []PropertyFilter`) already support this — only the CLI layer is missing it.

## What Changes

- Add `--filter` flag (repeatable) to `memory graph objects list`, accepting `key=value` shorthand
- Add `--filter-op` flag for advanced operators (`eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `contains`, `in`, `exists`)
- Wire CLI flags → existing `sdkgraph.ListObjectsOptions.PropertyFilters` field
- Update help text and CLI reference docs

## Capabilities

### New Capabilities

- `graph-objects-property-filter`: `--filter` and `--filter-op` flags on `memory graph objects list` that translate to server-side `property_filters` query params

### Modified Capabilities

<!-- No existing spec covers graph object listing CLI flags -->

## Impact

- **CLI**: `tools/cli/internal/cmd/graph.go` — `graphObjectsListCmd` flag parsing and `ListObjectsOptions` wiring
- **SDK**: No changes needed — `PropertyFilter` struct and `ListObjects` already handle this
- **API**: No changes needed — `property_filters` param already implemented in handler and store
- **Docs**: CLI reference skill (`tools/cli/internal/skillsfs/skills/memory-cli-reference/SKILL.md`)
