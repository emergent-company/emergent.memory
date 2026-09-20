## 1. Server — expose per-object embedding status on the object API

- [x] 1.1 Test-first: add a unit test asserting the graph object search/list/detail response includes `embedding_status` and `embedding_updated_at` (fails), then add the fields to the response DTO and verify the test passes
- [x] 1.2 Implement the status classification query: `LEFT JOIN LATERAL` to the latest `kb.graph_embedding_jobs` row per object plus `embedding_v2 IS NOT NULL`, producing status ∈ {embedded, pending, processing, failed, dead_letter, missing}; verify a unit test covers each classification
- [x] 1.3 Add unit tests for status precedence — an object with a vector is `embedded` regardless of job rows; an object with no vector takes the latest job row status; an object with neither is `missing`; a retried object shows the latest (not stale-failed) job row

## 2. Gateway data layer — GraphObject + embeddings client

- [x] 2.1 Test-first: write a unit test asserting `GraphObject` parses `embedding_status` and `embedding_updated_at` from object JSON (fails), then add the fields to `GraphObject` and verify the test passes
- [x] 2.2 Add `GetEmbeddingProgress` / `GetEmbeddingStatus` to `MemoryBackend` + `MemoryClient` with matching response types; unit-test the GET `/api/embeddings/progress` and `/api/embeddings/status` paths/payload parsing and update `fakeMemory` so the suite compiles

## 3. UI — object card and detail embedding badge

- [x] 3.1 Render an embedding-status badge on each object list card; unit-test that a card shows the correct badge for each status (embedded, pending, processing, failed, missing)
- [x] 3.2 Render the embedding status (and last-updated time when embedded) in the object detail header; unit-test the detail page shows the status for embedded and non-embedded objects

## 4. UI — embeddings status page

- [x] 4.1 Add the `/embeddings` route and handler that fetches progress + status and renders the page; unit-test the route is registered and the handler passes data to the template
- [x] 4.2 Render the object and relationship queue counts (pending/processing/completed/failed/dead-letter) and the worker running/paused state plus config; unit-test the rendered counts and worker state
- [x] 4.3 Render a clear empty state when no stats are available and an error state when the backend fetch fails, without breaking the page; unit-test both states
- [x] 4.4 Add a navigation entry to the embeddings status page; unit-test the nav link is present

## 5. Build and verify

- [x] 5.1 Run `templ generate` then `go build ./...` from `gateway/` and verify it compiles
- [x] 5.2 Run `go build ./...` from the server module root and verify it compiles
- [x] 5.3 Run `task lint` and `go test ./...` from `gateway/` and verify both pass
- [x] 5.4 Run the server graph-domain tests and verify they pass
- [ ] 5.5 Restart the dev server and manually verify in the browser: object cards and detail show embedding status, and the embeddings page shows queue counts and worker state
