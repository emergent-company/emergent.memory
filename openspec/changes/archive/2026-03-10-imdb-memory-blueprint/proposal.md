## Why

The existing IMDb graph seeder lives as a benchmark test inside the core monorepo, making it hard for developers to use it as a starting point for exploring Memory with real-world data. A standalone `imdb-memory-blueprint` repo lets developers point the Memory CLI at a public GitHub URL and instantly install the IMDb template pack, then run a Go seeder script to populate their project with a rich, well-known dataset — movies, people, and relationships.

## What Changes

- Create a new standalone repo `emergent-company/imdb-memory-blueprint` at `/root/imdb-memory-blueprint`
- `packs/` defines the IMDb template pack: `Movie` and `Person` object types with their properties, and `ACTED_IN`, `DIRECTED`, `WROTE` relationship types — applied via `memory blueprints <source>`
- `cmd/seeder/main.go` is a standalone Go program ported from the existing `imdb-graph-seeder` benchmark test — streams IMDb TSV datasets, filters, and inserts objects + relationships directly into a Memory project via the graph API
- `README.md` explains how to apply the blueprint and run the seeder
- No `seed/` directory, no agent definitions — this is a v1 port, not the future seed-format integration

## Capabilities

### New Capabilities

- `imdb-template-pack`: Template pack definition for the IMDb knowledge graph — `Movie` and `Person` object types with their properties, and `ACTED_IN`, `DIRECTED`, `WROTE` relationship types. Delivered as YAML/JSON files under `packs/`.
- `imdb-seeder-cli`: Standalone Go program (`cmd/seeder/main.go`) that streams the public IMDb TSV datasets, applies quality filters (movies only, >20k votes), and inserts objects and relationships into a Memory project via the graph API. Ported from the existing `imdb-graph-seeder` benchmark test.
- `blueprint-repo-structure`: The repo scaffold — `packs/`, `cmd/seeder/`, `README.md` — structured so `memory blueprints <source>` installs the template pack, and developers run the seeder separately.

### Modified Capabilities

*(none — standalone repo, no existing Memory monorepo specs change)*

## Impact

- **New repo**: `emergent-company/imdb-memory-blueprint` (standalone, not part of the Go monorepo)
- **Memory CLI**: `memory blueprints` used as-is to install the template pack; no CLI changes needed
- **Seeder**: calls Memory graph API directly (requires a project token and server URL, passed as flags or env vars)
- **External dependency**: public IMDb TSV datasets at `https://datasets.imdbws.com/` (no auth, used at seeder runtime)
- **Source**: logic ported from `apps/server/domain/graph/imdb_test.go` (or equivalent benchmark location)
- **No monorepo changes**: the existing `imdb-graph-seeder` benchmark test is unaffected
- **No dependency** on `blueprint-seed-data` — this is a self-contained v1
