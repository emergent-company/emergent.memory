## Why

The `/embeddings` page shows only embedding **job-queue** counters. Terminal job rows are purged after 7 days (`EmbeddingJobPurgeTask`), so a project that is fully embedded and idle reports every counter as zero and renders "No embedding statistics yet" — indistinguishable from a project that has never processed anything. A real, fully-embedded project (Norwegian Law: 107,233 objects / 81,919 relationships, all with vectors) looks broken.

The page needs an inventory signal alongside the queue signal: how many objects/relationships already hold vectors versus how many are still awaiting one.

## What Changes

- Add `GET /api/embeddings/coverage` (project-scoped, same auth posture as `/status` and `/progress`) returning `embedded` / `awaiting` / `total` per queue for objects and relationships.
- Add two partial btree indexes (`(project_id) WHERE embedding IS NOT NULL AND deleted_at IS NULL`) so coverage counts are exact and cheap; the existing `*_missing_embedding` partial indexes already serve the `awaiting` side.
- Gateway: fetch coverage alongside progress/status/model-config (independent degradation), render a "Coverage" section, and make the queue empty state coverage-aware — an idle, fully-embedded project reads "no embedding work pending", not "no statistics yet".

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `embedding-status`: add an embedding-coverage requirement and a coverage-backed endpoint; make the queue empty state distinguish "idle, fully embedded" from "no data".

## Impact

**Files (`apps/server/`):**
- `migrations/00201_graph_embedding_coverage_indexes.sql` — embedded-side partial indexes.
- `domain/extraction/graph_embedding_jobs.go` — `CoverageByProject` (objects).
- `domain/extraction/graph_relationship_embedding_jobs.go` — `CoverageByProject` (relationships).
- `domain/extraction/embedding_control_handler.go` — `EmbeddingCoverageResponse`, `Coverage` handler.
- `domain/extraction/embedding_control_routes.go` — register `GET /coverage` in the read group.
- `docs/swagger/*` — regenerated.

**Files (`apps/web-ui/gateway`):**
- `embeddings.go` — coverage client types + `GetEmbeddingCoverage`; page data/err wiring.
- `embeddings.templ` — coverage section + coverage-aware empty state.
- `embeddings_test.go` — contract tests.

## Non-Goals

- No change to the purge task or terminal-row retention.
- No chunk/document coverage on the embeddings page (objects + relationships only, matching the page).
- No new worker controls or config.
