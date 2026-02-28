## Why

We need to rigorously benchmark and test the "Natural Language to Graph Query" agent's ability to perform complex multi-hop reasoning, aggregation, and filtering across dense relationships. The current test data is insufficient for advanced architectural testing (like query routing or parallel sub-agents). By importing the official IMDb Daily Exports, we can create a vast, complex, and universally understood Knowledge Graph (movies, actors, directors) to stress-test the agent.

## What Changes

- Introduction of a standalone Go CLI tool (`seed-imdb-graph`) inside the `server-go` application.
- The tool will autonomously download, stream, and decompress official IMDb TSV exports (`title.basics.tsv.gz`, `title.ratings.tsv.gz`, `title.principals.tsv.gz`, `name.basics.tsv.gz`).
- A filtering mechanism to process only highly-rated/famous movies (e.g., >20,000 votes) to keep the graph dense but manageable (resulting in ~15k movies, ~50k people, ~200k relationships).
- Batch insertion of entities (`Movie`, `Person`, `Genre`) and relationships (`ACTED_IN`, `DIRECTED`, `WROTE`, `COMPOSED_MUSIC_FOR`, `PRODUCED`, `IN_GENRE`) into the Emergent Knowledge Graph.

## Capabilities

### New Capabilities

- `imdb-graph-seeder`: A CLI capability to fetch, parse, filter, and ingest IMDb datasets into the Knowledge Graph for benchmarking purposes.

### Modified Capabilities

## Impact

- **Code**: Adds a new CLI command/job to the `server-go` repository.
- **Dependencies**: Utilizes standard Go HTTP, streaming, and decompression (gzip) libraries.
- **Systems**: The database will experience a heavy write load during seeding, which will subsequently trigger the `EmbeddingSweepWorker` to generate ~200,000 embeddings in the background.
