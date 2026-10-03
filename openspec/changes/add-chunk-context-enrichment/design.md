## Context

`extraction/chunk_embedding_worker.go` embeds raw `chunk.Text`. Retrieval quality suffers
when context-free chunks collide: identical phrasing in unrelated documents embeds to
near-identical vectors. Onyx's "contextual RAG" rewrites every chunk with document
context before embedding — powerful but O(chunks) LLM calls. This change delivers the
cheap variant at O(documents) cost.

## Goals / Non-Goals

**Goals**

- A document filename (or `source_url`) + one-line summary prefix on the embedding input.
- Summary computed once per document (on ingest), cached on the document, recomputed when
  document content changes.
- Re-embed a document's chunks when the summary changes (including absent→present).
- Zero change to the stored retrieval text.

**Non-Goals**

- Full contextual RAG (per-chunk context rewriting) — rejected on cost.
- Changing what retrieval returns (the payload is `chunk.Text`, untouched).
- Summaries per chunk or per section (that reintroduces per-chunk cost).

## Decisions

### D1 — Cheap variant: prefix, not rewrite

The embedded input is built by concatenation with **optional** parts, so the empty case
collapses to raw text:

```
embedInput = filename_or_source_url (if present) + "\n" + summary (if present) + "\n" + chunk.Text
```

where `filename_or_source_url` is the document's `filename`, falling back to `source_url`
when filename is empty, and omitted entirely when both are empty; and `summary` is the
cached summary, omitted when absent. When both the filename/source_url and the summary are
absent, `embedInput = chunk.Text`. This is deterministic — no per-chunk LLM pass. It gives
the embedding model document-level grounding at effectively zero marginal cost beyond the
one summary call per document. Full contextual RAG (rewrite each chunk) is rejected: it
multiplies token spend by the chunk count and requires re-embedding semantics out of scope.

### D2 — Summary stored on the document, computed on ingest, recomputed on change

`kb.documents` gains a nullable `summary` column. A single LLM call produces the one-line
summary, cached on the document and reused by all its chunks. The summary is computed on
document **ingest** and **recomputed when the document content, `filename`, or `source_url`
changes** (and/or via a **resumable, bounded backfill job** for pre-existing documents).
This is the key cost decision: O(1) summary per document vs O(chunks) for per-chunk context.
The summary is stored on the document so it is shared, versioned, and independently
re-computable, and so re-embedding has a stable trigger (summary value change).

Summary computation is **atomic per document**. The chunk worker processes many chunks
concurrently, so several chunks can observe a NULL summary simultaneously; a bare
"compute-if-null" would fire one LLM call per racing chunk. The helper therefore uses a
compare-and-set / row-lock re-read (or a keyed singleflight guarded by the document row):
one writer computes and commits; every loser re-reads the committed value. This is what
makes "one LLM call per document" hold under concurrency, not merely in a sequential test.

### D3 — Embedding input vs stored text are separate

`chunk.Text` (the stored retrieval payload) is never mutated. The worker builds a separate
`embedInput` string for the embedding call. This is critical to preserve the
retrieval-payload contract and the exact-packet-reproducibility of
`retrieval-trace-persistence`. The trace/retrieval continues to return raw `chunk.Text`.

### D4 — Re-embed via `ChunkEmbeddingJobsService.EnqueueBatch`

A summary change is detected at summary-recompute time (document edit or a backfill job);
when the new summary differs from the cached value (including a transition from absent to
present), the document's chunks are enqueued via
`ChunkEmbeddingJobsService.EnqueueBatch(chunkIDs, priority)`
(`extraction/chunk_embedding_jobs.go:149`). If a by-document enqueue is not already
available, add an `EnqueueByDocument(documentID, priority)` method that resolves the
document's chunk ids and calls `EnqueueBatch`. The sweep worker
(`extraction/embedding_sweep_worker.go`) only backfills objects + relationships into
`kb.graph_embedding_jobs` and does **not** touch chunks, so it is not the re-embed path.
No new queue is introduced; chunk re-embed rides the existing chunk embedding job lifecycle
and its dequeue admission.

## Risks / Trade-offs

- **Summary quality vs cost.** A one-line summary is a weak context signal compared to full
  contextual RAG, but it is the deliberate cost trade-off; the requirement is "cheap".
- **Summary computation failures.** If the summary LLM call fails, chunks fall back to raw
  text (graceful fallback requirement) — no embedding blockage.
- **Re-embed storms.** A document edit that changes the summary enqueues all its chunks.
  Mitigation: only enqueue when the summary value actually differs (not on every edit), and
  ride the existing `kb.chunk_embedding_jobs` dequeue admission.
- **Determinism.** The prefix must be deterministic so re-embedding the same chunk with the
  same summary yields a stable vector; the summary is cached (not recomputed per chunk), so
  the prefix is stable across a document's chunks.
