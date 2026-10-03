<!-- openspec:archive-hold: spec-only change; implementation intentionally deferred (PR #1410) -->
## 1. Migration — document summary column (TDD)

- [ ] 1.1 New migration `apps/server/migrations/<n>_add_document_summary.sql`: add nullable `summary` (text) to `kb.documents`.
- [ ] 1.2 (TDD) Migration test: up adds the column (nullable); down drops it cleanly.

## 2. Summary computation once per document (TDD)

- [ ] 2.1 Add a summary-computation helper (in `domain/documents` or a small `pkg`) that produces a one-line summary via a single LLM call, caches it on `kb.documents.summary`, and returns the cached value when present. Make computation **atomic per document**: a compare-and-set / `SELECT … FOR UPDATE` re-read (or keyed singleflight guarded by the row) so concurrent callers observing a NULL summary compute exactly once — the loser re-reads the committed value instead of computing again.
- [ ] 2.2 (TDD) Unit test with a fake model: computing a summary for a multi-chunk document invokes the model once; a second request returns the cached value without a second call.
- [ ] 2.3 (TDD) Concurrent test: many chunks of the same document request a summary simultaneously; exactly ONE model call runs and all callers observe the same cached value (no duplicate LLM calls, no lost update).

## 3. Summary compute/recompute wiring

- [ ] 3.1 Wire summary computation into the document ingest path (compute on create) and the document update path (recompute on **content change OR `filename` OR `source_url` change** — each is a document-mutation invalidation trigger). Add a **resumable, bounded backfill job** that schedules summary computation for pre-existing documents (batch processing with a checkpoint, so it survives interruption and never double-computes/double-enqueues).
- [ ] 3.2 (TDD) Unit test: ingest computes and caches a summary; a content change recomputes it; a `filename`/`source_url` change recomputes it; the backfill computes summaries for documents missing one and resumes from its checkpoint after interruption.

## 4. Embedding input = filename/source_url + summary + text (TDD)

- [ ] 4.1 In `domain/extraction/chunk_embedding_worker.go` `processJob`, build the embedding input as `filename_or_source_url (if present) + "\n" + summary (if present) + "\n" + chunk.Text` (collapsing to raw `chunk.Text` when both are absent) and pass that to the embedding call; leave `chunk.Text` stored value untouched.
- [ ] 4.2 (TDD) Unit test: with filename/source_url+summary present, the embedding input is `filename_or_source_url + "\n" + summary + "\n" + chunk.Text`; with no summary, the input omits the summary; with neither, the input is raw `chunk.Text`; the stored `chunk.Text` is unchanged in all cases.
- [ ] 4.3 (TDD) Determinism test: two chunks of the same document receive the identical filename/source_url+summary prefix.

## 5. Re-embed on summary change (TDD)

- [ ] 5.1 On summary recompute, compare old vs new; when different (including absent→present), enqueue the document's chunks via `ChunkEmbeddingJobsService.EnqueueBatch(chunkIDs, priority)` (add an `EnqueueByDocument(documentID, priority)` method if a by-document enqueue does not already exist); when unchanged, enqueue nothing.
- [ ] 5.2 (TDD) Unit test: summary change enqueues chunks via `EnqueueBatch`; absent→present enqueues chunks; unchanged summary enqueues nothing.

## 6. Verify

- [ ] 6.1 `task build` (server compile).
- [ ] 6.2 `task lint` for the touched modules.
- [ ] 6.3 Deferred (documented): full contextual RAG (per-chunk rewrite) is out of scope.
