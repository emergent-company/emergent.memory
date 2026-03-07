## Why

Blueprints currently support applying template packs and agent definitions, but there is no way to seed a project with initial entities (graph objects) and relationships. Users who want to bootstrap a knowledge graph — for example, creating sample data alongside a template pack, or bulk-importing a large domain dataset — must do so manually via the API or UI. Adding a `seed/` directory to the Blueprint format closes this gap.

## What Changes

- New `seed/` directory convention in the Blueprint directory structure (alongside `packs/` and `agents/`)
- Each file in `seed/` defines a list of graph objects and/or relationships to create
- Files support `.json`, `.yaml`, `.yml` formats consistent with existing blueprint loaders
- `memory blueprints` CLI command processes `seed/` files after packs and agents
- Objects are created via bulk API; relationships are created after all objects are upserted (so references can be resolved by key)
- `--dry-run` previews seed operations without making API calls
- `--upgrade` flag controls whether existing objects (matched by `key`) are updated or skipped
- Summary output reports seed counts alongside pack/agent counts
- New `memory blueprints dump` subcommand exports existing graph objects and relationships from a project into a seed file, enabling round-trip workflows (dump → edit → apply)

## Capabilities

### New Capabilities

- `blueprint-seed-data`: Define seed data (graph objects and relationships) in a Blueprint directory that gets applied alongside template packs and agents. Supports large datasets via bulk API calls, key-based deduplication, and the same dry-run/upgrade flags as existing blueprint resources.
- `blueprint-seed-dump`: Export existing graph objects and relationships from a project into a seed file compatible with the `seed/` format. Supports filtering by object type, and outputs JSON or YAML.

### Modified Capabilities

- `template-packs`: No requirement changes — blueprints remain the primary installation mechanism; seed data is additive to the existing blueprint apply flow.

## Impact

- **CLI** (`tools/cli/internal/blueprints/`): new `SeedFile` type, loader for `seed/` directory, seeder applier using graph SDK bulk APIs, new `dump` subcommand using `ListObjects` / `ListRelationships` with cursor pagination
- **Graph SDK** (`apps/server/pkg/sdk/graph/`): used as-is via existing bulk create, `ListObjects`, and `ListRelationships`; no server changes needed
- **No breaking changes**: existing blueprint directories without a `seed/` directory are unaffected
- **Dependencies**: no new external dependencies; re-uses existing `graph.Client` already available in the CLI SDK
