## 1. Extend RelationshipSchema types

- [x] 1.1 Add `Properties map[string]PropertyDef` and `Required []string` to `RelationshipSchema` in `apps/server/domain/extraction/agents/prompts.go`
- [x] 1.2 Add `Properties map[string]agents.PropertyDef` and `Required []string` to `RelationshipSchema` in `apps/server/domain/schemaregistry/dto.go` (add `agents` import)
- [x] 1.3 Update `schemaProviderAdapter` in `apps/server/domain/graph/module.go` to copy `Properties` and `Required` when converting `schemaregistry.RelationshipSchema` → `agents.RelationshipSchema`

## 2. Strict property validation (reject unknown keys)

- [x] 2.1 In `validateProperties` (`apps/server/domain/graph/validation.go`), change the unknown-key branch from pass-through to error: when `!hasDef` and `schema.Properties` is non-empty, append `"unknown property: <key>"` to `validationErrors`

## 3. Object type allowlist in service

- [x] 3.1 In `Create`, `CreateOrUpdate`, and `Update` in `apps/server/domain/graph/service.go`: after loading schemas successfully, if `schemas.ObjectSchemas` is non-nil (schema is installed), check `if _, ok := schemas.ObjectSchemas[req.Type]; !ok` and return `apperror.ErrBadRequest.WithMessage("object_type_not_allowed")`

## 4. Relationship validation function

- [x] 4.1 Add `validateRelationship` to `apps/server/domain/graph/validation.go` — accepts `req.Type`, `srcObjType`, `dstObjType`, `req.Properties`, and `schemas *ExtractionSchemas`; performs: type allowlist check, fromTypes/toTypes check, property validation via `validateProperties`; returns error with appropriate message codes

## 5. Wire relationship validation into service

- [x] 5.1 In `CreateRelationship` (`apps/server/domain/graph/service.go`), after resolving endpoints and before building the `rel` struct, call `validateRelationship` (soft-fail on schema load error, increment counters)
- [x] 5.2 In `PatchRelationship`, after building merged `newProps`, load schemas and call `validateProperties` on merged props against the relationship schema (soft-fail on error)

## 6. Tests

- [x] 6.1 Update `validateProperties` unit tests in `apps/server/domain/graph/validation_test.go` to cover: unknown key rejected when properties map is non-empty, unknown key allowed when properties map is empty
- [x] 6.2 Add `validateRelationship` unit tests: type not in schema → error, fromTypes mismatch → error, toTypes mismatch → error, unknown property → error, missing required → error, valid → nil
- [x] 6.3 Add service-level tests for `Create` (object): unknown type rejected, unknown property rejected, valid passes
- [x] 6.4 Add service-level tests for `CreateRelationship`: unknown rel type rejected, fromTypes mismatch rejected, toTypes mismatch rejected, unknown property rejected, valid passes, no schema → passes
- [x] 6.5 Add service-level tests for `PatchRelationship`: unknown property in merged props rejected, valid patch passes

## 7. Verify

- [x] 7.1 Run `task build` to confirm no compile errors
- [x] 7.2 Run `task test` to confirm all tests pass
