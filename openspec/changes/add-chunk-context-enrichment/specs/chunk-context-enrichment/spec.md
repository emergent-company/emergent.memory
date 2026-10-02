## ADDED Requirements

### Requirement: Document summary is computed once per document

The document summary used for chunk embedding context SHALL be computed once per document (a single LLM call) and cached on the document, and SHALL NOT be recomputed per chunk. All of a document's chunks SHALL share the same cached summary.

#### Scenario: One summary per document

- **WHEN** a document with many chunks is embedded
- **THEN** the summary is computed once for the document and reused for every chunk, not recomputed per chunk

#### Scenario: Summary cached on the document

- **WHEN** a document's summary is computed
- **THEN** it is stored on the document (`kb.documents`), not duplicated onto each chunk

### Requirement: Embedding input is title + summary + chunk text

At embed time, the text passed to the embedding model SHALL be the document title plus the cached one-line document summary plus the chunk text. The raw chunk text alone SHALL no longer be the embedded input when a summary is available.

#### Scenario: Embedding input includes context

- **WHEN** a chunk is embedded and its document has a cached summary
- **THEN** the embedding input SHALL contain the document title, the summary, and the chunk text

#### Scenario: Applied deterministically to all chunks

- **WHEN** two chunks of the same document are embedded
- **THEN** each SHALL receive the same title and summary prefix, followed by its own chunk text

### Requirement: Graceful fallback when summary is missing

When a document has no cached summary (not yet computed, computation failed, or the document has no title), embedding SHALL fall back to the raw chunk text and SHALL NOT fail the chunk's embedding.

#### Scenario: Missing summary falls back to raw text

- **WHEN** a chunk is embedded and its document has no summary
- **THEN** the chunk is embedded using its raw text (possibly with the title if available), and embedding does not fail

#### Scenario: Summary computation failure does not block embedding

- **WHEN** summary computation fails for a document
- **THEN** its chunks SHALL still be embedded using raw text

### Requirement: Re-embed on summary change

When a document's cached summary changes, its chunks SHALL be re-embedded using the new summary, via the existing embedding sweep/job path.

#### Scenario: Summary change triggers re-embed

- **WHEN** a document's summary is recomputed and differs from the cached value
- **THEN** the document's chunks SHALL be enqueued for re-embedding

#### Scenario: Unchanged summary does not re-embed

- **WHEN** a document's summary recomputation yields the same value as the cached summary
- **THEN** no re-embedding SHALL be triggered

### Requirement: Stored retrieval text is unchanged

The stored chunk text (`kb.chunks.text`) — the payload returned in retrieval results — SHALL be unchanged. Context enrichment SHALL affect only the embedding input.

#### Scenario: Retrieval payload unaffected

- **WHEN** a chunk is embedded with context enrichment
- **THEN** the stored chunk text and the text returned to retrieval callers SHALL be identical to the pre-enrichment value
