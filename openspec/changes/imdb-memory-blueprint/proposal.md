## Why

The existing IMDb graph seeder lives as a benchmark test inside the core monorepo, making it hard for developers to use it as a starting point for exploring Memory with real-world data. A standalone `imdb-memory-blueprint` repo lets developers point the Memory CLI at a public GitHub URL and instantly populate a project with a rich, well-known dataset — movies, people, and their relationships — without touching internal test infrastructure.

## What Changes

- Create a new standalone repo `emergent-company/imdb-memory-blueprint` at `/root/imdb-memory-blueprint`
- The repo root contains `packs/` and `agents/` directories, making it directly consumable via `memory blueprints https://github.com/emergent-company/imdb-memory-blueprint`
- `packs/` defines the IMDb template pack: object types (`Movie`, `Person`) and relationship types (`ACTED_IN`, `DIRECTED`, `WROTE`)
- `agents/` defines an IMDb seeder agent that downloads, streams, filters, and ingests the public IMDb TSV datasets into a Memory project
- A `README.md` explains how to apply the blueprint and run the seeder against a local or remote Memory server
- The seeder agent reuses the filtering and ingestion logic already proven in the `imdb-graph-seeder` benchmark test (vote threshold, movie-only filter, streaming decompression)

## Capabilities

### New Capabilities

- `imdb-template-pack`: Template pack definition for the IMDb knowledge graph — `Movie` and `Person` object types with their properties, and `ACTED_IN`, `DIRECTED`, `WROTE` relationship types. Delivered as YAML/JSON files under `packs/`.
- `imdb-seeder-agent`: Agent definition that, when triggered, streams and ingests the public IMDb TSV datasets into the current Memory project using the defined template pack types. Delivered as YAML/JSON under `agents/`.
- `blueprint-repo-structure`: The repo scaffold itself — `packs/`, `agents/`, `README.md` — structured so `memory blueprints <source>` can apply it directly.

### Modified Capabilities

*(none — this is a new standalone repo; no existing Memory monorepo specs change)*

## Impact

- **New repo**: `emergent-company/imdb-memory-blueprint` (standalone, not part of the Go monorepo)
- **Memory CLI**: consumed as-is via `memory blueprints`; no CLI changes needed
- **Blueprint format**: must conform to the format expected by `memory blueprints` (`packs/` + `agents/` at root, `.json`/`.yaml`/`.yml` files)
- **Agent runtime**: the seeder agent will be executed by the Memory agent runtime; it calls Memory graph APIs to insert objects and relationships
- **External dependency**: public IMDb TSV datasets at `https://datasets.imdbws.com/` (no auth required)
- **No monorepo changes**: the existing `imdb-graph-seeder` benchmark test is unaffected
