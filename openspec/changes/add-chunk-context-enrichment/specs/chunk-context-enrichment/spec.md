## ADDED Requirements

### Requirement: Document summary is computed once per document

The document summary used for chunk embedding context SHALL be computed once per document (a single LLM call) and cached on the document, and SHALL NOT be recomputed per chunk. All of a document's chunks SHALL share the same cached summary.

#### Scenario: One summary per document

- **WHEN** a document with many chunks is embedded
- **THEN** the summary is computed once for the document and reused for every chunk, not recomputed per chunk

#### Scenario: Summary cached on the document

- **WHEN** a document's summary is computed
- **THEN** it is stored on the document (`kb.documents`), not duplicated onto each chunk

### Requirement: Summary is computed on ingest and recomputed on content change

The summary SHALL be computed when a document is ingested, and SHALL be recomputed when the document's content changes (and/or via a backfill job for pre-existing documents). When a summary transitions from absent to present, the document's chunks SHALL be enqueued for re-embedding.

#### Scenario: Summary computed on ingest

- **WHEN** a document is ingested
- **THEN** its summary SHALL be computed and cached once

#### Scenario: Summary recomputed on content change

- **WHEN** a document's content changes after ingest
- **THEN** its summary SHALL be recomputed and, if it differs, its chunks SHALL be enqueued for re-embedding

#### Scenario: Absent-to-present summary triggers re-embed

- **WHEN** a document that previously had no summary (e.g. computation failed, or a backfill runs) gains a summary
- **THEN** the document's chunks SHALL be enqueued for re-embedding using the new summary

### Requirement: Embedding input is filename/source_url + summary + chunk text

At embed time, the text passed to the embedding model SHALL be `filename_or_source_url (if present) + "\n" + summary (if present) + "\n" + chunk.Text`, where `filename_or_source_url` is the document `filename` (falling back to `source_url` when filename is empty, and omitted when both are empty) and `summary` is the cached summary (omitted when absent). The raw chunk text alone SHALL be the embedded input when both the filename/source_url and the summary are absent.

#### Scenario: Embedding input includes context

- **WHEN** a chunk is embedded and its document has a filename/source_url and a cached summary
- **THEN** the embedding input SHALL be `filename_or_source_url + "\n" + summary + "\n" + chunk.Text`

#### Scenario: Applied deterministically to all chunks

- **WHEN** two chunks of the same document are embedded
- **THEN** each SHALL receive the same filename/source_url and summary prefix, followed by its own chunk text

### Requirement: Graceful fallback when summary is missing

When a document has no cached summary (not yet computed, computation failed, or the document has no filename and no source_url), embedding SHALL fall back to the raw chunk text and SHALL NOT fail the chunk's embedding.

#### Scenario: Missing summary falls back to raw text

- **WHEN** a chunk is embedded and its document has no summary
- **THEN** the chunk is embedded using `filename_or_source_url + "\n" + chunk.Text` (or raw `chunk.Text` when neither is present), and embedding does not fail

#### Scenario: Summary computation failure does not block embedding

- **WHEN** summary computation fails for a document
- **THEN** its chunks SHALL still be embedded using raw text

### Requirement: Re-embed on summary change

When a document's cached summary changes (including absent→present), its chunks SHALL be re-embedded using the new summary, via `ChunkEmbeddingJobsService.EnqueueBatch(chunkIDs, priority)` (or a new by-document enqueue method that resolves chunk ids and calls `EnqueueBatch`).

#### Scenario: Summary change triggers re-embed

- **WHEN** a document's summary is recomputed and differs from the cached value
- **THEN** the document's chunks SHALL be enqueued for re-embedding via `ChunkEmbeddingJobsService.EnqueueBatch`

#### Scenario: Unchanged summary does not re-embed

- **WHEN** a document's summary recomputation yields the same value as the cached summary
- **THEN** no re-embedding SHALL be triggered

### Requirement: Stored retrieval text is unchanged

The stored chunk text (`kb.chunks.text`) — the payload returned in retrieval results — SHALL be unchanged. Context enrichment SHALL affect only the embedding input.

#### Scenario: Retrieval payload unaffected

- **WHEN** a chunk is embedded with context enrichment
- **THEN** the stored chunk text and the text returned to retrieval callers SHALL be identical to the pre-enrichment value
