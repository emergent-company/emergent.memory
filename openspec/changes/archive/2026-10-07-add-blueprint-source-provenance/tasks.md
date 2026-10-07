## 1. Database migration

- [x] 1.1 Add `00222_add_blueprint_source_provenance.sql` adding nullable
      `source_kind`, `source_url`, `source_ref` columns to `kb.blueprints`, with
      a down migration dropping them.

## 2. Entity and service

- [x] 2.1 Add `SourceKind`, `SourceURL`, `SourceRef` fields to `Blueprint`
      (bun + camelCase json tags, all optional) and `CreateBlueprintRequest`.
- [x] 2.2 Extract the canonical manifest checksum into a shared helper used by
      both `CreateBlueprint` and `PublishBlueprint`, and set the checksum at
      create time.
- [x] 2.3 Persist `SourceKind`/`SourceURL`/`SourceRef` in `CreateBlueprint`.

## 3. GitHub import

- [x] 3.1 Populate `SourceKind="github"`, normalized `SourceURL`, and `SourceRef`
      in `ImportFromGitHub`, normalizing the `HEAD` sentinel to `""`.

## 4. Tests

- [x] 4.1 Unit/integration test: a GitHub import records `sourceKind`,
      normalized `sourceUrl`, and `sourceRef` for explicit-ref and default-HEAD
      cases, plus a non-empty checksum.
- [x] 4.2 Unit/integration test: `CreateBlueprint` sets a canonical checksum and
      persists provenance fields.

## 5. Verification

- [x] 5.1 `go build ./...` from `apps/server`.
- [x] 5.2 `go test ./domain/blueprints/...` (DB-backed) passes.
- [x] 5.3 `gofmt -l` clean on changed files.
- [x] 5.4 `openspec validate add-blueprint-source-provenance --strict` passes.
