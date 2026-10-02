## Context

`extraction/chunk_embedding_worker.go` embeds raw `chunk.Text`. Retrieval quality suffers
when context-free chunks collide: identical phrasing in unrelated documents embeds to
near-identical vectors. Onyx's "contextual RAG" rewrites every chunk with document
context before embedding — powerful but O(chunks) LLM calls. This change delivers the
cheap variant at O(documents) cost.

## Goals / Non-Goals

**Goals**

- A document filename (or `source_url`) + one-line summary prefix on the embedding input.
- Summary computed once per document, cached on the document.
- Re-embed a document's chunks when the summary changes.
- Zero change to the stored retrieval text.

**Non-Goals**

- Full contextual RAG (per-chunk context rewriting) — rejected on cost.
- Changing what retrieval returns (the payload is `chunk.Text`, untouched).
- Summaries per chunk or per section (that reintroduces per-chunk cost).

## Decisions

### D1 — Cheap variant: prefix, not rewrite

The embedded input becomes `filename_or_source_url + "\n" + summary + "\n" + chunk.Text`,
where `filename_or_source_url` is the document's `filename` and, when that is empty, its
`source_url`. This is a deterministic string concatenation — no per-chunk LLM pass. It
gives the embedding model document-level grounding at effectively zero marginal cost
beyond the one summary call per document. Full contextual RAG (rewrite each chunk) is
rejected: it multiplies token spend by the chunk count and requires re-embedding
semantics that are out of scope here.

### D2 — Summary stored on the document, computed once

`kb.documents` gains a nullable `summary` column. A single LLM call produces the one-line
summary, cached on the document and reused by all its chunks. This is the key cost
decision: O(1) summary per document vs O(chunks) for per-chunk context. The summary is
stored on the document so it is shared, versioned, and independently re-computable, and so
re-embedding has a stable trigger (summary value change).

### D3 — Embedding input vs stored text are separate

`chunk.Text` (the stored retrieval payload) is never mutated. The worker builds a separate
`embedInput` string for the embedding call. This is critical to preserve the
retrieval-payload contract and the exact-packet-reproducibility of
`retrieval-trace-persistence`. The trace/retrieval continues to return raw `chunk.Text`.

### D4 — Re-embed via `kb.chunk_embedding_jobs`

A summary change is detected at summary-recompute time (document edit or a backfill job);
when the new summary differs, the document's chunks are enqueued into
`kb.chunk_embedding_jobs` (or routed through the document re-embed handler). The sweep
worker (`extraction/embedding_sweep_worker.go`) only backfills objects + relationships
into `kb.graph_embedding_jobs` and does **not** touch chunks, so it is not the re-embed
path here. No new queue is introduced; chunk re-embed rides the existing chunk embedding
job lifecycle and its dequeue admission.

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
