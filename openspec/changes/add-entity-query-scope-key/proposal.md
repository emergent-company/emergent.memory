## Why

`entity-query` cannot tell that a property filter like `chapter_id` only makes
sense within one parent document: `LegalParagraph.chapter_id` repeats in every
law, so `filters:{chapter_id:"kapittel-2-kapittel-1"}` matched 697 rows across
many unrelated laws/forskrifter (issue #1148). The opt-in `key_prefix` from
#1164 makes correct scoping possible, but the reported call still cross-matches
because the tool has no schema knowledge that `chapter_id` is a child of
`law_ref_id`. This is the deferred "option 2" product fix from PR #1164.

## What Changes

- **Schema field.** Extend the object-type schema format with an optional
  top-level **`scopeKey`** declaration naming the property that scopes the type
  to a parent/identity document and the reference it points at, e.g.
  `LegalParagraph.law_ref_id` → `Law.ref_id`. Absent = no change (backwards
  compatible). Accepted in JSON/SDK, blueprint manifests (server + CLI), and the
  project schema registry. Also accepts `scope_key`/`references_type`/… aliases.
- **Enforcement.** When the queried type declares a scope key, `entity-query`
  **rejects** (fail-closed) any filter on a property that is neither the declared
  scope property nor the declared identity property unless the scope property is
  also supplied in `filters` (or an explicit `key_prefix` is given). The error is
  actionable: `query entities: filter "chapter_id" requires scope key
  "law_ref_id" (declared scope for type LegalParagraph)`. A rejected call never
  executes a cross-document scan. **BREAKING** for callers that relied on a bare
  non-unique filter against a scope-declaring type; types with no declaration are
  unchanged.
- **Validation.** The scope key is validated where schemas/blueprints are
  validated: the named property must exist on the type, and a declared reference
  target must resolve to a known object type and property. Malformed declarations
  fail closed with an actionable message. Also validated on the registry
  type create/update API.
- **Kept.** The existing opt-in `key_prefix` remains a valid explicit scoping
  mechanism and satisfies the requirement.

## Capabilities

### New Capabilities

- `object-type-scope-key`: the optional schema-level scope-key declaration and
  its validation.

### Modified Capabilities

- `mcp-tool-results`: `entity-query` enforces a type's declared scope key on
  non-identity property filters and rejects the unscoped caller-visible call.

## Impact

- **Server** `domain/schemas` (declaration type, parse/validate, registry
  round-trip), `domain/mcp/service.go` (enforcement + tool description),
  `domain/schemaregistry` (type create/update validation),
  `domain/blueprints/manifest.go` (pass-through field).
- **CLI** `internal/blueprints/types.go` (pass-through field for blueprint
  packs).
- **No migration**: the declaration lives inside the existing `json_schema` /
  `object_type_schemas` JSONB, so nothing is added to the database.
- **UI/blueprint authoring surfacing** of the field is a follow-up, not in this
  change.
- **Tests**: pure unit tests for parse/validate; DB-backed fail-first tests for
  the rejection, the scoped result set, and the no-declaration no-regression.
