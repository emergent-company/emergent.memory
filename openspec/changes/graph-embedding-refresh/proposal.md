## Why

Property-only `PATCH`/upsert on a graph object unconditionally enqueued an
embedding job, even when the write touched no embedding-relevant content. A
property migration across a large project therefore triggered one re-embed per
object: a no-op `name` patch on a large `Law` object took ~45 s and an upsert of
a `LegalParagraph` ~188 s on dev (#1478). The re-embed is pure waste when the
embedded text is unchanged.

The embedded payload is the object's `properties` JSONB (there is no text
column). The existing `ContentHash` cannot gate this: it covers *all*
properties plus status, key, and labels, so it changes on any property edit and
cannot distinguish embedding-relevant from incidental changes.

## What Changes

- Add a neutral, dependency-free `apps/server/pkg/embeddingtext` package that
  builds the embedding text and hashes it. The extraction worker and the graph
  write path both call it, so the text that is embedded and the text that is
  hashed can never drift. The builder visits property keys in sorted order, so
  the hash is deterministic (the historical in-worker builder iterated the
  top-level map in Go map order).
- Refactor `extraction.GraphEmbeddingWorker.extractText` to delegate to the
  shared builder; the embedding output is byte-identical for the deterministic
  (boosted) portion and now deterministic for the remaining-property portion.
- Guard the version-creating write paths (`Patch`, `CreateOrUpdate` update, and
  `BulkUpdateObjects`, which loops `Patch`) so an embedding job is enqueued
  **iff** the new embedding-relevant text differs from the pre-update row's.
- When the text is unchanged and the previous row already had an embedding, copy
  the vector onto the new version row inside the same transaction, so the new
  HEAD stays searchable without a redundant embed.
- Guard `MoveObject` (branch move does not change embedded text) so it only
  enqueues when the moved row has no embedding yet.

## Capabilities

### New Capabilities

- `graph-embedding-refresh`: defines when a graph-object write (re)generates an
  embedding — only on a change to embedding-relevant content — and requires the
  embedded text and the write-path guard to share one deterministic builder.

### Modified Capabilities

- None. No main spec currently documents unconditional re-enqueue for graph
  object writes.
