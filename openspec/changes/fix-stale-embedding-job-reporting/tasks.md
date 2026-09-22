## 1. Shared stale-sweep marker

- [x] 1.1 Add canonical `StaleJobMessage` constant in `apps/server/internal/jobs/stale.go`
- [x] 1.2 Point `domain/scheduler.tasks.go` `staleJobMessage` at the shared constant so sweep and reporting cannot drift

## 2. Reporting split (server)

- [x] 2.1 `GraphEmbeddingJobsService.Stats`: `failed` excludes stale-sweep rows; add `StaleFailed`
- [x] 2.2 `GraphRelationshipEmbeddingJobsService.Stats`: same split
- [x] 2.3 `ChunkEmbeddingJobsService.Stats` and `StatsByProject`: same split
- [x] 2.4 `EmbeddingControlHandler.Progress`: map `staleFailed` into the response
- [x] 2.5 `health.MetricsHandler`: `stale_failed` counter across all five swept tables with the correct per-table error column

## 3. Web UI

- [x] 3.1 Gateway `EmbeddingQueueStats` gains `staleFailed`; empty-state check includes it
- [x] 3.2 Embeddings page renders a "Stale failed" counter with a non-error intent
- [x] 3.3 Gateway tests cover unmarshal and render of the new field

## 4. Retention purge widening

- [x] 4.1 Per-table purge config: terminal statuses + age expression for all six tables
- [x] 4.2 `kb.email_jobs` uses `COALESCE(processed_at, created_at)` and `sent` as terminal
- [x] 4.3 Purge task tests assert table scope, per-table status predicate, and email age expression

## 5. Verification

- [x] 5.1 Synthetic scratch Postgres proves before/after reporting on all five tables
- [x] 5.2 `go build ./...` (server + gateway)
- [x] 5.3 `go test` on touched packages
- [x] 5.4 `golangci-lint` on touched packages
- [x] 5.5 `openspec validate --strict`

## 6. One-off reclaim (not executed)

- [x] 6.1 Document idempotent reclaim SQL with before/after `pg_total_relation_size`
- [x] 6.2 Document VACUUM/REINDEX trade-off and lock/impact expectations
- [x] 6.3 Present for approval; do not run against dev and do not ship as a migration
