## ADDED Requirements

### Requirement: GraphObject response exposes VersionID and EntityID fields

The system SHALL expose graph object identifiers using the names `version_id` (the version-specific ID that changes on update) and `entity_id` (the stable ID that never changes) in all API responses.

#### Scenario: Object creation response includes both new field names

- **WHEN** user creates a graph object via POST /api/graph/objects
- **THEN** the response SHALL include `version_id` and `entity_id` fields

#### Scenario: Object retrieval includes both new field names

- **WHEN** user retrieves a graph object via GET /api/graph/objects/:id
- **THEN** the response SHALL include `version_id` and `entity_id` fields

#### Scenario: ListObjects response uses new field names

- **WHEN** user lists objects via GET /api/graph/objects/search
- **THEN** each object in the response SHALL include `version_id` and `entity_id` fields

#### Scenario: Backward-compatible old field names emitted during transition

- **WHEN** user receives any graph object API response during the deprecation period
- **THEN** the response SHALL include both old names (`id`, `canonical_id`) and new names (`version_id`, `entity_id`) with identical values

#### Scenario: Old field names removed after deprecation

- **WHEN** the deprecation period ends (major version bump)
- **THEN** the response SHALL include only `version_id` and `entity_id`, and `id`/`canonical_id` SHALL be removed

### Requirement: GraphRelationship response exposes VersionID and EntityID fields

The system SHALL expose graph relationship identifiers using the names `version_id` and `entity_id` in all API responses, consistent with GraphObject naming.

#### Scenario: Relationship creation response includes new field names

- **WHEN** user creates a relationship via POST /api/graph/relationships
- **THEN** the response SHALL include `version_id` and `entity_id` fields for the relationship itself

#### Scenario: Relationship response includes src_id and dst_id as EntityIDs

- **WHEN** user receives a relationship in any API response
- **THEN** `src_id` and `dst_id` SHALL contain the entity IDs (canonical IDs) of the source and destination objects

### Requirement: SDK GraphObject struct uses VersionID and EntityID field names

The Go SDK SHALL rename the `GraphObject` struct fields from `ID`/`CanonicalID` to `VersionID`/`EntityID` to make the stable identifier prominent.

#### Scenario: SDK struct field compilation

- **WHEN** a consumer accesses `obj.EntityID` on a GraphObject
- **THEN** the value SHALL be the stable, never-changing identifier for the entity

#### Scenario: SDK struct field compilation for version

- **WHEN** a consumer accesses `obj.VersionID` on a GraphObject
- **THEN** the value SHALL be the version-specific identifier that changes on every UpdateObject call

#### Scenario: Deprecated fields remain accessible during transition

- **WHEN** a consumer accesses `obj.ID` or `obj.CanonicalID` during the deprecation period
- **THEN** the values SHALL be identical to `obj.VersionID` and `obj.EntityID` respectively

#### Scenario: JSON deserialization handles both old and new names

- **WHEN** SDK receives a JSON response containing both `id`/`version_id` and `canonical_id`/`entity_id`
- **THEN** the SDK SHALL populate both old and new struct fields correctly

### Requirement: ExpandGraph response uses EntityID as node key

The system SHALL use EntityID as the primary key in ExpandGraph response node maps, making entity-keyed traversal the default.

#### Scenario: ExpandGraph nodes keyed by EntityID

- **WHEN** user calls POST /api/graph/expand
- **THEN** the response nodes map SHALL use entity_id as the key for each node

#### Scenario: ExpandGraph edges reference EntityIDs

- **WHEN** user calls POST /api/graph/expand
- **THEN** edge src_id and dst_id in the response SHALL contain entity_ids, matching the node map keys

### Requirement: Dual ID model documentation

The system SHALL provide prominent documentation of the dual ID model in SDK godoc and a standalone guide.

#### Scenario: GraphObject godoc explains ID semantics

- **WHEN** a developer reads the godoc for GraphObject.VersionID
- **THEN** the documentation SHALL state that this ID changes on every UpdateObject call and MUST NOT be used for persistent storage or comparison

#### Scenario: GraphObject godoc explains EntityID semantics

- **WHEN** a developer reads the godoc for GraphObject.EntityID
- **THEN** the documentation SHALL state that this ID never changes after creation and is the correct identifier for storage, relationships, and comparison

#### Scenario: UpdateObject godoc warns about ID mutation

- **WHEN** a developer reads the godoc for Client.UpdateObject
- **THEN** the documentation SHALL warn that the returned object has a new VersionID and that callers MUST use the returned object (not the input) for subsequent operations

#### Scenario: Standalone guide covers mental model

- **WHEN** a developer reads docs/graph-id-model.md
- **THEN** it SHALL include: the Create/Update lifecycle diagram, the UpdateObject footgun example, relationship lookup guidance, and query result dedup patterns
