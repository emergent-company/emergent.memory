## Why

`extraction/chunk_embedding_worker.go` (`processJob`, roughly lines 281-424) embeds raw
`chunk.Text` with no document-level context. A chunk that says "the second option is
preferred" is embedded identically whether it came from a compliance report or a recipe,
so vector retrieval cannot distinguish chunks that happen to share phrasing but belong
to different documents.

Onyx's answer is "contextual RAG" — rewrite each chunk with document context before
embedding. That is very expensive (one LLM pass per chunk). This change does the **cheap
variant**: prepend the document filename (falling back to `source_url`) plus a *cached
one-line document summary* to the embedded input, compute the summary **once per
document** (not per chunk), and store it **on the document** (not per chunk). The stored
chunk text — the retrieval payload — is unchanged; only the embedding input changes.

## What Changes

- At embed time, prepend the document filename (or `source_url` when filename is empty)
  and a cached one-line document summary to the text passed to the embedding model, each
  omitted when absent (raw `chunk.Text` when both absent).
- Store the summary on the document (`kb.documents`), computed once per document on
  ingest (a single LLM call, cached), recomputed when document content changes (and/or via
  a backfill job).
- Re-embed a document's chunks (via `ChunkEmbeddingJobsService.EnqueueBatch`) when its
  summary changes, including an absent→present transition.
- Stored `chunk.Text` (the retrieval result payload) is unchanged — only the embedded
  input differs.

## Capabilities

### New Capabilities

- `chunk-context-enrichment`: a cheap document-context prefix (filename/source_url +
  one-line summary) applied to the embedding input only, computed once per document and
  cached on the document, with re-embedding on summary change and no change to stored
  retrieval text.

### Related / Consumed Capabilities

- None — enrichment is self-contained. It consumes the existing `kb.chunk_embedding_jobs`
  re-embed path but modifies no existing capability spec (no delta).

## Impact

- **Server** (`apps/server/domain/extraction/chunk_embedding_worker.go`): build the
  embedding input from `chunk.Text` + document filename/source_url + summary instead of
  raw `chunk.Text`.
- **DB** (`apps/server/migrations/`): add a nullable `summary` (text) column to
  `kb.documents`. No `title` column exists; the existing `filename` (fallback `source_url`)
  is reused.
- **Summary computation** (`apps/server/domain/documents` or a small helper): compute the
  one-line summary once per document on ingest and cache it; recompute on content change
  and via a backfill job.
- **Re-embed trigger** (`apps/server/domain/extraction`): when a document's summary
  changes (including absent→present), enqueue its chunks via
  `ChunkEmbeddingJobsService.EnqueueBatch`.
- **No change** to stored `kb.chunks.text` or any retrieval result payload.

## Dependency

None. S-M effort, low risk. The summary-computation call reuses existing provider/model
resolution.
