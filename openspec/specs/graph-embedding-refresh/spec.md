# graph-embedding-refresh Specification

## Purpose

Defines how graph-object embeddings are built and refreshed. A single
deterministic text builder is shared by the embedding worker and the graph write
paths, so an embedding job is enqueued only when embedding-relevant content
actually changes. Unchanged text carries the previous vector forward, and branch
moves never trigger a redundant re-embed, keeping the graph's vector index
consistent without wasted embedding work.

## Requirements

### Requirement: Shared deterministic embedding-text builder

The build of the text embedded for a graph object and the hash used to decide
whether a write changes the embedding SHALL come from a single shared builder
(package `pkg/embeddingtext`), used by both the embedding worker and the graph
write path. The builder SHALL be deterministic: property keys are visited in a
stable order at every nesting level, so the same logical object state always
yields the same text and hash. The builder SHALL preserve the existing token
order (object type, stripped key, boosted fields `name`/`title`/`description`/
`summary`/`label` repeated twice, then remaining string/numeric/bool values)
and the existing character cap.

#### Scenario: Identical content, different map order

- **WHEN** two object states carry the same properties but were inserted into
  the Go map in different orders
- **THEN** `Text` returns byte-identical strings and `Hash` returns equal digests

#### Scenario: Only embedding-relevant fields affect the hash

- **WHEN** only an embedding-relevant field changes (for example `name` or a
  remaining string property)
- **THEN** the hash changes
- **AND** when only a skipped key (`version`, `checksum`, `id`, …), a URL value,
  a UUID value, or a large numeric ID changes, the hash does not change

### Requirement: Enqueue an embedding only when embedding-relevant content changes

For a write that creates a new version of an existing object, the graph write
path SHALL enqueue an embedding job if and only if the new version's
embedding-relevant text differs from the pre-update row's. This applies to
`Patch`, the update branch of `CreateOrUpdate`, and `BulkUpdateObjects` (which
patches each item). A write that creates a new object (no previous row) SHALL
always enqueue.

#### Scenario: Property-only PATCH does not enqueue

- **WHEN** an object is patched and only a non-embedded property changes
- **THEN** no embedding job is enqueued

#### Scenario: Semantic PATCH enqueues

- **WHEN** an object is patched and an embedded field (`name`, `description`, or
  another remaining property) changes
- **THEN** exactly one embedding job is enqueued

#### Scenario: Property-only upsert does not enqueue

- **WHEN** `CreateOrUpdate` updates an existing object and only a non-embedded
  property changes
- **THEN** no embedding job is enqueued

#### Scenario: Property-only bulk update does not enqueue

- **WHEN** a bulk update changes only a non-embedded property of an object
- **THEN** no embedding job is enqueued

#### Scenario: New object still enqueues

- **WHEN** an object is created and has no previous row
- **THEN** an embedding job is enqueued

### Requirement: Unchanged text carries the embedding forward

When a new version is created, the embedding-relevant text is unchanged, and the
previous row already has an embedding, the write path SHALL copy the previous
embedding vector and its timestamp onto the new version row within the same
transaction, so the new HEAD remains eligible for vector search without a
redundant embed. When the text is unchanged but the previous row has no
embedding, the write path SHALL NOT enqueue; the self-healing embedding sweep
backfills it.

#### Scenario: New HEAD inherits the vector

- **WHEN** an embedded object is patched with a non-embedded property change
- **THEN** the new HEAD row carries the previous embedding and no job is enqueued

### Requirement: Branch moves do not re-embed already-embedded objects

`MoveObject` SHALL NOT enqueue an embedding for a moved object that already has
one, because a branch move does not change the object's embedding-relevant text.
It SHALL still enqueue when the moved row has no embedding yet.

#### Scenario: Moving an embedded object does not enqueue

- **WHEN** an object with an embedding is moved to another branch
- **THEN** no embedding job is enqueued
