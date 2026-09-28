## MODIFIED Requirements

### Requirement: Scope-key declaration validation fails closed

Where object-type schemas are validated (schema-pack create/update, blueprint
apply, the project schema registry type create/update API, the `schema-create`
MCP tool, and backup restore), a `scopeKey` declaration SHALL be rejected with
an actionable message when it names a property that is not present on the type,
when `identityProperty` (if present) is not a property of the type, when
`referencesType`/`referencesProperty` are half-set, or when the reference target
does not resolve to a known object type (or the target property does not exist
on it). Every such write path SHALL reject the declaration *before* persisting
it: no path may write a `json_schema` (or registry row) carrying a malformed
declaration. This includes the `schema-create` MCP tool and backup restore, which
write type schemas directly rather than through schema-pack create/update.

#### Scenario: Unknown own property is rejected

- **WHEN** a type declares `scopeKey.property` that is not one of its properties
- **THEN** validation fails with a message naming the type and the unknown property

#### Scenario: Unresolvable reference target is rejected

- **WHEN** a type declares a `referencesType` that is not a known object type
- **THEN** validation fails with a message naming the unresolvable target

#### Scenario: Half-specified reference is rejected

- **WHEN** a type declares `referencesType` without `referencesProperty` (or vice versa)
- **THEN** validation fails with a message stating they must be set together

#### Scenario: schema-create MCP tool rejects a malformed declaration

- **WHEN** a client calls the `schema-create` MCP tool with an object type whose
  `scopeKey` names a property the type does not declare
- **THEN** the call fails with an actionable message and no registry row is persisted

#### Scenario: Backup restore rejects a malformed declaration

- **WHEN** a backup snapshot contains an object-type schema row whose `scopeKey`
  is malformed
- **THEN** the restore fails with an actionable message and no schema row is inserted
