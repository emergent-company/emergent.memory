# relationship-property-validation Specification

## Purpose
Validates graph object and relationship properties against installed schemas. Declared properties are type-coerced and required fields are enforced; undeclared property keys, object types, and relationship types pass through unchanged because the schema documents known types but is not an allowlist.

## Requirements

### Requirement: validateProperties passes through unknown property keys
When validating properties against a schema, `validateProperties` SHALL coerce and validate only the property keys declared in the schema's `Properties` map; property keys that are not declared SHALL be passed through unchanged. The schema is NOT an allowlist — users MAY store arbitrary metadata keys alongside declared ones.

#### Scenario: Unknown property key is passed through
- **WHEN** a caller supplies a property key that is not in the schema's `Properties` map (and the schema has a non-empty `Properties` map)
- **THEN** `validateProperties` SHALL return the unknown key unchanged in the result
- **AND** no error SHALL be returned for the unknown key

#### Scenario: Declared property keys are coerced and validated
- **WHEN** all supplied property keys exist in the schema
- **THEN** validation proceeds normally (type coercion + required checks), and a malformed value for a declared key SHALL produce an error naming that key

#### Scenario: Empty properties map — all keys passed through
- **WHEN** the schema has an empty `Properties` map (no properties defined)
- **THEN** any properties pass through unchanged (no validation)

### Requirement: Object type validation is allowlist-free
When a project has a schema installed, object creation SHALL validate properties only for object types that are declared in `ObjectSchemas`. An object whose `type` is not declared SHALL be accepted unchanged (the schema documents constraints for known types but does not act as an allowlist).

#### Scenario: Object type not in schema
- **WHEN** a client creates an object with a type not declared in the project schema
- **THEN** the object SHALL be created with its properties passed through unchanged

#### Scenario: Declared object type validates properties
- **WHEN** a client creates an object whose type is declared in the schema
- **THEN** the object's properties SHALL be coerced and validated against that type's `Properties`, and schema defaults SHALL be applied

#### Scenario: No schema installed
- **WHEN** the schema provider returns no schemas for the project
- **THEN** object creation passes through without type enforcement (backward compatible)

#### Scenario: Schema provider returns error
- **WHEN** the schema provider fails transiently
- **THEN** the server SHALL log a warning and allow the operation (soft-fail)

### Requirement: Relationship type validation is allowlist-free
`CreateRelationship` SHALL validate properties only for relationship types that are declared in `RelationshipSchemas`. A relationship whose `type` is not declared SHALL be accepted unchanged.

#### Scenario: Relationship type not in schema
- **WHEN** a client creates a relationship with a type not declared in the schema
- **THEN** the relationship SHALL be created with its properties passed through unchanged

#### Scenario: Declared relationship type validates properties
- **WHEN** a client creates a relationship whose type is declared in the schema and that type declares `Properties` or `Required`
- **THEN** the relationship's properties SHALL be coerced and validated (required enforced, unknown keys passed through)

#### Scenario: No schema installed
- **WHEN** the schema provider returns no schemas for the project
- **THEN** relationship creation passes through without type enforcement

### Requirement: Relationship endpoint types are informational
When `CreateRelationship` matches a declared relationship schema, the schema's `fromTypes`/`toTypes` SHALL be treated as documentation of the intended source/destination object types, NOT as hard enforcement gates. Any source/destination object type combination SHALL be accepted.

#### Scenario: Source object type not in fromTypes
- **WHEN** a client creates a relationship where the source object's type is not in the schema's `fromTypes`
- **THEN** the relationship SHALL still be created (fromTypes is informational)

#### Scenario: Schema declares no fromTypes/toTypes
- **WHEN** the relationship schema has empty `fromTypes` and `toTypes`
- **THEN** any source/destination object types are accepted

### Requirement: Relationship schema supports typed properties
`RelationshipSchema` in `agents/prompts.go` and `schemaregistry/dto.go` SHALL support `Properties map[string]PropertyDef` and `Required []string` fields. The `schemaProviderAdapter` SHALL copy these fields when converting between the two types.

#### Scenario: Relationship schema with properties is parsed and copied
- **WHEN** a schema JSON defines `properties` on a relationship type
- **THEN** those properties are available in `agents.RelationshipSchema` for validation

### Requirement: Relationship properties validated on create and patch
`CreateRelationship` SHALL run `validateProperties` against the matched relationship schema's `Properties` when that schema declares any: required fields SHALL be enforced and declared types coerced, and property keys not declared in the schema SHALL be passed through unchanged. `PatchRelationship` SHALL run `validateRelationshipPatchProperties`, which coerces only the properties being added or changed in the patch delta and enforces required fields against the merged result (the existing properties plus the patch delta), so a patch can neither clear nor omit a required property.

#### Scenario: Declared relationship property coerced on create
- **WHEN** a client creates a relationship with a declared property of the wrong type
- **THEN** the server SHALL coerce it to the declared type (or return an error naming the field when coercion fails)

#### Scenario: Missing required relationship property rejected on create
- **WHEN** a client creates a relationship missing a required property declared by its schema
- **THEN** the server SHALL return `400 Bad Request`

#### Scenario: Patch delta coerced, required enforced on merged result
- **WHEN** a client patches a relationship and a property in the patch delta fails the declared schema's type coercion
- **THEN** the server SHALL return `400 Bad Request` and no new version is written
- **AND** a patch that clears or omits a schema-required property (leaving the merged result without it) SHALL return `400 Bad Request`
- **AND** a patch that preserves all schema-required properties SHALL succeed

#### Scenario: Unknown relationship property passed through on create
- **WHEN** a client creates a relationship with a property key not declared in the schema
- **THEN** the server SHALL accept it and store the key unchanged
