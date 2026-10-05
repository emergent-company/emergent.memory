## Context

`kb.graph_objects` has no text column; the embedded payload is the `properties`
JSONB (`domain/graph/entity.go`). The embedded text is built by
`extraction/graph_embedding_worker.go` `extractText` (type + stripped key +
boosted fields ×2 + remaining properties, cap 8000 chars). `extraction` imports
`graph`, so `graph` cannot import `extraction` to reuse that builder.

The existing `ContentHash` includes all properties + status + key + labels, so it
changes on any property edit and cannot gate embedding re-enqueue.

## Goals / Non-Goals

- Goal: a property-only `PATCH`/upsert/bulk-update enqueues no embedding when the
  embedding-relevant text is unchanged, and the new HEAD stays searchable.
- Goal: one deterministic builder shared by the worker and the write-path guard.
- Non-goal: changing `ContentHash` semantics.
- Non-goal: profiling/optimising the remaining synchronous write-path cost of
  large-object writes (tracked as a follow-up in the task report).

## Decisions

### D1 — Neutral shared builder `pkg/embeddingtext`

`Text(objectType string, key *string, properties map[string]any) string` and
`Hash(...) [32]byte` (SHA-256). No imports from `domain/*`, so both `graph` and
`extraction` can use it. `extractText` becomes a one-line delegation.

Determinism: the historical builder iterated the top-level property map and
nested maps in Go map order (randomised per call). The shared builder sorts keys
at every level. This is required for the guard: old vs new state must serialise
stably to be comparable. For objects whose only embedded tokens are the
deterministic boosted fields the output is unchanged; where multiple non-boosted
properties exist the order is now stable rather than random — an improvement,
not a regression (embeddings were already recomputed per call because of the
worker's re-embed on every write).

### D2 — Guard on version-creating writes

`carryEmbeddingIfUnchanged(tx, prev, next)`:
- if `Hash(prev) != Hash(next)` → return `wantEnqueue = true` (caller enqueues
  after commit);
- else, if `prev.EmbeddingUpdatedAt != nil` → `CopyEmbedding(prev.ID, next.ID)`
  inside the transaction and return `false`;
- else return `false` (sweep backfills later).

Applied in `Patch` and `CreateOrUpdate`'s update branch. `BulkUpdateObjects`
inherits it by looping `Patch`. The no-op `CreateOrUpdate` branch keeps its
existing `EmbeddingUpdatedAt == nil` condition (content is unchanged there by
construction), and object creation always enqueues.

### D3 — Embedding carry-over

`Repository.CopyEmbedding` runs
`UPDATE kb.graph_objects dst SET embedding_v2 = src.embedding_v2,
embedding_updated_at = src.embedding_updated_at FROM kb.graph_objects src WHERE
dst.id = ? AND src.id = ? AND src.embedding_v2 IS NOT NULL`. Without this, the
new version row (inserted with NULL `embedding_v2` by `CreateVersion`) would drop
out of vector search (`supersedes_id IS NULL AND embedding_v2 IS NOT NULL`) until
the sweep ran, and the sweep would re-embed anyway — defeating the fix. The FTS
trigger skips this update (key/type/properties unchanged).

### D4 — MoveObject

A branch move leaves `type`/`key`/`properties` untouched, so the vector stays
valid; only enqueue when the moved row has no embedding.

## Risks / Trade-offs

- Copying the vector costs one indexed `UPDATE` on an unchanged write — far
  cheaper than an embedding round-trip and it keeps search results fresh
  immediately instead of after the next sweep.
- The builder no longer matches the historical randomised order for objects with
  multiple non-boosted string properties; those embeddings may differ from a
  previously generated vector but were non-deterministic before, so no stable
  contract is broken.
