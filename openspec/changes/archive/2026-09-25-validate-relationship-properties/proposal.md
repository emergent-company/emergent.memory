## Why

The graph API currently has no schema enforcement at the boundary. Objects and relationships can be created with any type name, any property keys, and any property values — even when the project has a schema that explicitly defines which types, properties, and connection rules are allowed. This makes schemas decorative rather than authoritative, and allows bad data to silently accumulate.

## What Changes

- **Object creation/update**: reject object types not declared in the schema; reject unknown (undeclared) properties; enforce required fields; enforce property type coercion
- **Relationship creation/patch**: reject relationship types not declared in the schema; enforce `fromTypes`/`toTypes` (source/target object types must match schema); reject unknown properties; enforce required fields; enforce property type coercion
- **No schema = nothing allowed**: if a project has a schema configured but it declares no object types or no relationship types, creation of those entities is blocked entirely (the schema is the source of truth)
- **No schema configured**: if the schema provider returns nothing (project has no schema installed), all creation passes through unchanged (backward compatible)

## Capabilities

### New Capabilities

- `schema-enforcement`: Strict schema-driven allowlist for object types, relationship types, endpoint type constraints, and property validation (unknown keys rejected, required fields enforced, type coercion)

### Modified Capabilities

<!-- No existing spec-level behavior changes -->

## Impact

- `apps/server/domain/graph/service.go` — `Create`, `CreateOrUpdate`, `Update`, `CreateRelationship`, `PatchRelationship`
- `apps/server/domain/graph/validation.go` — `validateProperties` to reject unknown keys; new `validateRelationship` function
- `apps/server/domain/extraction/agents/prompts.go` — `RelationshipSchema`: add `Properties`/`Required` fields
- `apps/server/domain/schemaregistry/dto.go` — `RelationshipSchema`: add `Properties`/`Required` fields
- **Breaking for projects with schemas**: callers that previously sent undeclared types or properties will start receiving `400`. Projects without any schema installed are unaffected.
