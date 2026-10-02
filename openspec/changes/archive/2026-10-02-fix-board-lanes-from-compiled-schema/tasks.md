## 1. Server compiled-types field preservation

- [x] 1.1 Add a failing server-side regression test that an array-form object-type schema (the `task-board` `Task` shape) yields compiled types carrying `boardEnabled`, `allowedStatuses`, `skipEmbeddings`, `skipExtraction`, and `excludeFromSearch` (`apps/server/domain/schemas/repository_test.go`).
- [x] 1.2 Add the map-format counterpart test asserting the compiled path agrees with the runtime normalisation for the same fields.
- [x] 1.3 Carry the work-config fields through the array branch of `parseObjectTypeSchemasToMap` (`apps/server/domain/schemas/repository.go`) so the reconstructed per-type schema object preserves them — parity with `extraction.normalizeSchemaToMap`.
- [x] 1.4 Verify `parseObjectTypeSchemas` returns the declared board config for both storage formats.

## 2. Gateway shadowed-type filtering

- [x] 2.1 Add a gateway regression test that a shadowed board type's statuses are excluded from `boardStatusesFromCompiled` (`apps/web-ui/gateway/board_test.go`).
- [x] 2.2 Filter shadowed types inside `boardStatusesFromCompiled` via `visibleCompiledTypes` (`apps/web-ui/gateway/board.go`).

## 3. Verification

- [x] 3.1 `cd apps/server && go build ./... && go test -count=1 ./domain/schemas/... ./domain/blueprints/... ./domain/extraction/...`
- [x] 3.2 `cd apps/web-ui/gateway && templ generate && go build ./... && go test ./...`
- [x] 3.3 `bash apps/server/scripts/lint-ratchet.sh` and `golangci-lint run ./...`
- [x] 3.4 Run the js-dom gate as CI runs it.
- [x] 3.5 `openspec validate fix-board-lanes-from-compiled-schema --strict`
