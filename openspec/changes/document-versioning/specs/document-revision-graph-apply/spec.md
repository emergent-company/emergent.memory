## ADDED Requirements

### Requirement: Extraction records chunk provenance per graph object
Object extraction SHALL record, for every graph object it creates or updates, the chunk or chunks whose text produced that object. Provenance SHALL be persisted in `kb.object_chunks` with the object id, the chunk id, the extraction job id, and the object's extraction confidence when available. Batch construction for extraction MUST carry chunk identity alongside chunk text so that a batch's objects can be attributed back to their source chunks.

#### Scenario: Object written with provenance
- **WHEN** extraction creates a graph object from a batch built from one or more chunks
- **THEN** a row exists in `kb.object_chunks` for that object and each source chunk with the extraction job id recorded

#### Scenario: Re-extraction refreshes provenance
- **WHEN** extraction runs again for a revision and updates an existing `(type, key)` object
- **THEN** provenance rows reflect the chunks used by the latest extraction job

### Requirement: Revision extraction is staged and not auto-merged
Extraction triggered for a document revision SHALL write its graph objects and relationships to a staging branch and SHALL NOT auto-merge into the main graph. The staging branch id SHALL be recorded on the extraction job. Extraction for non-revision documents MAY retain its existing behaviour.

#### Scenario: Revision extraction lands on a staging branch
- **WHEN** extraction runs for a revision
- **THEN** the created objects and relationships exist on a staging branch and are absent from the main graph until applied

#### Scenario: Staging branch recorded
- **WHEN** a revision extraction completes
- **THEN** the extraction job row records the staging branch id used

### Requirement: Apply a revision to the main graph
The API SHALL apply a revision's staged delta to the main graph via `POST /api/documents/:id/revisions/:revId/apply`. Applying SHALL first perform a dry-run merge to enumerate changes, then execute the merge of the revision's staging branch into the main graph. Applying SHALL additionally tombstone graph objects whose entire provenance is removed content — that is, objects sourced only from chunks present in the source revision and absent from the applied revision. Applying SHALL NOT tombstone objects that remain sourced by unchanged or added chunks. The response SHALL report added, updated, and removed counts. Applying is idempotent: re-applying an already-applied revision SHALL NOT duplicate objects, relationships, or tombstones.

#### Scenario: Apply a revision with additions and updates
- **WHEN** a client applies a revision whose staged delta adds and updates objects
- **THEN** the main graph reflects those additions and updates and the response reports the added and updated counts

#### Scenario: Apply tombstones removed objects
- **WHEN** a revision removes content that was the sole source of a graph object
- **THEN** applying the revision tombstones that object in the main graph and the response counts it under removed

#### Scenario: Objects still sourced by retained content survive
- **WHEN** an object is produced by chunks that exist in both the source and applied revisions
- **THEN** applying the revision leaves that object live in the main graph

#### Scenario: Apply is idempotent
- **WHEN** a client applies the same revision twice
- **THEN** the second apply performs no additional object, relationship, or tombstone writes and reports no additional changes

#### Scenario: Apply an unknown or foreign revision
- **WHEN** a client applies a revision id that is not part of the target document's group
- **THEN** the API returns a not-found or validation error and does not touch the main graph

### Requirement: Discarding a revision removes its staging
Discarding a revision SHALL delete that revision's staging branch and any objects staged on it, in addition to the revision row and its chunks. Discarding SHALL NOT affect already-applied objects on the main graph.

#### Scenario: Discard drops staged objects
- **WHEN** a client discards a revision that was staged but never applied
- **THEN** the staging branch and its objects are removed and the main graph is unchanged

### Requirement: Revision apply is review-gated
A revision SHALL NOT modify the main graph until an explicit apply is requested. Creating a revision and running its extraction MUST leave the main graph unchanged beyond what an already-applied prior revision established.

#### Scenario: New revision does not mutate the main graph
- **WHEN** a revision is uploaded and extraction completes for it
- **THEN** the main graph contains no new or modified objects from that revision until an apply is requested
