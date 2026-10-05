## 1. Gateway data layer

- [x] 1.1 Add `ExtractionJob` type and `ListExtractionJobs(ctx, documentID)` to `memory_documents.go` (GET `/api/admin/extraction-jobs/projects/{projectID}?source_id={id}&source_type=document`).
- [x] 1.2 Add `DownloadDocument(ctx, id)` to `memory_documents.go`: GET `/api/documents/{id}/download`, follow the 307 to the signed URL, return a body reader + content type (mirror `GetAvatar`).
- [x] 1.3 Add `GetMergeReadiness(ctx, branchID)` and `MergeBranch(ctx, sourceBranchID, policy)` to `memory_branches.go` (GET `/api/graph/branches/{id}/merge-readiness`; POST `/api/graph/branches/main/merge` with `{source_branch_id, execute:true, policy:"partial"}`).
- [x] 1.4 Extend `Branch` with `MergedAt` (and any display fields) so an already-merged branch can be recognised — also added `BranchResponse.MergedAt` on the server (swagger regenerated).
- [x] 1.5 Add the new methods to the `memoryClient` interface in `backend.go` and to `fakeMemory` in `handlers_test.go`; add client unit tests.
- [x] 1.6 Add `GetDocumentContent(ctx, documentID)` so the text preview works for raw-content documents with no stored original.

## 2. Overview sub-page (preview)

- [x] 2.1 Add `documentSubNav(documentID, active)` in `documents.templ` using `layout.Rail` + `components.SettingsRail` (Overview / Chunks / Extractions).
- [x] 2.2 Render `DocumentDetailPage` as the overview: keep the header and extraction trigger card; drop the inline extraction-results section.
- [x] 2.3 Add a preview card chosen by `MimeType`: `application/pdf` → inline `<embed src="/documents/{id}/file">`; `image/*` → `<img>`; `text/*`/markdown → capped escaped text preview from `/content`; otherwise a download link.
- [x] 2.4 Add the binary proxy handler and `GET /documents/:id/file` route (stream bytes, `Content-Disposition: inline`).

## 3. Chunks sub-page

- [x] 3.1 Add `GET /documents/:id/chunks` + `uiDocumentChunks`, rendering the existing `chunksSection` under the rail.

## 4. Extraction history sub-page

- [x] 4.1 Add `GET /documents/:id/extractions` + `uiDocumentExtractions`: list the document's jobs newest-first with timestamp, status badge, object/relationship counts, and discovered types, under the rail.
- [x] 4.2 Add `GET /documents/:id/extractions/:jobId` + `uiDocumentExtractionDetail` (HTMX partial): resolve the job's staging branch from its first created object, list that branch's objects (filtered to the job's created IDs) and relationships, and show merge-readiness.
- [x] 4.3 Add `POST /documents/:id/extractions/:jobId/merge` + `uiDocumentMerge`: merge the job's staging branch into main with policy `partial`, then PRG back to the history page with a flash (success or error).
- [x] 4.4 Hide the merge action when the job has no staging branch or the branch is already merged; show an empty state when a job produced no objects.
- [x] 4.5 Handle branch/job load failures as a degraded section, not a broken page.

## 5. Tests + verification

- [x] 5.1 Unit-test sub-nav active state, preview rendering per MIME type, history list, and merge form/handler (including the hide-when-merged rule).
- [x] 5.2 `templ generate`; `go build ./...`; `go test ./...`; `task lint` from `apps/web-ui/gateway` — all green.
- [ ] 5.3 Browser-test all three sub-pages, the PDF/image/text preview, and a merge flow. Pending: needs a deployed dev build (new routes require a gateway restart); the supervised dev service serves the shared checkout, not this worktree.
- [ ] 5.4 Update `openspec/specs/document-browser/spec.md` via this change's deltas on archive (post-merge).
