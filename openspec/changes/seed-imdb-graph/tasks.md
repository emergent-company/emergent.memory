## 1. Test Suite Setup

-[x] 1.1 Create `imdb_benchmark_test.go` in `apps/server-go/tests/e2e/`
-[x] 1.2 Define `IMDBBenchmarkSuite` struct extending `testutil.BaseSuite`
-[x] 1.3 Add `SetupSuite` method that checks for the `RUN_IMDB_BENCHMARK=true` environment variable and skips the test if absent
-[x] 1.4 Setup project and API key configuration for the test agent

## 2. Data Streaming and Filtering Implementation

-[x] 2.1 Implement `streamIMDBFile(url string) (*gzip.Reader, error)` helper function to download and decompress TSV streams on the fly
-[x] 2.2 Implement parser for `title.ratings.tsv.gz` to extract a set of `tconst` IDs where `numVotes > 20000`
-[x] 2.3 Implement parser for `title.basics.tsv.gz` that cross-references the filtered `tconst` set and extracts movie titles, release years, and runtime
-[x] 2.4 Implement parser for `name.basics.tsv.gz` to extract actor/director names and birth years
-[x] 2.5 Implement parser for `title.principals.tsv.gz` to extract relationships (actor, director, writer) between filtered titles and names

## 3. Database Ingestion Pipeline

-[x] 3.1 Create bulk `INSERT` or `COPY` SQL statements for inserting `kb.graph_objects` (Movies and Persons) quickly
-[x] 3.2 Create bulk `INSERT` or `COPY` SQL statements for inserting `kb.graph_relationships` (`ACTED_IN`, `DIRECTED`, etc.) quickly
-[x] 3.3 Execute the batch ingestion within the test setup using `s.DB()`
-[x] 3.4 Print metrics on the number of objects and relationships successfully ingested

## 4. Benchmark Query Tests

-[x] 4.1 Write `TestBenchmark_ComplexTraversal` to ask the agent: "Find me movies from the 1990s directed by Steven Spielberg"
-[x] 4.2 Write `TestBenchmark_ActorIntersection` to ask the agent: "Did Tom Hanks and Meg Ryan ever act in the same movie?"
-[x] 4.3 Write `TestBenchmark_GenreAndRating` to ask the agent: "What are the top rated Sci-Fi movies released after 2010?"
-[x] 4.4 Verify that the agent correctly parses the SSE stream, invokes the right tools, and returns accurate answers based on the ingested graph
