## Context

The graph service has two existing validation paths for objects: `validateProperties` in `validation.go` (type coercion + required fields) called from `Create`, `CreateOrUpdate`, and `Update`. However this validation:

1. Passes through unknown property keys silently
2. Does not check whether the object *type* itself is declared in the schema
3. Is entirely absent for relationships

The `schemaProvider` interface returns `ExtractionSchemas` which has both `ObjectSchemas map[string]agents.ObjectSchema` and `RelationshipSchemas map[string]agents.RelationshipSchema`. The relationship schema already carries `fromTypes`/`toTypes` (via `GetSourceTypes()`/`GetTargetTypes()` helpers on `schemaregistry.RelationshipSchema`), but neither struct has a `Properties`/`Required` field yet.

## Goals / Non-Goals

**Goals:**
- Object creation: reject type not in schema; reject unknown properties; enforce required; enforce property types
- Relationship creation: reject type not in schema; enforce `fromTypes`/`toTypes`; reject unknown properties; enforce required; enforce property types
- `PatchRelationship`: same property rules on merged props
- No schema installed on project → pass-through (unchanged behavior)
- Schema installed but empty object/relationship types map → block all creation of that entity kind

**Non-Goals:**
- Retroactive re-validation of existing rows
- Enforcing fromTypes/toTypes on existing data during patch (only checked on create)
- Modifying extraction pipeline validation (separate concern)

## Decisions

**1. `validateProperties` becomes strict: unknown keys are rejected**

Currently unknown keys pass through. Change the loop to accumulate an error for any key not present in `schema.Properties`. This applies to both object and relationship validation.

The `agents.ObjectSchema` struct is reused for relationship property schemas (same `Properties map[string]PropertyDef` + `Required []string` shape). `RelationshipSchema` in both `agents/prompts.go` and `schemaregistry/dto.go` gets these two fields added.

**2. New `validateRelationship` function in `validation.go`**

Encapsulates the full relationship check:
- Type exists in schema map (if schema map is non-nil)
- Source object type is in `GetSourceTypes()` (if non-empty)
- Dest object type is in `GetTargetTypes()` (if non-empty)
- Property validation via `validateProperties`

This keeps `service.go` clean — one call, all checks.

**3. Object type allowlist check inline in service**

No new function needed — a simple `if _, ok := schemas.ObjectSchemas[req.Type]; !ok` before the property validation block handles it.

**4. "No schema" vs "empty schema" distinction**

- `schemaProvider == nil` or `GetProjectSchemas` returns error → soft-fail, skip all validation (no schema installed)
- Schema loaded successfully but `ObjectSchemas` is empty → block object creation (`object_type_not_allowed`)
- Schema loaded successfully but `RelationshipSchemas` is empty → block relationship creation (`relationship_type_not_allowed`)

**5. `ExtractionSchemas` carries `agents.RelationshipSchema` but `schemaregistry.RelationshipSchema` is what the JSON is parsed into**

The `schemaProviderAdapter` in `module.go` converts `schemaregistry.RelationshipSchema` → `agents.RelationshipSchema`. The `Properties`/`Required` fields must be added to both and the adapter must copy them across.

## Risks / Trade-offs

- **Breaking for existing projects with schemas**: any caller sending undeclared object types or property keys will get `400`. This is intentional.
  → Mitigation: document clearly; projects without schemas installed are unaffected.

- **`PatchRelationship` can't re-check fromTypes/toTypes**: the endpoints are already established; patching only changes properties/weight. Only property validation runs on patch.
  → Acceptable: type + endpoint enforcement happens at create time.

## Migration Plan

No DB migration. Deploy via hot reload. Projects must ensure their schema JSON is up-to-date before this is enabled — no schema = safe pass-through.

## Open Questions

None.
