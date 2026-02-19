## MODIFIED Requirements

### Requirement: Search response format remains backward compatible

The system SHALL maintain backward compatibility for search response structure while adding optional relationship results and transitioning to new ID field names.

**Modified from:** Response contains only `objects: []` array with `id` and `canonical_id` fields
**Modified to:** Response contains `objects: []` AND optional `relationships: []` array, with objects using `version_id`/`entity_id` fields alongside deprecated `id`/`canonical_id` during transition

#### Scenario: Existing clients ignore relationship results

- **WHEN** old client receives search response with `relationships` array
- **THEN** client can safely ignore new field and use `objects` array as before

#### Scenario: New clients consume relationship results

- **WHEN** new client receives search response
- **THEN** client can access both `objects` and `relationships` arrays for enhanced display

#### Scenario: Search response objects include new ID field names

- **WHEN** client receives search response during the deprecation period
- **THEN** each object in the `objects` array SHALL include both `version_id`/`entity_id` and deprecated `id`/`canonical_id` fields

#### Scenario: Search response objects use only new names after deprecation

- **WHEN** client receives search response after the deprecation period ends (major version bump)
- **THEN** each object SHALL include only `version_id` and `entity_id`, with `id`/`canonical_id` removed

## ADDED Requirements

### Requirement: FTSSearch SDK response uses VersionID and EntityID

The SDK SHALL expose FTS search result objects using the `VersionID` and `EntityID` field names, consistent with the GraphObject struct rename.

#### Scenario: FTSSearch result objects use new field names

- **WHEN** SDK consumer calls `FTSSearch` and iterates over results
- **THEN** each result object SHALL have `VersionID` and `EntityID` fields populated

#### Scenario: FTSSearch deprecated fields accessible during transition

- **WHEN** SDK consumer accesses `result.ID` or `result.CanonicalID` during the deprecation period
- **THEN** the values SHALL be identical to `result.VersionID` and `result.EntityID` respectively

### Requirement: HybridSearch SDK response uses VersionID and EntityID

The SDK SHALL expose hybrid search result objects using the `VersionID` and `EntityID` field names, consistent with the GraphObject struct rename.

#### Scenario: HybridSearch result objects use new field names

- **WHEN** SDK consumer calls `HybridSearch` and iterates over results
- **THEN** each result object SHALL have `VersionID` and `EntityID` fields populated

#### Scenario: HybridSearch deprecated fields accessible during transition

- **WHEN** SDK consumer accesses `result.ID` or `result.CanonicalID` during the deprecation period
- **THEN** the values SHALL be identical to `result.VersionID` and `result.EntityID` respectively
