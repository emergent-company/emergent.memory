## Purpose

An optional, backward-compatible way for an object type to declare the property
that scopes it to a parent/identity document and the reference that property
points at, so downstream consumers (starting with `entity-query`) can reason
about which property filter pins a document.

## ADDED Requirements

### Requirement: Optional object-type scope-key declaration

An object type schema MAY carry a top-level `scopeKey` object declaring the
property on the type that scopes it to a parent/identity document:

- `property` (required when `scopeKey` is present): the property on this type
  that scopes it (e.g. `law_ref_id`).
- `referencesType` / `referencesProperty` (optional together): the target object
  type and the property on it that `property` values reference (e.g. `Law.ref_id`).
- `identityProperty` (optional): the property that identifies an entity within
  the scope (e.g. `section_id`).

`property` and `identityProperty` are consumed by `entity-query` enforcement.
`referencesType`/`referencesProperty` are **declarative reference metadata**: they
are validated (fail-closed) but intentionally have no runtime query consumer yet
— they let tooling (schema browsers, graph navigators, future join/traverse
features) follow the reference without guessing.

A type schema with no `scopeKey` MUST be accepted unchanged. The declaration
SHALL be stored inside the type's existing JSON schema (`json_schema` /
`object_type_schemas`), so no database migration is required. Both camelCase and
snake_case aliases (`scope_key`, `references_type`, `references_property`,
`identity_property`) SHALL be accepted.

#### Scenario: No declaration is accepted unchanged

- **WHEN** a type schema is created with no `scopeKey`
- **THEN** it is accepted and behaves exactly as before

#### Scenario: Declaration is preserved into the registry

- **WHEN** a schema pack or blueprint declares a `scopeKey` for a type
- **THEN** the project schema registry's stored JSON schema for that type carries
  the declaration

### Requirement: Scope-key declaration validation fails closed

Where object-type schemas are validated (schema-pack create/update, blueprint
apply, and the project schema registry type create/update API), a `scopeKey`
declaration SHALL be rejected with an actionable message when it names a property
that is not present on the type, when `identityProperty` (if present) is not a
property of the type, when `referencesType`/`referencesProperty` are half-set, or
when the reference target does not resolve to a known object type (or the target
property does not exist on it).

#### Scenario: Unknown own property is rejected

- **WHEN** a type declares `scopeKey.property` that is not one of its properties
- **THEN** validation fails with a message naming the type and the unknown property

#### Scenario: Unresolvable reference target is rejected

- **WHEN** a type declares a `referencesType` that is not a known object type
- **THEN** validation fails with a message naming the unresolvable target

#### Scenario: Half-specified reference is rejected

- **WHEN** a type declares `referencesType` without `referencesProperty` (or vice versa)
- **THEN** validation fails with a message stating they must be set together
