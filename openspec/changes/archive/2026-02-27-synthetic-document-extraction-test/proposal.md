## Why

The extraction pipeline has no benchmark test that verifies end-to-end quality — uploading a real document, running extraction, and measuring how accurately objects and relationships are recovered. Without a known ground truth, it's impossible to compare extraction results across runs or detect regressions. We need a repeatable, deterministic benchmark (modeled after the IMDB graph benchmark) that generates a synthetic document from a fixed set of known entities and relationships, uploads it, kicks off extraction, and scores precision/recall against the ground truth.

## What Changes

- Add a new opt-in e2e benchmark test suite (`RUN_EXTRACTION_BENCHMARK=true`) in `apps/server-go/tests/e2e/`
- Implement a synthetic document generator that produces a readable text document from a fixed set of IMDB-style `Movie`, `Person`, and relationship data (ACTED_IN, DIRECTED, WROTE)
- The generator produces a deterministic document so tests can be run repeatedly and results compared
- The test uploads the document via `POST /api/documents/upload`, triggers extraction via `POST /api/admin/extraction-jobs`, polls to completion, then queries graph objects/relationships
- Scoring logic computes precision and recall against the known ground truth and logs a structured results table
- Test accepts configurable thresholds (env vars) so it can be used both as a pass/fail gate and as a regression tracker

## Capabilities

### New Capabilities

- `extraction-benchmark-test`: An opt-in e2e benchmark test that generates a synthetic IMDB-style document, uploads it, runs extraction, and scores the result against known ground truth using precision/recall metrics

### Modified Capabilities

_(none — this is a new test capability; no existing spec requirements change)_

## Impact

- **New file**: `apps/server-go/tests/e2e/extraction_benchmark_test.go`
- **Touches**: document upload API (`/api/documents/upload`), extraction job API (`/api/admin/extraction-jobs`), graph objects API (`/api/graph/objects`, `/api/graph/relationships`)
- **Dependencies**: Requires a live server with storage configured (same as IMDB benchmark — runs against `BENCHMARK_SERVER_URL`, opt-in via env var)
- **No production code changes** — test-only
