## 1. Graph provenance plumbing

- [x] 1.1 Add `ExtractionJobID *uuid.UUID` to `graph.CreateGraphObjectRequest` (`apps/server/domain/graph/dto.go`)
- [x] 1.2 `graph.Service.Create`: set `obj.ExtractionJobID = req.ExtractionJobID`
- [x] 1.3 `graph.Service.CreateOrUpdate`: set `ExtractionJobID` on the new-object branch, the deleted-restore branch, and the changed-properties branch; in the no-op branch, write the column when the existing row is NULL and a request id is supplied
- [x] 1.4 `graph.Repository.CreateVersion`: inherit `prevHead.ExtractionJobID` when the new version supplies none
- [x] 1.5 `graph.BranchObjectHead`: add `ExtractionJobID`; select it in `GetBranchObjectHeads`
- [x] 1.6 `applyMerge` "added" clone: copy `src.ExtractionJobID`
- [x] 1.7 `BulkCopyObjectsToBranch`: copy `obj.ExtractionJobID`
- [x] 1.8 `SoftDeleteOnBranch`: copy `mainHead.ExtractionJobID`
- [x] 1.9 `extraction.persistResults`: pass parsed `job.ID` as `ExtractionJobID` on both `CreateOrUpdate` and `Create` object calls

## 2. Provenance backfill migration

- [x] 2.1 Add Goose migration backfilling `kb.graph_objects.extraction_job_id` from `properties->>'_extraction_job_id'` where valid uuid and a matching `kb.object_extraction_jobs` row exists
- [x] 2.2 Verify `task migrate:status` and apply locally

## 3. Deletion cascade and impact correctness

- [x] 3.1 Add a shared provenance-resolution helper used by deletion and impact (job ids -> attributable rows -> fully-removed canonicals -> object rows to delete -> relationships to delete)
- [x] 3.2 Rewrite `Repository.DeleteWithCascade` to use canonical-aware resolution and report accurate counts
- [x] 3.3 Rewrite `Repository.GetDeletionImpact` to mirror the delete resolution
- [x] 3.4 Rewrite `Repository.BulkDeleteWithCascade` to use the same resolution over the document id set
- [x] 3.5 Rewrite `Repository.GetBulkDeletionImpact` to mirror bulk delete resolution (remove proportional approximation)
- [x] 3.6 Add `project_id` scoping to the graph-object lookups

## 4. Verification

- [x] 4.1 `go build ./...` from `gateway`/module root as applicable; server build via `task build`
- [x] 4.2 Unit or integration test covering: object extraction stamps provenance; delete impact non-zero; cascade removes objects and relationships; entity with a surviving version is retained
- [x] 4.3 `task lint`
- [x] 4.4 `task test`

## 5. PR review follow-ups

- [x] 5.1 Keep `ExtractionJobID` internal-only (`json:"-"`), never client-bindable
- [x] 5.2 Propagate staging-source provenance through fast-forward/conflict/similar merge versions
- [x] 5.3 Repair surviving version chains (clear dangling `supersedes_id`, guarantee one HEAD)
- [x] 5.4 Batch bulk deletion-impact graph resolution (constant queries)
- [x] 5.5 Add `(project_id, extraction_job_id)` index to migration 00157
