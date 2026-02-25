## Context

The IMDB benchmark (`imdb_benchmark_test.go`) proves that the graph seeding pipeline works correctly at scale by seeding real IMDB data and then running NL agent queries with known expected answers. However, it exercises the graph-import path, not the document extraction path.

The extraction pipeline — upload a document → run an extraction job → produce graph objects/relationships — has no equivalent benchmark. Existing extraction tests (`extraction_test.go`) verify that the API returns success but do not measure extraction _quality_. There is no test that compares what was extracted to what should have been extracted.

The key challenge is defining ground truth. A real document (e.g., a Wikipedia article) is hard to score against because the ground truth is ambiguous. A synthetic document generated from a fixed, known dataset is unambiguous: every entity and relationship written into the document is exactly what should be extracted.

## Goals / Non-Goals

**Goals:**

- Ship as a standalone Go command (`cmd/extraction-bench/main.go`), not a test file — invoked directly with `--host` and `--api-key` flags
- Generate a deterministic synthetic document from a small, hardcoded IMDB-style dataset (movies, people, relationships)
- Upload the document via the real upload API and trigger a real extraction job
- Poll for completion and query the resulting graph objects/relationships
- Score precision and recall of extracted entities and relationships against the known ground truth
- Log a structured comparison table showing matched, missing, and spurious entities per run
- Append a JSONL run log (like `imdb-bench`) so results can be compared across runs and server versions
- Be runnable multiple times with comparable results
- Exit non-zero if results fall below configurable minimum thresholds (flags or env vars)

**Non-Goals:**

- Being a Go test file (`*_test.go`) — this is an explicitly invoked CLI tool, not part of `go test`
- Generating documents from live IMDB data downloads (determinism requires a fixed embedded dataset)
- Testing extraction of images, PDFs, or non-text formats (plain text is sufficient for establishing the baseline)
- Modifying any production code
- Replacing the existing `extraction_test.go` functional tests (this is additive)
- Measuring LLM latency or token usage

## Decisions

### Decision 0: Standalone CLI script, not a Go test file

**Choice:** Implement as `apps/server-go/cmd/extraction-bench/main.go` — a `package main` binary invoked directly with CLI flags (`--host`, `--api-key`, `--project-id`), following the pattern of `cmd/imdb-bench/main.go`.

**Rationale:** A test file requires a specific env gate, lives inside the test binary lifecycle, and can't easily accept explicit flags at invocation time. A standalone script is more ergonomic for manual benchmarking: `go run ./cmd/extraction-bench/ --host https://api.dev.emergent-company.ai --api-key emt_xxx --project-id yyy`. It also naturally separates benchmark tooling from the test suite. The `imdb-bench` cmd establishes this pattern already.

**Alternative considered:** `*_test.go` with env var gate (original approach). Rejected because the user explicitly wants explicit host + API key flags and a directly runnable script.

### Decision 1: Hardcoded synthetic dataset, not live IMDB download

**Choice:** Embed a small fixed dataset of ~10 movies, ~15 people, and ~25 relationships directly in the test file as Go struct literals.

**Rationale:** The IMDB benchmark downloads live data, which means the dataset changes over time. For a benchmark that measures extraction quality and allows run-to-run comparison, the input document must be byte-identical across runs. Using a live download would introduce drift. The dataset is small enough (~10 movies) that embedding it inline costs nothing.

**Alternative considered:** Downloading a small fixed slice of IMDB data and caching it to disk. Rejected because caching adds complexity and a cached file may differ between environments.

### Decision 2: Prose document format, not structured data

**Choice:** The synthetic document is plain English prose — paragraphs describing each movie, its cast, director, and writer — not JSON, CSV, or a table.

**Rationale:** The extraction pipeline is designed to work on unstructured text (e.g., Wikipedia articles, PDFs, reports). A structured input document would make extraction trivially easy and not representative of real use. Prose is closer to what users actually upload, so it's a better signal of real-world performance.

**Alternative considered:** A semi-structured format (bullet lists, markdown tables). Rejected because prose is harder and more realistic.

### Decision 3: Three schema types — Movie, Person, and three relationship types

**Choice:** Extraction schema mirrors the IMDB benchmark graph schema:

- Object types: `Movie` (title, year, genre, runtime) and `Person` (name, birth year)
- Relationship types: `ACTED_IN` (Person→Movie), `DIRECTED` (Person→Movie), `WROTE` (Person→Movie)

**Rationale:** This is already the canonical domain used in the IMDB benchmark, so anyone familiar with one test immediately understands the other. Reusing the same types also means the extraction schema can be validated against the existing graph schema for consistency.

**Alternative considered:** A completely different domain (e.g., biomedical). Rejected for being unfamiliar and harder to reason about.

### Decision 4: Fuzzy name matching for scoring

**Choice:** When comparing extracted objects to ground truth, use case-insensitive substring matching on the primary identifying property (movie title or person name) rather than exact equality.

**Rationale:** LLMs may slightly alter capitalization, punctuation, or article usage (e.g., "The Dark Knight" vs "Dark Knight"). Exact matching would produce artificially low precision/recall. Substring matching captures near-misses without false positives.

**Alternative considered:** Embedding-based similarity. Rejected as overkill; substring matching is transparent and deterministic.

### Decision 5: Upload via multipart form, not manual DB insertion

**Choice:** Use `POST /api/documents/upload` with a multipart form to upload the synthetic document, then reference the returned document ID in the extraction job request.

**Rationale:** This exercises the full end-to-end path including storage. The existing `extraction_test.go` uses `testutil.CreateTestDocument` to bypass upload and inject directly into the DB — that path doesn't test the upload pipeline. A benchmark should exercise the real path.

**Consequence:** The test requires a live server with object storage configured (`BENCHMARK_SERVER_URL`), same as the IMDB benchmark.

### Decision 6: Precision/recall scoring with configurable thresholds

**Choice:** Compute entity precision, entity recall, relationship precision, and relationship recall. Print a formatted table to stdout. Exit non-zero if results fall below thresholds configurable via flags (defaults: 60% entity recall, 50% entity precision, 50% relationship recall, 40% relationship precision). Append a JSONL run record to a log file (default: `docs/tests/extraction_bench_log.jsonl`) for cross-run comparison.

**Rationale:** Thresholds allow the script to serve as both a quality gate (exit code in CI) and a regression tracker (log file). Low defaults reflect that extraction quality can vary by model and prompt version. The JSONL log pattern is already established by `imdb-bench`.

## Risks / Trade-offs

- **LLM non-determinism** → Even with a fixed document, extraction results may vary slightly run-to-run. Mitigation: use fuzzy matching and percentage thresholds rather than exact counts; JSONL log enables trend analysis.
- **Storage dependency** → Script requires a configured object storage backend (S3/MinIO) on the target server. Mitigation: document this clearly in the usage comment; same constraint as `imdb-bench`.
- **Extraction timeout** → Large documents or slow model responses could cause the poll timeout to expire. Mitigation: the synthetic document is deliberately small (~600 words) to keep extraction fast.
- **No extraction SDK client** → The SDK has no `extractionjobs` package. The script will call `POST /api/admin/extraction-jobs` and `GET /api/admin/extraction-jobs/{id}` directly via `net/http`, matching the pattern used in `extraction_test.go`.
- **Schema drift** → If the extraction job or graph API changes its response shape, JSON parsing will break. Mitigation: use loose `map[string]any` parsing so it's easy to update.
- **False precision from partial matches** → Fuzzy matching could match "John" to "John Smith" and "John Doe" both, inflating precision. Mitigation: require the full identifying token to appear as a substring, not just any partial overlap.
