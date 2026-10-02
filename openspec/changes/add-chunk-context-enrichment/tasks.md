## 1. Migration — document summary column (TDD)

- [ ] 1.1 New migration `apps/server/migrations/<n>_add_document_summary.sql`: add nullable `summary` (text) to `kb.documents`.
- [ ] 1.2 (TDD) Migration test: up adds the column (nullable); down drops it cleanly.

## 2. Summary computation once per document (TDD)

- [ ] 2.1 Add a summary-computation helper (in `domain/documents` or a small `pkg`) that produces a one-line summary via a single LLM call, caches it on `kb.documents.summary`, and returns the cached value when present.
- [ ] 2.2 (TDD) Unit test with a fake model: computing a summary for a multi-chunk document invokes the model once; a second request returns the cached value without a second call.

## 3. Embedding input = title + summary + text (TDD)

- [ ] 3.1 In `domain/extraction/chunk_embedding_worker.go` `processJob`, build the embedding input as `title + "\n" + summary + "\n" + chunk.Text` (falling back to raw text when title/summary missing) and pass that to the embedding call; leave `chunk.Text` stored value untouched.
- [ ] 3.2 (TDD) Unit test: with title+summary present, the embedding input contains both plus the chunk text; with no summary, the embedding input is raw chunk text; the stored `chunk.Text` is unchanged in all cases.
- [ ] 3.3 (TDD) Determinism test: two chunks of the same document receive the identical title+summary prefix.

## 4. Re-embed on summary change (TDD)

- [ ] 4.1 On summary recompute, compare old vs new; when different, enqueue the document's chunks for re-embedding via the existing sweep/job path; when unchanged, enqueue nothing.
- [ ] 4.2 (TDD) Unit test: summary change enqueues chunks; unchanged summary enqueues nothing.

## 5. Verify

- [ ] 5.1 `cd apps/server && go build ./... && go test ./domain/extraction/... ./domain/documents/...`.
- [ ] 5.2 `task lint` for the touched modules.
- [ ] 5.3 Deferred (documented): full contextual RAG (per-chunk rewrite) is out of scope.
