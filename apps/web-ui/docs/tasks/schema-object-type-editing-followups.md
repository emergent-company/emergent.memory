# Follow-up: schema object-type editing extensions

Origin: OpenSpec change `edit-object-types-in-schema-ui` (task 5.2).

The initial change covers editing **object** types in the Schema area. Deferred work:

## 1. Edit relationship types

- Relationship types are read-only in the Schema area (`gateway/schema.templ`, `compiledTypesTable`).
- Extend the same edit surface (project-authored pack write for project types, copy-on-write override pack for blueprint-derived types) to relationship types.
- Relationship types are keyed by `name|sourceType|targetType` in memory's compiled merge (`domain/schemas/repository.go`), so the editor must handle source/target type constraints.

## 2. Delete project-authored types

- No delete/uninstall UI exists for object types. Adding it must respect referenced data and blueprint ownership:
  - only project-authored (non-blueprint-derived) types may be deleted;
  - deleting a type that has existing objects needs an explicit confirmation and a policy for orphaned objects.

## 3. Backend dependencies

- Per-type version history and blueprint→pack provenance are not exposed by the memory API. Tracked in `emergent-company/emergent.memory#423`. The current change infers provenance gateway-side from blueprint manifests; switch to the authoritative link once available.
