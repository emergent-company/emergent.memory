## 1. Server manifest plural support

- [x] 1.1 Add `SourceTypes`/`TargetTypes` fields and `GetSourceTypes`/`GetTargetTypes` methods to `RelationshipTypeDef` (`apps/server/domain/blueprints/manifest.go`), mirroring the CLI loader.
- [x] 1.2 Unit test: `GetSourceTypes`/`GetTargetTypes` prefer the plural array, fall back to singular, else nil (covered by `TestExpandRelationshipTypes`).

## 2. Expand plural declarations at apply time

- [x] 2.1 Add `expandRelationshipTypes` and use it in both the create and update pack paths so the payload sent to the schemas service is singular-only.
- [x] 2.2 Unit test: `TestExpandRelationshipTypes` locks singular pass-through, plural cross-product, and the deliberate neither-source-nor-target behaviour.

## 3. End-to-end regression test

- [x] 3.1 Add `TestImportFromGitHub_PluralRelationshipSourceTypes` driving GitHub import → apply with `sourceTypes: [Alpha, Beta]` + `targetType: Gamma`, asserting the schemas layer receives two singular entries.

## 4. Verification

- [x] 4.1 `go build ./...`
- [x] 4.2 `go test ./domain/blueprints/... ./domain/schemas/...`
- [x] 4.3 `gofmt -l` on changed files
- [x] 4.4 `openspec validate fix-blueprint-plural-relationship-source-types --strict`
