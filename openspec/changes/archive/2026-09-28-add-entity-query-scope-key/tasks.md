# Tasks

## 1. Schema field

- [x] 1.1 Add `ScopeKeyDeclaration` + `ParseScopeKey` + `ValidateScopeKey` /
  `ValidateTypeScopeKey` in `domain/schemas` (camelCase + snake_case aliases).
- [x] 1.2 Preserve `scopeKey` through pack array→map parsing and schema merge,
  so it reaches the registry `json_schema`.
- [x] 1.3 Carry `scopeKey` in the server blueprint manifest and the CLI pack
  type (pass-through, no behavior at authoring time).

## 2. Validation

- [x] 2.1 Validate scope keys in `validateSchemaDefinitions` (schema-pack
  create/update + blueprint apply): own property, identity property,
  half-specified reference, unresolved target/type, unresolved target property.
- [x] 2.2 Validate the registry type create/update API against the project's
  registered types.
- [x] 2.3 Unit tests for parse/validate and `validateSchemaDefinitions`.

## 3. Enforcement

- [x] 3.1 `executeQueryEntities`: before any scan, reject a non-identity filter
  on a scope-declaring type unless the scope property (or `key_prefix`) is present.
- [x] 3.2 Actionable error naming the filter and required scope key.
- [x] 3.3 Update the `filters` tool description.

## 4. Tests (fail-first, DB-backed)

- [x] 4.1 (a) declared scope key + bare non-identity filter → rejected (RED
  before the contract, GREEN after).
- [x] 4.2 (b) same call WITH the scope key → scoped to one document.
- [x] 4.3 (c) type without declaration → unchanged; `key_prefix` and
  identity-property filters still work.

## 5. Verification

- [x] 5.1 `go build ./...` (server + CLI), `task lint`, `gofmt -l`.
- [x] 5.2 `openspec validate --all --strict`.
- [x] 5.3 Hermetic Postgres: RED→GREEN for the three tests; `EXPLAIN` confirms
  the scoped containment predicate still uses `idx_graph_objects_project_type_props_gin`
  (no seq scan).
