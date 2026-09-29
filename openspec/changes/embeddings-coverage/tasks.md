## 1. Backend — indexes

- [x] 1.1 Add `apps/server/migrations/00201_graph_embedding_coverage_indexes.sql` with partial indexes `(project_id)` on `kb.graph_objects WHERE embedding_v2 IS NOT NULL AND deleted_at IS NULL` and `kb.graph_relationships WHERE embedding IS NOT NULL AND deleted_at IS NULL`, plus the matching `-- +goose Down` drop. Follow the conventions in `apps/server/migrations/README.md`. Verify: `go build ./...` from `apps/server/`.

## 2. Backend — coverage query + endpoint

- [x] 2.1 Add `CoverageByProject(ctx, projectID)` (and a global `Coverage(ctx)`) to `GraphEmbeddingJobsService` (`graph_embedding_jobs.go`) returning embedded/awaiting counts for live objects.
- [x] 2.2 Same for `GraphRelationshipEmbeddingJobsService` (`graph_relationship_embedding_jobs.go`) using `embedding` (not `embedding_v2`).
- [x] 2.3 Add `EmbeddingCoverage`, `EmbeddingCoverageResponse`, and the `Coverage` handler in `embedding_control_handler.go`; mirror `Progress` authorization (project-scoped; deployment-wide only for active `superadmin_full`, else 403). Wire counts through a fold helper mirroring `embeddingProgressResponse`.
- [x] 2.4 Register `read.GET("/coverage", h.Coverage)` in `embedding_control_routes.go` and extend the group comment.
- [x] 2.5 Regenerate swagger (`task server:swagger`) and commit `apps/server/docs/swagger/*`.
- [x] 2.6 Tests: DB-backed coverage tests for both services (all-embedded, partially-awaited, project-scoping) mirroring `graph_relationship_embedding_stats_test.go`; handler test for 200 shape + 403 without project context. Verify `go test ./domain/extraction/... -count=1`.

## 3. Gateway — coverage section + empty state

- [x] 3.1 `embeddings.go`: add `EmbeddingCoverage`/`EmbeddingCoverageResponse` types and `GetEmbeddingCoverage(ctx)` (GET `/api/embeddings/coverage`), mirroring the existing progress/status client methods.
- [x] 3.2 `uiEmbeddings`: add a fourth parallel fetch with independent degradation (`CoverageErr`), and carry coverage + awaiting-derived state into `embeddingPageData`.
- [x] 3.3 `embeddings.templ`: render a "Coverage" section (Embedded / Awaiting / Total per queue; objects + relationships). Reword the queue empty state so an idle, fully-embedded project reads as "no embedding work pending" with the embedded counts, and only a genuine no-data case keeps a terse empty state. Coverage fetch failure shows a coverage-only error.
- [x] 3.4 Extend `embeddings_test.go`: coverage present, all-embedded copy, coverage fetch failure degrades independently, queue empty-state selection.
- [x] 3.5 Verify from `apps/web-ui/gateway`: `templ generate` (if needed), `go build ./...`, `go test ./...`, `task lint`.

## 4. Verification

- [x] 4.1 `openspec validate embeddings-coverage --strict` passes.
- [x] 4.2 Server: `go build ./...`, `go vet ./...`, `go test ./... -count=1` from `apps/server`.
- [x] 4.3 Gateway: `go build ./...`, `go test ./...`, `task lint` from `apps/web-ui/gateway`.
- [x] 4.4 Coverage query plan confirmed cheap on a large project (EXPLAIN shows the partial indexes; no seq scan of `graph_objects` / `graph_relationships`).
- [ ] 4.5 Manual (after dev deploy): `/embeddings` for Norwegian Law shows embedded/total coverage and no longer reads "No embedding statistics yet".
