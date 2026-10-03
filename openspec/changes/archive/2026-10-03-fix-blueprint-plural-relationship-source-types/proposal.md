## Why

Importing a blueprint from a GitHub URL whose pack declares a relationship type
with plural `sourceTypes`/`targetTypes` (arrays) fails during apply with:

```
400 bad_request: invalid schema definitions: relationshipTypeSchemas [8]
("delivered_in") is missing required field 'sourceType'
```

The CLI loader accepts the plural form
(`apps/cli/internal/blueprints/types.go` — `RelationshipTypeDef` +
`GetSourceTypes`/`GetTargetTypes`), but the server-side blueprint manifest
(`apps/server/domain/blueprints/manifest.go` `RelationshipTypeDef`) only knows
the singular `sourceType`/`targetType`, so the plural YAML is silently dropped.
`apply.go` then marshals the now source-less/target-less defs into the schemas
service, whose `validateSchemaDefinitions` requires a non-empty singular
`sourceType`/`targetType`, producing the 400.

## What Changes

- Add plural `SourceTypes`/`TargetTypes` fields to the server-side
  `RelationshipTypeDef`, plus `GetSourceTypes`/`GetTargetTypes` accessors
  mirroring the CLI (plural array wins; singular fallback; else nil).
- At apply time, expand plural relationship declarations into singular entries
  (the cross-product of source types × target types) before posting to the
  schemas service, in both the create and update pack paths. The stored
  blueprint manifest keeps the plural fields (round-trip); only the schemas
  payload is singular.
- Bound each pack's expansion to 10 000 singular relationship types. The total is
  pre-computed and an over-budget pack is rejected with `400 bad_request` naming
  the offending definition *before* any output is allocated, so a pathological
  cross-product cannot exhaust memory.
- Add an end-to-end regression test driving GitHub import → apply and asserting
  the schemas layer receives two singular entries for `sourceTypes: [Alpha,
  Beta]` + `targetType: Gamma`.

## Capabilities

### Modified Capabilities

- `blueprint-api`: blueprint relationship type declarations accept plural
  `sourceTypes`/`targetTypes`, expanded to singular schemas on apply.

## Impact

- `apps/server/domain/blueprints/manifest.go`: plural fields + accessors.
- `apps/server/domain/blueprints/apply.go`: `expandRelationshipTypes` helper,
  used in both the create and update pack paths.
- `apps/server/domain/blueprints/github_import_e2e_test.go`: regression test.
- No DB schema changes; no breaking changes (singular declarations are
  unchanged).
