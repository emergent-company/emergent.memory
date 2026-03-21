## ADDED Requirements

### Requirement: validateProperties rejects unknown property keys
When validating properties against a schema that has a non-empty `Properties` map, `validateProperties` SHALL reject any property key that is not declared in the schema. The error message SHALL name the unknown field.

#### Scenario: Unknown property key is rejected
- **WHEN** a caller supplies a property key that is not in the schema's `Properties` map
- **THEN** `validateProperties` SHALL return an error naming the unknown field
- **AND** nothing SHALL be written to the database

#### Scenario: All property keys declared — passes
- **WHEN** all supplied property keys exist in the schema
- **THEN** validation proceeds normally (type coercion + required checks)

#### Scenario: Empty properties map — all keys allowed
- **WHEN** the schema has an empty `Properties` map (no properties defined)
- **THEN** any properties pass through unchanged (no unknown-key check)

---

### Requirement: Object type must be declared in schema
When a project has a schema installed (schema provider returns a non-error result), `Create`, `CreateOrUpdate`, and `Update` SHALL reject any object whose `type` is not a key in `ObjectSchemas`.

#### Scenario: Object type not in schema
- **WHEN** a client creates an object with a type not declared in the project schema
- **THEN** the server SHALL return `400 Bad Request` with error code `object_type_not_allowed`
- **AND** no object SHALL be written to the database

#### Scenario: Schema installed with no object types defined
- **WHEN** a project has a schema installed but it declares zero object types
- **THEN** all object creation SHALL be rejected with `object_type_not_allowed`

#### Scenario: No schema installed
- **WHEN** the schema provider returns no schemas for the project
- **THEN** object creation passes through without type enforcement (backward compatible)

#### Scenario: Schema provider returns error
- **WHEN** the schema provider fails transiently
- **THEN** the server SHALL log a warning and allow the operation (soft-fail)

---

### Requirement: Relationship type must be declared in schema
`CreateRelationship` SHALL reject any relationship whose `type` is not a key in `RelationshipSchemas` when the project has a schema installed.

#### Scenario: Relationship type not in schema
- **WHEN** a client creates a relationship with a type not declared in the schema
- **THEN** the server SHALL return `400 Bad Request` with error code `relationship_type_not_allowed`
- **AND** no relationship SHALL be written to the database

#### Scenario: Schema installed with no relationship types
- **WHEN** a project schema declares zero relationship types
- **THEN** all relationship creation SHALL be rejected with `relationship_type_not_allowed`

#### Scenario: No schema installed
- **WHEN** the schema provider returns no schemas for the project
- **THEN** relationship creation passes through without type enforcement

---

### Requirement: Relationship endpoint types must match schema
When `CreateRelationship` is called and the matched relationship schema declares non-empty `fromTypes`/`toTypes`, the source and destination object types SHALL be validated against those lists.

#### Scenario: Source object type not in fromTypes
- **WHEN** a client creates a relationship where the source object's type is not in the schema's `fromTypes`
- **THEN** the server SHALL return `400 Bad Request` with error code `relationship_source_type_not_allowed`

#### Scenario: Destination object type not in toTypes
- **WHEN** a client creates a relationship where the destination object's type is not in the schema's `toTypes`
- **THEN** the server SHALL return `400 Bad Request` with error code `relationship_target_type_not_allowed`

#### Scenario: Schema declares no fromTypes/toTypes
- **WHEN** the relationship schema has empty `fromTypes` and `toTypes`
- **THEN** any source/destination object types are accepted

---

### Requirement: Relationship schema supports typed properties
`RelationshipSchema` in `agents/prompts.go` and `schemaregistry/dto.go` SHALL support `Properties map[string]PropertyDef` and `Required []string` fields. The `schemaProviderAdapter` SHALL copy these fields when converting between the two types.

#### Scenario: Relationship schema with properties is parsed and copied
- **WHEN** a schema JSON defines `properties` on a relationship type
- **THEN** those properties are available in `agents.RelationshipSchema` for validation

---

### Requirement: Relationship properties validated on create and patch
`CreateRelationship` and `PatchRelationship` SHALL run `validateProperties` (strict — unknown keys rejected, required enforced, types coerced) against the relationship's schema `Properties`.

#### Scenario: Unknown relationship property rejected on create
- **WHEN** a client creates a relationship with a property key not in the schema
- **THEN** the server SHALL return `400 Bad Request`

#### Scenario: Missing required relationship property rejected on create
- **WHEN** a client creates a relationship missing a required property
- **THEN** the server SHALL return `400 Bad Request`

#### Scenario: Patch merged props violate schema
- **WHEN** a client patches a relationship resulting in merged props that fail schema validation
- **THEN** the server SHALL return `400 Bad Request` and no new version is written
