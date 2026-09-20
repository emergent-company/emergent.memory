# document-deletion-provenance Specification

## Purpose
Guarantee that a document's derived graph objects and relationships are truthfully previewed and actually removed when the document is deleted, by recording reliable extraction provenance on graph objects and resolving that provenance through canonical identity during deletion.

## Requirements

### Requirement: Graph objects record extraction provenance

Every graph object created or updated by an object extraction job SHALL persist the originating extraction job id on the object row (`kb.graph_objects.extraction_job_id`), in addition to the existing `properties._extraction_job_id` value.

#### Scenario: Object created by an extraction job

- **WHEN** the extraction worker creates a new graph object for extraction job J
- **THEN** the object row's `extraction_job_id` equals J

#### Scenario: Existing object updated by an extraction job

- **WHEN** the extraction worker upserts an object that already exists and a new version is written for job J
- **THEN** the new version row's `extraction_job_id` equals J

### Requirement: Provenance survives versioning and branching

Provenance SHALL be preserved across object versioning, staging-branch extraction, branch merge, and branch fork.

#### Scenario: Version written without explicit provenance

- **WHEN** a new version of an object is created and the caller supplies no extraction job id
- **THEN** the new version inherits the previous head's `extraction_job_id`

#### Scenario: Extraction happens on a staging branch and is merged

- **WHEN** objects extracted on a staging branch are cloned onto the target branch by a merge
- **THEN** the cloned objects retain the originating `extraction_job_id`

#### Scenario: Objects copied to another branch

- **WHEN** existing objects are copied to another branch by fork
- **THEN** the copied objects retain their `extraction_job_id`

### Requirement: Deletion impact reports true derived graph counts

`GET` deletion impact (single document and bulk) SHALL report the number of graph object rows and graph relationship rows that deleting the document(s) will actually remove.

#### Scenario: Document with derived objects

- **WHEN** a client requests deletion impact for a document whose extraction jobs produced graph objects
- **THEN** `graphObjects` and `graphRelationships` are non-zero and equal the rows that deletion removes

#### Scenario: Objects already merged onto the main graph

- **WHEN** a document's objects were extracted on a staging branch and merged to main before the impact is requested
- **THEN** the impact counts include the merged main-graph rows, not zero

#### Scenario: Document with no derived objects

- **WHEN** a document has no extraction jobs or none produced graph objects
- **THEN** the impact reports zero without error

### Requirement: Document deletion removes derived graph data

`DELETE` of a document (single and bulk) SHALL remove the same graph objects and relationships reported by deletion impact.

#### Scenario: Entities wholly derived from the document

- **WHEN** a document is deleted and an entity's versions are all derived from that document's extraction jobs
- **THEN** all versions of that entity and all relationships whose endpoints reference that entity's canonical id are deleted

#### Scenario: Entity also derived from another extraction

- **WHEN** a document is deleted and an entity has at least one version not derived from that document's extraction jobs
- **THEN** only the versions derived from that document are removed and the surviving entity and its relationships are kept

#### Scenario: Counting matches removal

- **WHEN** a document is deleted with cascade
- **THEN** the returned summary's `graphObjects` and `graphRelationships` equal the rows the operation removed

### Requirement: Existing provenance is backfilled

A migration SHALL backfill `kb.graph_objects.extraction_job_id` from the existing `properties._extraction_job_id` value for rows where that value is a valid uuid and the referenced extraction job still exists.

#### Scenario: Legacy object with property provenance

- **WHEN** the migration runs and an object has `properties._extraction_job_id` pointing to an existing extraction job
- **THEN** the object's `extraction_job_id` is set to that job id

#### Scenario: Legacy value without a matching job

- **WHEN** the migration runs and an object's `properties._extraction_job_id` is missing, malformed, or references a job that no longer exists
- **THEN** the row's `extraction_job_id` is left NULL and the row is treated as not document-derived
