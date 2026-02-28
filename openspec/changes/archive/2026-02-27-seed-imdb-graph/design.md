## Context

We need to seed the Emergent Knowledge Graph with a large, complex dataset (IMDb movie data) to benchmark advanced AI agent architectures (like parallel sub-agents and query routing). Initially, this was proposed as a standalone production CLI tool. However, to keep the production binary clean and align with the goal of testing, we will implement this seeder entirely within the E2E test suite. The test will autonomously fetch, stream, and ingest IMDb data before running complex natural language queries.

## Goals / Non-Goals

**Goals:**

- Implement the IMDb seeder as a dedicated integration test suite (e.g., `imdb_benchmark_test.go`).
- Download, decompress, stream, and filter official IMDb TSV exports on the fly during test execution.
- Ingest ~15k highly-rated movies, ~50k cast/crew members, and ~200k relationships into the graph.
- Execute complex benchmark queries against the agent using this newly populated data.

**Non-Goals:**

- Do not add any new production code, commands, or jobs to `apps/server-go/cmd`.
- Do not run this massive ingestion continuously on standard CI pipelines (it should be an opt-in or separate manual workflow).

## Decisions

1. **Implementation Location**: The logic will reside entirely in `apps/server-go/tests/e2e/imdb_benchmark_test.go`.
2. **Data Streaming**: Instead of downloading gigabytes of TSV files to disk, the test will stream `title.basics.tsv.gz`, `title.ratings.tsv.gz`, and `title.principals.tsv.gz` using standard `net/http` and `compress/gzip`. It will hold the filtered data in memory.
3. **Filtering Heuristic**: We will filter the `title.ratings.tsv` for movies (`titleType == "movie"`) with `numVotes > 20000`. This reduces millions of entries down to a dense, high-quality graph of well-known movies and actors.
4. **Database Insertion**: Because we are inserting ~250,000 entities/relationships, using the standard REST API would be too slow. Since this is an E2E test (where we have direct DB access via `s.DB()`), we will use raw SQL `COPY` or batched `INSERT` statements to populate `kb.graph_objects` and `kb.graph_relationships` incredibly fast.
5. **Embedding Generation**: We will rely on the newly created `EmbeddingSweepWorker` to pick up the NULL embeddings in the background, or we will manually trigger the `EmbedQueryWithUsage` service in a massive concurrent batch to speed up the test.

## Risks / Trade-offs

- **Test Duration**: Downloading, streaming, and generating 250,000 vector embeddings will take a significant amount of time (and cost money via the Gemini Embedding API).
  - _Mitigation_: We will place this test behind a custom environment variable flag (e.g., `RUN_IMDB_BENCHMARK=true`) so it does not block standard CI/CD.
- **Memory Consumption**: Cross-referencing three TSV streams requires caching IDs.
  - _Mitigation_: We will only cache the `tconst` (title IDs) of the filtered ~15k movies, keeping memory usage well under 100MB.
