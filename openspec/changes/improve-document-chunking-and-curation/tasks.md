## 1. Characterize current behavior

- [x] 1.1 Add a characterization unit test pinning the current `recursive_character` splitter output for a fixture corpus (plain text, markdown, tables, code), so the refactor cannot change defaults

## 2. Strategy registry and configuration

- [x] 2.1 Add a unit test that chunking with no configured strategy uses `recursive_character`, then introduce the strategy registry
- [ ] 2.2 Add a unit test for per-project strategy selection through the config, then wire it through `chunking.Service`
- [x] 2.3 Add a unit test that an unknown strategy or out-of-range parameter is rejected on write and the previous config is unchanged, then implement the chunking-config write/validation surface
- [ ] 2.4 Add unit tests for the config read/write endpoints (valid, invalid), then implement handlers and routes

## 3. Structure-aware strategy

- [x] 3.1 Add a unit test that heading/paragraph boundaries are respected, then implement the structure-aware strategy
- [x] 3.2 Add a unit test that a fenced code block longer than the chunk size is emitted whole, then implement the protected-segment tokenizer
- [x] 3.3 Add a unit test that a table spanning a chunk boundary stays in one chunk, then implement table protection
- [x] 3.4 Add a unit test that an oversized protected segment is emitted as its own chunk, flagged in metadata, and skipped for embedding over the model context, then implement
- [x] 3.5 Add a unit test that a chunk starting mid-table gets the header prepended, then implement header carry
- [x] 3.6 Add a unit test that an unknown header records `headerCarry: "unknown"`, then implement

## 4. Parent-child chunking

- [x] 4.1 Add a unit test that parent-child is off by default, then implement the flag
- [x] 4.2 Add a unit test that each child records its parent section id when enabled, then implement parent emission
- [x] 4.3 Add a unit test that parent chunks are excluded from embedding and search by default, then implement the exclusion

## 5. Chunk revisions and curation

- [x] 5.1 Add a migration for `kb.chunk_revisions` (with monotonic `seq`) and the chunk columns (`parent_chunk_id`, `edited`, `current_revision`), including a backfill of sequence 1 from `kb.chunks.text`; verify with a migration test
- [ ] 5.2 Add a unit test that creating a chunk records sequence 1 with the original text, then implement
- [ ] 5.3 Add a unit test that editing a chunk appends the next revision, sets `edited`, and enqueues re-embedding, then implement
- [ ] 5.4 Add a unit test that listing revisions returns them by ascending `seq` with author/time, then implement
- [ ] 5.5 Add a unit test that rollback restores prior text and records a new revision, then implement
- [ ] 5.6 Add a unit test that re-chunk preserves edited chunks (flagged, position retained or marked stale) with a warning, then implement
- [ ] 5.7 Add a unit test that `force` discards edited chunks with a warning, then implement

## 6. API and UI

- [ ] 6.1 Add unit tests for chunk edit/list-revisions/diff/rollback endpoints (success and not-found), then implement handlers and routes
- [x] 6.2 Add the gateway chunk editor with history and rollback; verify with render tests

## 7. Verification

- [ ] 7.1 `go build ./...` + `go vet ./...` + `go test ./...` in `apps/server` pass
- [ ] 7.2 `task lint` passes; `templ generate` produces no diff if gateway UI changed
- [ ] 7.3 Add an e2e test: chunk a markdown document with tables/code, confirm no split tables and headers present, edit a chunk, view history, roll back
- [ ] 7.4 Manual browser test: chunk a markdown document with tables/code, confirm no split tables and headers present, edit a chunk, view history, roll back
