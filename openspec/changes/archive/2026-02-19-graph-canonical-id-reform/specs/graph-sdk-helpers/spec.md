## ADDED Requirements

### Requirement: IDSet for canonical-aware ID comparison

The SDK SHALL provide an `IDSet` type that wraps both ID variants (VersionID and EntityID) of a graph entity, enabling comparison against either variant with a single call.

#### Scenario: IDSet matches VersionID

- **WHEN** an IDSet is created from a GraphObject with VersionID "v1-abc" and EntityID "ent-xyz"
- **THEN** `idSet.Contains("v1-abc")` SHALL return true

#### Scenario: IDSet matches EntityID

- **WHEN** an IDSet is created from a GraphObject with VersionID "v1-abc" and EntityID "ent-xyz"
- **THEN** `idSet.Contains("ent-xyz")` SHALL return true

#### Scenario: IDSet rejects unrelated ID

- **WHEN** an IDSet is created from a GraphObject with VersionID "v1-abc" and EntityID "ent-xyz"
- **THEN** `idSet.Contains("other-id")` SHALL return false

#### Scenario: IDSet constructed from GraphObject

- **WHEN** caller passes a `*GraphObject` to `NewIDSet`
- **THEN** the IDSet SHALL extract VersionID and EntityID from the object

### Requirement: ObjectIndex for O(1) lookup by either ID variant

The SDK SHALL provide an `ObjectIndex` type that indexes a slice of GraphObjects by both VersionID and EntityID for O(1) retrieval by either identifier.

#### Scenario: Lookup by VersionID

- **WHEN** an ObjectIndex is built from a slice of GraphObjects and caller calls `Get("v1-abc")` where "v1-abc" is a VersionID of one of the objects
- **THEN** the matching GraphObject SHALL be returned

#### Scenario: Lookup by EntityID

- **WHEN** an ObjectIndex is built from a slice of GraphObjects and caller calls `Get("ent-xyz")` where "ent-xyz" is an EntityID of one of the objects
- **THEN** the matching GraphObject SHALL be returned

#### Scenario: Lookup with unknown ID

- **WHEN** caller calls `Get("unknown-id")` on an ObjectIndex
- **THEN** nil SHALL be returned

#### Scenario: Index handles duplicates by keeping latest

- **WHEN** multiple objects share the same EntityID (multiple versions)
- **THEN** the ObjectIndex SHALL keep the object with the highest Version number

### Requirement: UniqueByEntity deduplication utility

The SDK SHALL provide a `UniqueByEntity` function that deduplicates a slice of GraphObjects by EntityID, keeping only the latest version of each entity.

#### Scenario: Dedup removes older versions

- **WHEN** a slice contains two objects with the same EntityID but different VersionIDs and Versions (1 and 2)
- **THEN** `UniqueByEntity` SHALL return only the object with Version 2

#### Scenario: Dedup preserves unique entities

- **WHEN** a slice contains three objects with three distinct EntityIDs
- **THEN** `UniqueByEntity` SHALL return all three objects

#### Scenario: Dedup handles empty EntityID

- **WHEN** an object has an empty EntityID
- **THEN** `UniqueByEntity` SHALL fall back to using VersionID as the dedup key

### Requirement: GetByAnyID client method

The SDK client SHALL provide a `GetByAnyID` method that resolves any ID (version-specific or entity) to the current HEAD object.

#### Scenario: Resolve by VersionID

- **WHEN** caller passes a version-specific ID to `GetByAnyID`
- **THEN** the current HEAD version of the entity SHALL be returned

#### Scenario: Resolve by EntityID

- **WHEN** caller passes an entity ID to `GetByAnyID`
- **THEN** the current HEAD version of the entity SHALL be returned

#### Scenario: Stale VersionID still resolves

- **WHEN** caller passes a VersionID from a previous version (superseded)
- **THEN** the current HEAD version of the entity SHALL be returned, not the old version

#### Scenario: Unknown ID returns error

- **WHEN** caller passes an ID that does not match any object
- **THEN** an appropriate error SHALL be returned

### Requirement: HasRelationship canonical-aware existence check

The SDK client SHALL provide a `HasRelationship` method that checks whether a relationship exists between two entities, matching by canonical (entity) IDs regardless of which ID variant the caller provides.

#### Scenario: Relationship found with EntityIDs

- **WHEN** a relationship of type "has_task" exists between entities A and B, and caller passes their EntityIDs
- **THEN** `HasRelationship` SHALL return true

#### Scenario: Relationship found with VersionIDs

- **WHEN** a relationship of type "has_task" exists between entities A and B, and caller passes VersionIDs for A and B
- **THEN** `HasRelationship` SHALL return true (server resolves to canonical)

#### Scenario: Relationship found with mixed ID variants

- **WHEN** a relationship exists and caller passes EntityID for source and VersionID for destination
- **THEN** `HasRelationship` SHALL return true

#### Scenario: No relationship returns false

- **WHEN** no relationship of the given type exists between the two entities
- **THEN** `HasRelationship` SHALL return false
