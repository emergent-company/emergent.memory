## 1. Rollback archive splice

- [x] 1.1 Replace `obj.MigrationArchive[:archiveIndex]` with a splice that removes only the matched entry and preserves newer entries
- [x] 1.2 Unit test: archive `[1.0.0 → 2.0.0, 2.0.0 → 3.0.0]`, rollback to `2.0.0` consumes the `2.0.0` entry and leaves the `3.0.0` entry (with its `dropped_data`) intact

## 2. Verification

All Go commands run from `apps/server/`; `openspec` runs from the repository root.

- [x] 2.1 `go build ./...` clean
- [x] 2.2 `go vet ./domain/graph/` clean
- [x] 2.3 `go test ./domain/graph/ -run TestSchemaMigration_Rollback -count=1` passes
- [x] 2.4 `openspec validate fix-rollback-archive-splice` passes (repository root)
