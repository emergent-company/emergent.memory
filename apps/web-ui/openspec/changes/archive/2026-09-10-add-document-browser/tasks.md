## 1. Memory client document methods

- [x] 1.1 Add `Document` and `Chunk` structs to `gateway/memory.go` mirroring `DocumentDto`/`ChunkDto` (id, name, mimeType, chunks, extractionStatus, extractionObjectsCount, createdAt; chunk index/size/hasEmbedding/text)
- [x] 1.2 Add `ListDocuments` method (GET /documents, cursor passthrough + x-next-cursor capture)
- [x] 1.3 Add `GetDocument` method (GET /documents/{id})
- [x] 1.4 Add `ListChunks` method (GET /chunks?documentId=)
- [x] 1.5 Add `UploadDocument` method (multipart POST /ingest/upload) with a 10 MB size cap
- [x] 1.6 Add `CreateExtractionJob` method (POST /admin/extraction-jobs with source_type "document")
- [x] 1.7 Unit-test the document methods against a mock HTTP server (happy paths + error/empty cases)

## 2. Gateway HTTP handlers and routes

- [x] 2.1 Add `GET /api/documents` handler returning `{documents, nextCursor}`
- [x] 2.2 Add `GET /api/documents/:id` handler returning document detail
- [x] 2.3 Add `GET /api/documents/:id/chunks` handler returning ordered chunks
- [x] 2.4 Add `POST /api/documents` handler accepting a single multipart file upload
- [x] 2.5 Add `POST /api/documents/:id/extract` handler returning the created job reference
- [x] 2.6 Register the five document routes in `gateway/main.go` under the authenticated `/api` group
- [x] 2.7 Unit-test the handlers with `httptest` (empty list, unknown id, upload validation, unauthenticated → 401)

## 3. Web UI

- [x] 3.1 Add a "Documents" entry to `sidebarGroups()` in `gateway/ui.go`
- [x] 3.2 Add a documents templ page (list + upload form) and `uiDocuments` handler at `/documents`
- [x] 3.3 Add a document detail templ page (chunk preview + extraction trigger/status) and `uiDocument` handler at `/documents/:id`
- [x] 3.4 Register `/documents` and `/documents/:id` routes in `gateway/main.go`
- [x] 3.5 Add templ render smoke tests for the list and detail pages (empty, populated, and error states)

## 4. Verification

- [x] 4.1 Run `templ generate` and `go build ./...` in `gateway/`
- [x] 4.2 Run `go test ./...` in `gateway/` and fix any failures
- [x] 4.3 Run the configured linter (`task lint` or equivalent) and fix findings
- [x] 4.4 Restart the server and manually verify upload, chunk preview, and extraction trigger in the browser

## 5. Extraction results (objects + relationships)

- [x] 5.1 Add `ExtractionSummary`, `GraphObject` (with branch_id), and `GraphRelationship` structs to `gateway/memory.go`
- [x] 5.2 Add `GetExtractionSummary`, `GetGraphObject`, `ListGraphObjects` (branch + ids), and `ListGraphRelationships` (branch) methods
- [x] 5.3 Add `extractionResults` assembly + relationship filtering in `gateway/documents.go`, wired into `uiDocument`
- [x] 5.4 Add the "Extraction results" section (objects + relationships) to `gateway/documents.templ`
- [x] 5.5 Unit-test the assembly, relationship filtering, and results rendering

## 6. Objects browser (standalone)

- [x] 6.1 Extend `ListGraphObjects` with a type filter and add `GetObjectEdges`
- [x] 6.2 Add `uiObjects`/`uiObject` page handlers + helpers in `gateway/objects.go`
- [x] 6.3 Add the `ObjectsPage`/`ObjectDetailPage` views in `gateway/objects.templ`
- [x] 6.4 Add the "Objects" sidebar entry and `/objects` + `/objects/:id` routes
- [x] 6.5 Unit-test the handlers, helpers, and rendering

## 7. Branch selection in the objects browser

- [x] 7.1 Add the `Branch` struct and `ListBranches` method
- [x] 7.2 Add a branch selector (main + named branches) to the objects page
- [x] 7.3 Wire `?branch=` filtering through `uiObjects`
- [x] 7.4 Unit-test branch filtering and selector rendering
