## ADDED Requirements

### Requirement: Extraction records chunk provenance per graph object
Object extraction SHALL record, for every graph object it creates or updates, the chunks whose text produced that object, persisted in `kb.object_chunks` with the object id, the chunk id, the extraction job id, and the extraction confidence when available.

Because the extraction pipeline passes each batch to the model as plain text and the extracted-entity model carries no source chunk or span reference, phase 1 provenance SHALL be **batch-scoped**: an object SHALL be linked to every chunk that fed the batch it was extracted from. Batch-scoped provenance attributes an object to a revision and its chunks and MUST NOT be treated as exact per-entity span attribution; exact span attribution requires the extraction output schema to return source references and is a follow-up.

#### Scenario: Object written with batch provenance
- **WHEN** extraction creates a graph object from a batch built from one or more chunks
- **THEN** a row exists in `kb.object_chunks` for that object and each chunk that fed the batch, with the extraction job id recorded

#### Scenario: Re-extraction refreshes provenance
- **WHEN** extraction runs again for a revision and an object is produced again
- **THEN** provenance rows for that object include the chunks of the latest extraction job

### Requirement: Revision extraction is staged and aborts if staging fails
Extraction triggered for a document revision SHALL write its graph objects and relationships to a staging branch and SHALL NOT write to the main graph. The staging branch id SHALL be recorded on the extraction job before extraction proceeds.

For revision extraction, staging-branch creation and its recording SHALL be **mandatory**: if the branch cannot be created or recorded, the extraction SHALL fail (or remain retryable) rather than falling back to the main graph. The existing best-effort fallback used by non-revision extraction SHALL NOT apply to revisions.

#### Scenario: Revision extraction lands on a staging branch
- **WHEN** extraction runs for a revision
- **THEN** the created objects and relationships exist only on a staging branch and are absent from the main graph until applied

#### Scenario: Staging branch recorded before extraction
- **WHEN** a revision extraction begins
- **THEN** the extraction job records the staging branch id before any object is written

#### Scenario: Staging branch creation fails
- **WHEN** the staging branch cannot be created or its id cannot be recorded for a revision extraction
- **THEN** the extraction does not write to the main graph and is reported as failed or retryable

### Requirement: Apply reconciles staged objects to the main graph by type and key
The API SHALL apply a revision's staged delta to the main graph via `POST /api/documents/:id/revisions/:revId/apply`. Because graph branch merging classifies objects by `canonical_id` and a fresh staging branch produces new canonical ids for entities that already exist on the main graph, apply SHALL NOT rely on `MergeBranch`'s canonical-id classification. Apply SHALL instead reconcile staged objects explicitly against the main graph by `(type, key)`:

- staged `(type, key)` absent on the main graph → create the object;
- staged `(type, key)` present on the main graph with differing content → create a new version of the existing object (an update), recording a change summary;
- relationships reconciled by their endpoints' reconciled keys and relationship type.

Apply SHALL be performed atomically and SHALL be idempotent: re-applying an already-applied revision SHALL make no further object, relationship, provenance, or tombstone writes. On success the revision SHALL be marked applied and its staging branch removed.

#### Scenario: Apply an added object
- **WHEN** a staged object's `(type, key)` does not exist on the main graph
- **THEN** applying the revision creates it on the main graph and reports it under added

#### Scenario: Apply an updated object without duplicating it
- **WHEN** a staged object's `(type, key)` already exists on the main graph with different content
- **THEN** applying the revision creates a new version of that existing object (not a duplicate row) and reports it under updated

#### Scenario: Apply conflates identical objects
- **WHEN** a staged object's `(type, key)` exists on the main graph with identical content
- **THEN** applying the revision makes no change for that object

#### Scenario: Apply is idempotent
- **WHEN** a client applies the same revision twice
- **THEN** the second apply performs no additional writes and reports no additional changes

#### Scenario: Apply an unknown or foreign revision
- **WHEN** a client applies a revision id that is not part of the target document's group
- **THEN** the API returns a not-found or validation error and does not touch the main graph

### Requirement: Apply removes objects attributable only to superseded revisions
Applying a revision SHALL tombstone (soft-delete) a main-graph object when the object has provenance from an earlier revision of the same group and provenance from the applied revision's chunks is absent. Chunk identity SHALL NOT be assumed stable across revisions: each revision is chunked independently and chunk ids differ between revisions, so survivorship SHALL be determined by provenance to the applied revision's chunks, not by a chunk id appearing in both revisions.

Objects that are produced again by the applied revision (and therefore gain provenance to the applied revision's chunks), and objects that also derive from another document, SHALL remain live. Apply SHALL re-point provenance for reconciled objects to the main object ids.

#### Scenario: Object removed with the new revision's prose
- **WHEN** an object was produced by an earlier revision of the group and is not produced by the applied revision, and has no provenance from the applied revision's chunks or from another document
- **THEN** applying the revision tombstones that object and reports it under removed

#### Scenario: Unchanged entity survives
- **WHEN** an entity's text is unchanged and the applied revision's extraction produces it again, giving it provenance to the applied revision's chunks
- **THEN** applying the revision leaves that object live

#### Scenario: Shared entity survives
- **WHEN** an object also derives from a different document's chunks
- **THEN** applying the revision leaves that object live even if this document no longer mentions it

### Requirement: Discarding a revision removes its staging
Discarding a revision SHALL delete that revision's staging branch and any objects staged on it, in addition to the revision row and its chunks. Discarding SHALL NOT affect already-applied objects on the main graph, and applied revisions SHALL NOT be discardable (see `document-revisions`), so applied provenance is never destroyed by a discard.

#### Scenario: Discard drops staged objects
- **WHEN** a client discards a revision that was staged but never applied
- **THEN** the staging branch and its objects are removed and the main graph is unchanged

#### Scenario: Applied revisions are immutable
- **WHEN** a revision has been applied
- **THEN** it cannot be discarded and its chunks and provenance remain available for later removal detection

### Requirement: Revision apply is review-gated
A revision SHALL NOT modify the main graph until an explicit apply is requested. Creating a revision and completing its extraction MUST leave the main graph unchanged beyond what already-applied prior revisions established.

#### Scenario: New revision does not mutate the main graph
- **WHEN** a revision is uploaded and extraction completes for it
- **THEN** the main graph contains no new or modified objects from that revision until an apply is requested
