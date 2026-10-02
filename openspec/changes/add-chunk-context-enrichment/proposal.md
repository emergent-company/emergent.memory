## Why

`extraction/chunk_embedding_worker.go` (`processJob`, roughly lines 281-424) embeds raw
`chunk.Text` with no document-level context. A chunk that says "the second option is
preferred" is embedded identically whether it came from a compliance report or a recipe,
so vector retrieval cannot distinguish chunks that happen to share phrasing but belong
to different documents.

Onyx's answer is "contextual RAG" — rewrite each chunk with document context before
embedding. That is very expensive (one LLM pass per chunk). This change does the **cheap
variant**: prepend the document title plus a *cached one-line document summary* to the
embedded input, compute the summary **once per document** (not per chunk), and store it
**on the document** (not per chunk). The stored chunk text — the retrieval payload — is
unchanged; only the embedding input changes.

## What Changes

- At embed time, prepend the document title and a cached one-line document summary to the
  text passed to the embedding model.
- Store the summary on the document (`kb.documents`), computed once per document via a
  single LLM call (cached), reused by all that document's chunks.
- Re-embed (sweep) a document's chunks when its summary changes.
- Stored `chunk.Text` (the retrieval result payload) is unchanged — only the embedded
  input differs.

## Capabilities

### New Capabilities

- `chunk-context-enrichment`: a cheap document-context prefix (title + one-line summary)
  applied to the embedding input only, computed once per document and cached on the
  document, with re-embedding on summary change and no change to stored retrieval text.

### Modified Capabilities

- `embedding-sweep-ceilings` (related): the sweep gains a trigger — re-embed a document's
  chunks when its cached summary changes. (No delta written here; noted for the sweep
  change to own the admission mechanics.)

## Impact

- **Server** (`apps/server/domain/extraction/chunk_embedding_worker.go`): build the
  embedding input from `chunk.Text` + document title + summary instead of raw `chunk.Text`.
- **DB** (`apps/server/migrations/`): add a nullable `summary` (text) column to
  `kb.documents`.
- **Summary computation** (`apps/server/domain/documents` or a small helper): compute the
  one-line summary once per document and cache it; re-compute when the document changes.
- **Re-embed trigger** (`apps/server/domain/extraction`): when a document's summary
  changes, enqueue its chunks for re-embedding via the existing sweep/job path.
- **No change** to stored `kb.chunks.text` or any retrieval result payload.

## Dependency

None. S-M effort, low risk. The summary-computation call reuses existing provider/model
resolution.
